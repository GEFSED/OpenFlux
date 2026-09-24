package yandex

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openflux/utils"
)

const fixtureOrigin = "https://docs.yandex.ru"
const fixtureDoc = fixtureOrigin + "/fixture-doc?private=SENTINEL_DOCUMENT"
const fixtureChallenge = fixtureOrigin + "/showcaptchafast?state=SENTINEL_CHALLENGE"

type captchaRoundTrip func(*http.Request) (*http.Response, error)

func (f captchaRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type countedCaptchaBody struct {
	io.Reader
	closes *int
}

func (b countedCaptchaBody) Close() error { *b.closes++; return nil }

func fixtureCaptcha() string {
	return `<script>window.__SSR_DATA__=JSON.parse(atob("` + base64.StdEncoding.EncodeToString([]byte(`{"uniqueKey":"SENTINEL_STATE","pow":{"complexity":0,"prefix":"aabb"}}`)) + `"))</script><form id="tmgrdfrend-form" action="/checkcaptchafast?state=SENTINEL_STATE&amp;x=1"></form>`
}
func fixtureBootstrap() string {
	return `<script id="client-config">{"SENTINEL_JSON_KEY":"SENTINEL_JSON_VALUE","officeActionData":{"action_url":"` + fixtureOrigin + `/auth","access_token":"SENTINEL_ACCESS_TOKEN","access_token_ttl":12345},"editorParams":{"idDoc":"SENTINEL_DOCUMENT_ID"}}</script>`
}
func fixtureSessionLocation() string {
	q := url.Values{"token": {"SENTINEL_SESSION_TOKEN"}, "request-path": {"SENTINEL_REQUEST_PATH"}, "json": {`{"sessionId":"SENTINEL_SESSION","userId":1,"xiva":{"sign":"SENTINEL_SIGN","ts":"1","user":"1"}}`}}
	return fixtureOrigin + "/session?" + q.Encode()
}

type captchaStep struct {
	method, path string
	status       int
	body         string
	headers      http.Header
	check        func(*testing.T, *http.Request)
	err          error
}

func redirectStep(path, location string) captchaStep {
	return captchaStep{method: "GET", path: path, status: 302, headers: http.Header{"Location": {location}}}
}
func challengeSteps() []captchaStep {
	return []captchaStep{
		redirectStep("/fixture-doc", fixtureChallenge),
		{method: "GET", path: "/showcaptchafast", status: 200, body: fixtureCaptcha()},
		{method: "POST", path: "/checkcaptchafast", status: 302, headers: http.Header{"Location": {fixtureDoc}}},
	}
}
func normalSteps() []captchaStep {
	return []captchaStep{
		{method: "GET", path: "/fixture-doc", status: 200, body: fixtureBootstrap(), check: func(t *testing.T, r *http.Request) {
			if r.URL.String() != fixtureDoc || r.Header.Get("Accept-Language") != "ru-RU,ru;q=0.9" {
				t.Fatal("original document/headers changed")
			}
		}},
		{method: "POST", path: "/auth", status: 302, headers: http.Header{"Location": {fixtureSessionLocation()}}, check: func(t *testing.T, r *http.Request) {
			if r.ParseForm() != nil || r.Form.Get("access_token") != "SENTINEL_ACCESS_TOKEN" || r.Form.Get("access_token_ttl") != "12345" || r.Referer() != fixtureDoc || r.Header.Get("Origin") != "https://disk.yandex.ru" {
				t.Fatal("legacy authorization contract changed")
			}
		}},
		{method: "GET", path: "/session", status: 200},
	}
}
func runCaptchaSteps(t *testing.T, steps []captchaStep, wantError string) *volgaAuth {
	t.Helper()
	client := newVolgaAuthClient()
	defer client.CloseIdleConnections()
	index, bodies, closes := 0, 0, 0
	client.Transport = captchaRoundTrip(func(r *http.Request) (*http.Response, error) {
		if index >= len(steps) {
			t.Fatal("unexpected request: retry/fallback beyond script")
		}
		s := steps[index]
		index++
		if r.Method != s.method || r.URL.Path != s.path || r.Header.Get("User-Agent") != volgaAuthUserAgent {
			t.Fatal("unexpected request class or UA")
		}
		if s.check != nil {
			s.check(t, r)
		}
		if s.err != nil {
			return nil, s.err
		}
		bodies++
		if s.headers == nil {
			s.headers = make(http.Header)
		}
		return &http.Response{StatusCode: s.status, Header: s.headers, Body: countedCaptchaBody{strings.NewReader(s.body), &closes}, Request: r}, nil
	})
	a, err := authorizeWithClient(context.Background(), fixtureDoc, client)
	if wantError == "" && err != nil {
		t.Fatalf("unexpected safe error: %v", err)
	}
	if wantError != "" && (err == nil || !strings.Contains(err.Error(), wantError)) {
		t.Fatalf("expected error class %q, got %v", wantError, err)
	}
	if index != len(steps) || closes != bodies {
		t.Fatalf("requests %d/%d, closed %d/%d", index, len(steps), closes, bodies)
	}
	return a
}

func TestCaptchaAuthorizationFlows(t *testing.T) {
	t.Run("normal_legacy_unchanged", func(t *testing.T) {
		a := runCaptchaSteps(t, normalSteps(), "")
		if a.Token != "SENTINEL_SESSION_TOKEN" || a.Sign != "SENTINEL_SIGN" || a.DocID != "SENTINEL_DOCUMENT_ID" {
			t.Fatal("legacy result changed")
		}
	})
	t.Run("first_redirect_challenge_retry_original", func(t *testing.T) { runCaptchaSteps(t, append(challengeSteps(), normalSteps()...), "") })
	t.Run("relative_challenge_redirect", func(t *testing.T) {
		s := challengeSteps()
		s[0].headers.Set("Location", "/showcaptchafast?state=SENTINEL_CHALLENGE")
		runCaptchaSteps(t, append(s, normalSteps()...), "")
	})
	t.Run("solver_parse_failure", func(t *testing.T) {
		s := challengeSteps()[:2]
		s[1].body = "malformed"
		runCaptchaSteps(t, s, "captcha bootstrap missing")
	})
	t.Run("repeated_challenge", func(t *testing.T) {
		runCaptchaSteps(t, append(challengeSteps(), redirectStep("/fixture-doc", fixtureChallenge)), "captcha attempt limit")
	})
	t.Run("unrelated_redirect_marker_in_query", func(t *testing.T) {
		s := normalSteps()
		s[0].path = "/other"
		s[0].check = nil
		s[1].check = nil
		runCaptchaSteps(t, append([]captchaStep{redirectStep("/fixture-doc", "/other?x=showcaptchafast")}, s...), "")
	})
	t.Run("missing_location", func(t *testing.T) {
		runCaptchaSteps(t, []captchaStep{redirectStep("/fixture-doc", "")}, "without Location")
	})
	t.Run("malformed_location", func(t *testing.T) {
		runCaptchaSteps(t, []captchaStep{redirectStep("/fixture-doc", "https://%")}, "document request failed")
	})
	t.Run("redirect_bound", func(t *testing.T) {
		var s []captchaStep
		for range maxVolgaBootstrapRequests {
			s = append(s, redirectStep("/fixture-doc", fixtureDoc))
		}
		runCaptchaSteps(t, s, "too many document redirects")
	})
	t.Run("no_solve_when_retry_budget_exhausted", func(t *testing.T) {
		var s []captchaStep
		for range maxVolgaBootstrapRequests - 1 {
			s = append(s, redirectStep("/fixture-doc", fixtureDoc))
		}
		s = append(s, redirectStep("/fixture-doc", fixtureChallenge))
		runCaptchaSteps(t, s, "too many document redirects")
	})
	t.Run("cookie_continuity", func(t *testing.T) {
		s := challengeSteps()
		s[0].headers.Set("Set-Cookie", "before=SENTINEL_COOKIE; Path=/; Secure")
		s[1].check = func(t *testing.T, r *http.Request) {
			if c, e := r.Cookie("before"); e != nil || c.Value != "SENTINEL_COOKIE" {
				t.Fatal("redirect cookie lost")
			}
		}
		s[2].headers.Set("Set-Cookie", "passed=SENTINEL_PASSED; Path=/; Secure")
		s[2].check = func(t *testing.T, r *http.Request) {
			if _, e := r.Cookie("before"); e != nil {
				t.Fatal("challenge cookie lost")
			}
			verifyFingerprint(t, r)
		}
		n := normalSteps()
		n[0].check = func(t *testing.T, r *http.Request) {
			for _, name := range []string{"before", "passed"} {
				if _, e := r.Cookie(name); e != nil {
					t.Fatal("retry cookie lost")
				}
			}
			if r.URL.String() != fixtureDoc {
				t.Fatal("did not retry original")
			}
		}
		runCaptchaSteps(t, append(s, n...), "")
	})
	t.Run("oversized_challenge", func(t *testing.T) {
		s := challengeSteps()[:2]
		s[1].body = strings.Repeat("x", maxCaptchaBody+1)
		runCaptchaSteps(t, s, "too large")
	})
	t.Run("compressed_challenge", func(t *testing.T) {
		s := challengeSteps()
		s[1].body = gzipFixture(s[1].body)
		s[1].headers = http.Header{"Content-Encoding": {"gzip"}}
		runCaptchaSteps(t, append(s, normalSteps()...), "")
	})
	t.Run("compressed_expansion_bound", func(t *testing.T) {
		s := challengeSteps()[:2]
		s[1].body = gzipFixture(strings.Repeat("x", maxCaptchaBody+1))
		s[1].headers = http.Header{"Content-Encoding": {"gzip"}}
		runCaptchaSteps(t, s, "too large")
	})
	t.Run("malformed_compression", func(t *testing.T) {
		s := challengeSteps()[:2]
		s[1].headers = http.Header{"Content-Encoding": {"gzip"}}
		runCaptchaSteps(t, s, "invalid authorization compression")
	})
	t.Run("unsupported_compression", func(t *testing.T) {
		s := challengeSteps()[:2]
		s[1].headers = http.Header{"Content-Encoding": {"br"}}
		runCaptchaSteps(t, s, "unsupported authorization compression")
	})
	t.Run("post_303_accepted", func(t *testing.T) {
		s := challengeSteps()
		s[2].status = 303
		runCaptchaSteps(t, append(s, normalSteps()...), "")
	})
	for _, status := range []int{200, 201, 204, 301, 304, 307, 308, 400, 500} {
		t.Run("post_status_"+http.StatusText(status), func(t *testing.T) {
			s := challengeSteps()
			s[2].status = status
			runCaptchaSteps(t, s, "submission status rejected")
		})
	}
	for _, status := range []int{201, 302, 403, 500} {
		t.Run("get_status_"+http.StatusText(status), func(t *testing.T) {
			s := challengeSteps()[:2]
			s[1].status = status
			runCaptchaSteps(t, s, "page status rejected")
		})
	}
	t.Run("post_missing_location", func(t *testing.T) {
		s := challengeSteps()
		s[2].headers.Del("Location")
		runCaptchaSteps(t, s, "completion redirect")
	})
	t.Run("cross_origin_form_rejected", func(t *testing.T) {
		s := challengeSteps()[:2]
		s[1].body = strings.ReplaceAll(s[1].body, `action="/checkcaptchafast`, `action="https://untrusted.invalid/checkcaptchafast`)
		runCaptchaSteps(t, s, "form origin rejected")
	})
	t.Run("body_closed_on_auth_failure", func(t *testing.T) {
		s := normalSteps()[:2]
		s[1].status = 403
		runCaptchaSteps(t, s, "auth/initial status")
	})
	t.Run("transport_error_redacted", func(t *testing.T) {
		s := challengeSteps()[:2]
		s[1].err = errors.New("SENTINEL_TRANSPORT https://private.invalid/secret")
		runCaptchaSteps(t, s, "captcha request failed")
	})
}

func gzipFixture(s string) string {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}
func verifyFingerprint(t *testing.T, r *http.Request) {
	t.Helper()
	if r.ParseForm() != nil || r.Form.Get("version") != "1.5.0" || r.Form.Get("uniquekey") != "SENTINEL_STATE" || r.Form.Get("chstate") != "ok" {
		t.Fatal("upstream form contract changed")
	}
	encoded := r.Form.Get("fingerprint")
	if !strings.HasPrefix(encoded, "~") || !strings.HasSuffix(encoded, "~") {
		t.Fatal("fingerprint framing")
	}
	b, err := base64.StdEncoding.DecodeString(strings.Trim(encoded, "~"))
	if err != nil {
		t.Fatal("fingerprint base64")
	}
	z, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal("fingerprint gzip")
	}
	defer z.Close()
	var fp map[string]interface{}
	if json.NewDecoder(z).Decode(&fp) != nil || fp["c9"] != volgaAuthUserAgent || fp["version"] != "1.5.0" {
		t.Fatal("fingerprint contract")
	}
	nonce, err := hex.DecodeString(fp["m10"].(string))
	if err != nil || len(nonce) != 16 {
		t.Fatal("nonce contract")
	}
}

