package yandex

// Initial-auth compatibility adapted from upstream 38782b2. This helper owns no
// client or dialer: all traffic uses the caller's transport and cookie jar.
import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"openflux/utils"
)

const (
	maxCaptchaBody            = 1 << 20 // both encoded and decoded response bytes
	maxCaptchaSSR             = 64 << 10
	maxCaptchaAttempts        = 1
	maxCaptchaWork            = 10_000_000
	maxCaptchaComplexity      = 24
	captchaWorkTimeout        = 5 * time.Second
	captchaFlowTimeout        = 45 * time.Second
	maxVolgaBootstrapRequests = 10 // includes the post-captcha continuation
)

var (
	reCaptchaSSR  = regexp.MustCompile(`window\.__SSR_DATA__\s*=\s*JSON\.parse\(atob\("([^"]+)"\)\)`)
	reCaptchaForm = regexp.MustCompile(`<form[^>]*id="tmgrdfrend-form"[^>]*action="([^"]+)"`)
)

type captchaSSRData struct {
	UniqueKey string `json:"uniqueKey"`
	Pow       struct {
		Complexity int    `json:"complexity"`
		Prefix     string `json:"prefix"`
	} `json:"pow"`
}

func resolveAuthRedirect(base, location string) (*url.URL, error) {
	b, err := url.Parse(base)
	if err != nil {
		return nil, errors.New("invalid authorization redirect")
	}
	u, err := url.Parse(location)
	if err != nil || location == "" {
		return nil, errors.New("invalid authorization redirect")
	}
	u = b.ResolveReference(u)
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return nil, errors.New("invalid authorization redirect")
	}
	return u, nil
}

func isVolgaCaptcha(u *url.URL) bool {
	return u != nil && u.User == nil && u.Scheme == "https" && strings.EqualFold(u.Host, "docs.yandex.ru") && u.Path == "/showcaptchafast"
}

// Close even on decode/read errors. Do not drain an untrusted body to EOF.
// Explicit encoding support also handles responses not auto-decoded by Go.
func readAuthBody(resp *http.Response, limit int64) ([]byte, error) {
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errors.New("authorization response unreadable or too large")
	}
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
		return raw, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, errors.New("invalid authorization compression")
		}
		defer reader.Close()
		body, err := io.ReadAll(io.LimitReader(reader, limit+1))
		if err != nil || int64(len(body)) > limit {
			return nil, errors.New("authorization response unreadable or too large")
		}
		return body, nil
	default:
		return nil, errors.New("unsupported authorization compression")
	}
}

func captchaRequestError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// net/http errors include URLs. Never return them to caller logs.
	return errors.New("captcha request failed")
}

func solveVolgaCaptcha(ctx context.Context, session *http.Client, challenge *url.URL) (completion *url.URL, err error) {
	utils.Debugf("[VOLGA] CAPTCHA_FLOW_STARTED")
	defer func() {
		if err != nil {
			utils.Debugf("[VOLGA] CAPTCHA_FLOW_FAILED")
		} else {
			utils.Debugf("[VOLGA] CAPTCHA_FLOW_COMPLETED")
		}
	}()
	if session == nil || session.Jar == nil || session.Transport == nil || session.Timeout <= 0 || !isVolgaCaptcha(challenge) {
		return nil, errors.New("invalid captcha session")
	}
	ctx, cancel := context.WithTimeout(ctx, captchaFlowTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", challenge.String(), nil)
	if err != nil {
		return nil, errors.New("invalid captcha request")
	}
	setCaptchaHeaders(req)
	resp, err := session.Do(req)
	if err != nil {
		return nil, captchaRequestError(ctx)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, errors.New("captcha page status rejected")
	}
	body, err := readAuthBody(resp, maxCaptchaBody)
	if err != nil {
		return nil, err
	}
	ssr, action, err := parseVolgaCaptcha(body, challenge)
	if err != nil {
		return nil, err
	}
	workCtx, stopWork := context.WithTimeout(ctx, captchaWorkTimeout)
	nonce, err := solveCaptchaPoW(workCtx, ssr.Pow.Prefix, ssr.Pow.Complexity)
	stopWork()
	if err != nil {
		return nil, err
	}
	fingerprint, err := encodeCaptchaFingerprint(buildCaptchaFingerprint(nonce, volgaAuthUserAgent))
	if err != nil {
		return nil, err
	}
	form := url.Values{"version": {"1.5.0"}, "uniquekey": {ssr.UniqueKey}, "chstate": {"ok"}, "fingerprint": {fingerprint}}
	req, err = http.NewRequestWithContext(ctx, "POST", action.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("invalid captcha submission")
	}
	setCaptchaHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", challenge.Scheme+"://"+challenge.Host)
	req.Header.Set("Referer", challenge.String())
	resp, err = session.Do(req)
	if err != nil {
		return nil, captchaRequestError(ctx)
	}
	// No redirect is followed here; the caller continues its bounded bootstrap loop.
	// A 200/307/308 is not a successful POST-to-GET challenge completion.
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
		return nil, errors.New("captcha submission status rejected")
	}
	completion, err = resolveAuthRedirect(action.String(), resp.Header.Get("Location"))
	if err != nil {
		return nil, errors.New("invalid captcha completion redirect")
	}
	return completion, nil
}

