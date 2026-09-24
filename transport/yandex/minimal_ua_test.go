package yandex

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const priorVolgaUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:153.0) Gecko/20100101 Firefox/153.0"

func requireMinimalAuthUA(steps []captchaStep) []captchaStep {
	for i := range steps {
		priorCheck := steps[i].check
		steps[i].check = func(t *testing.T, r *http.Request) {
			t.Helper()
			if r.Header.Get("User-Agent") != "Mozilla/5.0" {
				t.Fatal("authorization request did not use the literal minimal UA")
			}
			if priorCheck != nil {
				priorCheck(t, r)
			}
			if r.Method == "POST" && r.URL.Path == "/checkcaptchafast" {
				verifyFingerprint(t, r)
				fp := decodeMinimalUAFingerprint(t, r.Form.Get("fingerprint"))
				if fp["c9"] != "Mozilla/5.0" {
					t.Fatal("submitted captcha fingerprint did not use the literal minimal UA")
				}
			}
		}
	}
	return steps
}

func decodeMinimalUAFingerprint(t *testing.T, encoded string) map[string]interface{} {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(strings.Trim(encoded, "~"))
	if err != nil {
		t.Fatal("invalid synthetic fingerprint encoding")
	}
	z, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal("invalid synthetic fingerprint compression")
	}
	defer z.Close()
	var fp map[string]interface{}
	if json.NewDecoder(z).Decode(&fp) != nil {
		t.Fatal("invalid synthetic fingerprint JSON")
	}
	return fp
}

func TestMinimalVolgaAuthUA(t *testing.T) {
	t.Run("separate_auth_and_data_plane_values", func(t *testing.T) {
		if volgaAuthUserAgent != "Mozilla/5.0" || volgaUserAgent != priorVolgaUA {
			t.Fatal("auth experiment escaped its UA boundary")
		}
	})
	t.Run("document_initial_auth_and_session", func(t *testing.T) {
		runCaptchaSteps(t, requireMinimalAuthUA(normalSteps()), "")
	})
	t.Run("challenge_fingerprint_and_original_document_retry", func(t *testing.T) {
		runCaptchaSteps(t, requireMinimalAuthUA(append(challengeSteps(), normalSteps()...)), "")
	})
	t.Run("ordinary_redirect_retains_minimal_ua", func(t *testing.T) {
		steps := normalSteps()
		steps[0].check = nil
		steps[0].path = "/redirected-doc"
		steps[1].check = func(t *testing.T, r *http.Request) {
			if r.Referer() != fixtureOrigin+"/redirected-doc" {
				t.Fatal("redirect referer changed")
			}
		}
		runCaptchaSteps(t, requireMinimalAuthUA(append([]captchaStep{redirectStep("/fixture-doc", "/redirected-doc")}, steps...)), "")
	})
	t.Run("repeated_challenge_has_no_ua_fallback", func(t *testing.T) {
		steps := append(challengeSteps(), redirectStep("/fixture-doc", fixtureChallenge))
		runCaptchaSteps(t, requireMinimalAuthUA(steps), "captcha attempt limit")
	})
	t.Run("missing_config_has_no_ua_fallback", func(t *testing.T) {
		steps := []captchaStep{{method: "GET", path: "/fixture-doc", status: 200, body: "<html></html>"}}
		runCaptchaSteps(t, requireMinimalAuthUA(steps), "client-config not found")
	})
	t.Run("fingerprint_changes_only_c9", func(t *testing.T) {
		const nonce = "00112233445566778899aabbccddeeff"
		before := buildCaptchaFingerprint(nonce, priorVolgaUA)
		after := buildCaptchaFingerprint(nonce, volgaAuthUserAgent)
		if after["c9"] != "Mozilla/5.0" {
			t.Fatal("minimal UA missing from fingerprint")
		}
		after["c9"] = before["c9"]
		if !reflect.DeepEqual(before, after) {
			t.Fatal("non-UA fingerprint fields changed")
		}
	})
}

// These use the real relay request builders with an in-memory transport. No
// provider traffic or workers are started. WS code is byte-frozen by the verifier.
func TestMinimalVolgaAuthUAPreservesRelay(t *testing.T) {
	for _, mode := range []string{"standard", "speed", "optimized"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: captchaRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("User-Agent") != priorVolgaUA {
					t.Fatal("relay UA changed with auth experiment")
				}
				return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			})}
			auth := &volgaAuth{UserID: 1, RequestPath: "SYNTHETIC", Token: "SENTINEL"}
			cfg := DefaultVolgaConfig()
			cfg.RateLimit429GuardEnabled = mode == "optimized"
			batch := [][]byte{[]byte("synthetic packet")}
			var err error
			if mode == "standard" {
				r := &relayClient{auth: auth, config: cfg, stats: &VolgaStats{}, httpClient: client, ctx: context.Background()}
				err = r.sendBatch(batch)
			} else {
				r := &configuredRelayClient{auth: auth, config: cfg, stats: &configuredVolgaStats{}, httpClient: client, ctx: context.Background(), rateLimit: newRelay429Gate(cfg.RateLimit429GuardEnabled)}
				err = r.sendBatch(batch)
			}
			if err != nil || calls != 1 {
				t.Fatal("synthetic relay request failed or retried")
			}
		})
	}
}