func TestCaptchaPoWAndParserBounds(t *testing.T) {
	t.Run("valid_work", func(t *testing.T) {
		n, err := solveCaptchaPoW(context.Background(), "aabb", 8)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := hex.DecodeString(n)
		sum := sha256.Sum256(append(b, 0xaa, 0xbb))
		if !captchaCheckComplexity(sum[:], 8) {
			t.Fatal("invalid proof")
		}
	})
	t.Run("literal_prefix", func(t *testing.T) {
		if _, e := solveCaptchaPoW(context.Background(), "literal-prefix", 0); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("work_cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, e := solveCaptchaPoW(ctx, "aabb", 24); !errors.Is(e, context.Canceled) {
			t.Fatal("work did not cancel")
		}
	})
	t.Run("work_deadline", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		if _, e := solveCaptchaPoW(ctx, "aabb", 24); !errors.Is(e, context.DeadlineExceeded) {
			t.Fatal("work deadline ignored")
		}
	})
	t.Run("complexity_full_hash_no_panic", func(t *testing.T) {
		if !captchaCheckComplexity(make([]byte, 32), 256) || captchaCheckComplexity(make([]byte, 32), 257) {
			t.Fatal("bit bounds")
		}
	})
	for _, bits := range []int{-1, 25, 256} {
		t.Run("work_reject_"+string(rune('A'+bits+1)), func(t *testing.T) {
			if _, e := solveCaptchaPoW(context.Background(), "aabb", bits); e == nil {
				t.Fatal("complexity cap ignored")
			}
		})
	}
	t.Run("parser_rejects_huge_ssr", func(t *testing.T) {
		u, _ := url.Parse(fixtureChallenge)
		body := []byte(`window.__SSR_DATA__=JSON.parse(atob("` + strings.Repeat("A", base64.StdEncoding.EncodedLen(maxCaptchaSSR)+4) + `"))`)
		if _, _, e := parseVolgaCaptcha(body, u); e == nil {
			t.Fatal("SSR cap ignored")
		}
	})
	t.Run("parser_rejects_invalid_json", func(t *testing.T) {
		u, _ := url.Parse(fixtureChallenge)
		body := []byte(`window.__SSR_DATA__=JSON.parse(atob("` + base64.StdEncoding.EncodeToString([]byte(`{"uniqueKey":true}`)) + `"))`)
		if _, _, e := parseVolgaCaptcha(body, u); e == nil {
			t.Fatal("invalid SSR accepted")
		}
	})
	t.Run("exact_challenge_path_and_host", func(t *testing.T) {
		for _, s := range []string{"https://evil.invalid/showcaptchafast", "https://docs.yandex.ru.evil.invalid/showcaptchafast", "http://docs.yandex.ru/showcaptchafast", "https://docs.yandex.ru/other?showcaptchafast=1", "https://docs.yandex.ru/showcaptchafast/extra"} {
			u, _ := url.Parse(s)
			if isVolgaCaptcha(u) {
				t.Fatal("overbroad recognition")
			}
		}
	})
}