func parseVolgaCaptcha(body []byte, challenge *url.URL) (*captchaSSRData, *url.URL, error) {
	m := reCaptchaSSR.FindSubmatch(body)
	if len(m) != 2 || len(m[1]) > base64.StdEncoding.EncodedLen(maxCaptchaSSR) {
		return nil, nil, errors.New("captcha bootstrap missing or too large")
	}
	raw, err := base64.StdEncoding.DecodeString(string(m[1]))
	if err != nil || len(raw) > maxCaptchaSSR {
		return nil, nil, errors.New("invalid captcha bootstrap")
	}
	var ssr captchaSSRData
	if json.Unmarshal(raw, &ssr) != nil || ssr.UniqueKey == "" || len(ssr.UniqueKey) > 4096 ||
		ssr.Pow.Prefix == "" || len(ssr.Pow.Prefix) > 256 || ssr.Pow.Complexity < 0 || ssr.Pow.Complexity > maxCaptchaComplexity {
		return nil, nil, errors.New("invalid captcha bootstrap")
	}
	m = reCaptchaForm.FindSubmatch(body)
	if len(m) != 2 || len(m[1]) > 4096 {
		return nil, nil, errors.New("captcha form missing or too large")
	}
	action, err := resolveAuthRedirect(challenge.String(), html.UnescapeString(string(m[1])))
	if err != nil || action.Scheme != challenge.Scheme || !strings.EqualFold(action.Host, challenge.Host) || action.Fragment != "" {
		return nil, nil, errors.New("captcha form origin rejected")
	}
	return &ssr, action, nil
}

func solveCaptchaPoW(ctx context.Context, prefixHex string, complexity int) (string, error) {
	if complexity < 0 || complexity > maxCaptchaComplexity || len(prefixHex) == 0 || len(prefixHex) > 256 {
		return "", errors.New("captcha work parameters rejected")
	}
	prefix, err := hex.DecodeString(prefixHex)
	if err != nil {
		prefix = []byte(prefixHex)
	} // upstream protocol's literal-prefix fallback
	input := make([]byte, 16+len(prefix))
	copy(input[16:], prefix)
	for attempt := 0; attempt < maxCaptchaWork; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		binary.LittleEndian.PutUint64(input[:8], uint64(time.Now().UnixMilli()))
		binary.LittleEndian.PutUint64(input[8:16], rand.Uint64()>>1)
		sum := sha256.Sum256(input)
		if captchaCheckComplexity(sum[:], complexity) {
			return hex.EncodeToString(input[:16]), nil
		}
	}
	return "", errors.New("captcha work limit reached")
}

func captchaCheckComplexity(hash []byte, bits int) bool {
	if bits < 0 || bits > len(hash)*8 {
		return false
	}
	for _, b := range hash[:bits/8] {
		if b != 0 {
			return false
		}
	}
	return bits%8 == 0 || hash[bits/8]>>(8-uint(bits%8)) == 0
}

func encodeCaptchaFingerprint(fp map[string]interface{}) (string, error) {
	raw, err := json.Marshal(fp)
	if err != nil {
		return "", errors.New("invalid captcha fingerprint")
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		w.Close()
		return "", errors.New("captcha fingerprint encoding failed")
	}
	if err := w.Close(); err != nil {
		return "", errors.New("captcha fingerprint encoding failed")
	}
	return "~" + base64.StdEncoding.EncodeToString(buf.Bytes()) + "~", nil
}

func setCaptchaHeaders(req *http.Request) {
	req.Header.Set("User-Agent", volgaAuthUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	// Explicit gzip keeps encoded AND decoded sizes under our own bound.
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Sec-GPC", "1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Cache-Control", "no-cache")
}

func buildCaptchaFingerprint(nonceHex, userAgent string) map[string]interface{} {
	return map[string]interface{}{
		"b6": 8, "b7": 8, "b9": []string{"en-US", "en"},
		"c2": "", "c4": "MacIntel", "c5": []interface{}{}, "c9": userAgent,
		"f4": 1080, "f5": 1920, "f6": 24, "f7": 1080, "f8": true,
		"f9": []int{1920, 1080}, "g1": 1920,
		"g2": "Europe/Moscow", "g3": -180,
		"j5": true,
		"m2": map[string]interface{}{"mTP": 0, "tE": false, "tS": false},
		"n6": false,
		"o2": 0, "o3": "srgb", "o4": 0, "o5": "en-US",
		"o8": nil, "o9": nil,
		"p1": nil, "p2": 0, "p3": nil, "p4": nil,
		"p5": nil, "p6": nil, "p8": []interface{}{}, "p9": "111111111",
		"j6": 48000,
		"a1": "",
		"a2": map[string]interface{}{"w": false, "d": ""},
		"a3": map[string]interface{}{
			"acos": 1.4444399284962483, "asin": 0.12349655394506357,
			"atan": 0.4636476090008061, "cos": -0.8390715290095377,
			"exp": 2.718281828459045, "log1p": 2.3978952727983707,
			"sin": -0.9917788534431158, "tan": -0.23206847684369653,
		},
		"a4": map[string]interface{}{"minDelta": 0.1, "maxDelta": 1.2},
		"a5": nil,
		"k4": []interface{}{},
		"j1": map[string]interface{}{
			"vn": "WebKit", "vr": "WebKit WebGL", "vU": "",
			"r": "Mozilla", "rU": "", "sLV": "WebGL GLSL ES 1.0 (1.0)",
		},
		"j2": map[string]interface{}{
			"cA": []interface{}{}, "p": []interface{}{}, "sP": []interface{}{},
			"e": []interface{}{}, "eP": []interface{}{},
		},
		"m10":     nonceHex,
		"version": "1.5.0",
	}
}