func TestCaptchaCancellationAndSession(t *testing.T) {
	t.Run("HTTP_client_timeout", func(t *testing.T) {
		client := newVolgaAuthClient()
		client.Timeout = 20 * time.Millisecond
		var calls atomic.Int32
		client.Transport = captchaRoundTrip(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		u, _ := url.Parse(fixtureChallenge)
		start := time.Now()
		if _, err := solveVolgaCaptcha(context.Background(), client, u); err == nil || calls.Load() != 1 || time.Since(start) > time.Second {
			t.Fatal("HTTP timeout ignored or retried")
		}
	})
	for _, stage := range []string{"GET", "POST"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := newVolgaAuthClient()
			calls := 0
			client.Transport = captchaRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method == stage {
					cancel()
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fixtureCaptcha())), Request: r}, nil
			})
			u, _ := url.Parse(fixtureChallenge)
			if _, e := solveVolgaCaptcha(ctx, client, u); !errors.Is(e, context.Canceled) {
				t.Fatal("request cancellation not propagated")
			}
			if calls > 2 {
				t.Fatal("retried")
			}
		})
	}
	t.Run("finite_client_timeout", func(t *testing.T) {
		client := newVolgaAuthClient()
		if client.Timeout != 30*time.Second {
			t.Fatal("HTTP timeout changed")
		}
		if client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
			t.Fatal("implicit redirects enabled")
		}
	})
	t.Run("no_implicit_default_transport", func(t *testing.T) {
		client := newVolgaAuthClient()
		client.Transport = nil
		u, _ := url.Parse(fixtureChallenge)
		if _, e := solveVolgaCaptcha(context.Background(), client, u); e == nil {
			t.Fatal("default transport permitted")
		}
	})
}

type captchaReadFailure struct{}

func (captchaReadFailure) Read([]byte) (int, error) { return 0, errors.New("SENTINEL_BODY_FAILURE") }

func TestCaptchaReadAndRedirectBounds(t *testing.T) {
	cases := []struct {
		name, encoding, data string
		failRead             bool
		wantErr              bool
	}{
		{name: "read_error", failRead: true, wantErr: true},
		{name: "encoded_cap", data: strings.Repeat("x", maxCaptchaBody+1), wantErr: true},
		{name: "exact_limit", data: strings.Repeat("x", maxCaptchaBody)},
		{name: "identity", encoding: "identity", data: "fixture"},
		{name: "gzip_trailer_error", encoding: "gzip", data: gzipFixture("fixture")[:len(gzipFixture("fixture"))-1], wantErr: true},
		{name: "gzip_multistream_cap", encoding: "gzip", data: gzipFixture(strings.Repeat("x", maxCaptchaBody/2+1)) + gzipFixture(strings.Repeat("x", maxCaptchaBody/2)), wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			closes := 0
			var reader io.Reader = strings.NewReader(c.data)
			if c.failRead {
				reader = captchaReadFailure{}
			}
			resp := &http.Response{Header: http.Header{"Content-Encoding": {c.encoding}}, Body: countedCaptchaBody{reader, &closes}}
			_, err := readAuthBody(resp, maxCaptchaBody)
			if (err != nil) != c.wantErr || closes != 1 {
				t.Fatal("read bound/closure violated")
			}
			if err != nil && strings.Contains(err.Error(), "SENTINEL") {
				t.Fatal("body error leaked")
			}
		})
	}
	t.Run("invalid_redirect_targets", func(t *testing.T) {
		for _, target := range []string{"", "https://%", "file:///private", "javascript:state", "https://user:password@docs.yandex.ru/showcaptchafast"} {
			if _, err := resolveAuthRedirect(fixtureDoc, target); err == nil {
				t.Fatal("invalid redirect accepted")
			}
		}
	})
	t.Run("no_captcha_status_policy_preserved", func(t *testing.T) {
		// Existing legacy auth parses any non-redirect body; the selective port
		// intentionally does not copy upstream's unrelated final-200-only change.
		s := normalSteps()
		s[0].status = 403
		runCaptchaSteps(t, s, "")
	})
	t.Run("normal_android_read_policy_preserved", func(t *testing.T) {
		s := normalSteps()[:1]
		s = normalSteps()
		s[0].body = fixtureBootstrap() + strings.Repeat(" ", (2<<20)+1)
		runCaptchaSteps(t, s, "")
	})
}

func TestCaptchaPrivacy(t *testing.T) {
	var logs bytes.Buffer
	var logsMu sync.Mutex
	utils.SetLogSink(func(message string) {
		logsMu.Lock()
		defer logsMu.Unlock()
		logs.WriteString(message)
		logs.WriteByte('\n')
	})
	t.Cleanup(func() {
		utils.SetDebug(false)
		utils.SetLogSink(nil)
	})
	utils.SetDebug(true)
	privateSteps := challengeSteps()
	privateSteps[0].headers.Set("Set-Cookie", "fixture=SENTINEL_COOKIE; Path=/; Secure")
	privateSteps[2].headers.Set("Set-Cookie", "passed=SENTINEL_SESSION_COOKIE; Path=/; Secure")
	runCaptchaSteps(t, append(privateSteps, normalSteps()...), "")
	s := challengeSteps()[:2]
	s[1].err = errors.New("SENTINEL_FAILURE https://private.invalid/doc?token=SENTINEL_TOKEN")
	runCaptchaSteps(t, s, "captcha request failed")
	logsMu.Lock()
	output := logs.String()
	logsMu.Unlock()
	if strings.Contains(output, "SENTINEL") || strings.Contains(output, "https://") || strings.Contains(output, "12345") {
		t.Fatal("private value leaked")
	}
	for _, event := range []string{"CAPTCHA_REDIRECT_DETECTED", "CAPTCHA_FLOW_STARTED", "CAPTCHA_FLOW_COMPLETED", "CAPTCHA_FLOW_FAILED", "AUTH_RETRY_STARTED"} {
		if !strings.Contains(output, event) {
			t.Errorf("missing safe event %s", event)
		}
	}
}

// Real local TLS sockets exercise the exact production client factory and its
// shared HTTP transport, including challenge GET/POST and the original retry.
// Only this synthetic test redirects sockets to loopback; Android production
// uses the unchanged process-level VpnService package exclusion.
func TestCaptchaSharedHTTPTransportAndNoFallback(t *testing.T) {
	steps := append(challengeSteps(), normalSteps()...)
	var paths []string
	var mu sync.Mutex
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		i := len(paths)
		paths = append(paths, r.URL.Path)
		if i >= len(steps) || steps[i].method != r.Method || steps[i].path != r.URL.Path {
			t.Error("unexpected network request")
			w.WriteHeader(500)
			return
		}
		s := steps[i]
		for k, v := range s.headers {
			w.Header()[k] = v
		}
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, s.body)
	}))
	defer srv.Close()
	var dials atomic.Int32
	client := newVolgaAuthClient()
	tr := client.Transport.(*http.Transport)
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		if address != "docs.yandex.ru:443" {
			return nil, errors.New("unapproved synthetic destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	tr.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	tr.TLSClientConfig.ServerName = srv.Certificate().DNSNames[0]
	tr.DisableKeepAlives = true
	defer client.CloseIdleConnections()
	if _, e := authorizeWithClient(context.Background(), fixtureDoc, client); e != nil {
		t.Fatal(e)
	}
	if dials.Load() != int32(len(steps)) {
		t.Fatal("a request bypassed the injected dialer")
	}
	mu.Lock()
	got := append([]string(nil), paths...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{"/fixture-doc", "/showcaptchafast", "/checkcaptchafast", "/fixture-doc", "/auth", "/session"}) {
		t.Fatal("unexpected path order")
	}
	// Failed injected dial must propagate without a default-network retry.
	dials.Store(0)
	client2 := newVolgaAuthClient()
	client2.Transport.(*http.Transport).DialContext = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("SENTINEL_DIAL")
	}
	defer client2.CloseIdleConnections()
	u, _ := url.Parse(fixtureChallenge)
	if _, e := solveVolgaCaptcha(context.Background(), client2, u); e == nil || dials.Load() != 1 {
		t.Fatal("dial failure bypassed or retried")
	}
}

func TestCaptchaAndroidClientPolicyUnchanged(t *testing.T) {
	client := newVolgaAuthClient()
	defer client.CloseIdleConnections()
	tr := client.Transport.(*http.Transport)
	if tr.DialContext != nil || tr.Proxy != nil || tr.MaxIdleConns != 100 || tr.MaxIdleConnsPerHost != 100 || tr.IdleConnTimeout != 90*time.Second || client.Timeout != 30*time.Second || client.Jar == nil {
		t.Fatal("Android authorization client policy changed")
	}
	if client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("automatic redirects enabled")
	}
}
