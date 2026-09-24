package yandex

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Fixtures are synthetic. Every request goes through runCaptchaSteps' in-memory
// transport, which checks exact order/count, minimal UA and response closure.
func completionBootstrapSteps(t *testing.T, target string) []captchaStep {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal("invalid test fixture")
	}
	s := normalSteps()
	s[0].path = u.Path
	s[0].check = func(t *testing.T, r *http.Request) {
		if r.URL.String() != target || r.Referer() != fixtureDoc || r.Header.Get("Accept-Language") != "ru-RU,ru;q=0.9" {
			t.Fatal("completion URL or unchanged bootstrap headers not used")
		}
	}
	s[1].check = func(t *testing.T, r *http.Request) {
		if r.ParseForm() != nil || r.Form.Get("access_token") != "SENTINEL_ACCESS_TOKEN" || r.Form.Get("access_token_ttl") != "12345" || r.Referer() != target || r.Header.Get("Origin") != "https://disk.yandex.ru" {
			t.Fatal("auth contract or final bootstrap referer changed")
		}
	}
	return s
}

func TestCompletionRedirectStatusAndResolution(t *testing.T) {
	for _, tc := range []struct {
		name, location, target string
		status                 int
	}{
		{"302_absolute", fixtureOrigin + "/completion?state=SENTINEL", fixtureOrigin + "/completion?state=SENTINEL", 302},
		{"303_absolute", fixtureOrigin + "/completion?state=SENTINEL", fixtureOrigin + "/completion?state=SENTINEL", 303},
		{"302_root_relative", "/completion?state=SENTINEL", fixtureOrigin + "/completion?state=SENTINEL", 302},
		{"303_path_relative", "completion?state=SENTINEL", fixtureOrigin + "/completion?state=SENTINEL", 303},
		{"query_relative", "?done=SENTINEL", fixtureOrigin + "/checkcaptchafast?done=SENTINEL", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := challengeSteps()
			s[2].status = tc.status
			s[2].headers.Set("Location", tc.location)
			read := captureVolgaStartup(t)
			runCaptchaSteps(t, requireMinimalAuthUA(append(s, completionBootstrapSteps(t, tc.target)...)), "")
			assertSafeStartupEvents(t, read(), []VolgaStartupEvent{VolgaAuthStart, VolgaCaptchaDetected, VolgaCaptchaStarted, VolgaCaptchaCompleted, VolgaCaptchaCompletionFollowed, VolgaAuthRetry, VolgaAuthSuccess})
		})
	}
}

func TestCompletionRedirectInvalid(t *testing.T) {
	for _, tc := range []struct{ name, location string }{
		{"empty", ""},
		{"malformed", "https://%SENTINEL"},
		{"file", "file:///SENTINEL"},
		{"javascript", "javascript:SENTINEL"},
		{"userinfo", "https://SENTINEL:SENTINEL@docs.yandex.ru/completion"},
		{"missing_host", "https:///SENTINEL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := challengeSteps()
			s[2].headers.Set("Location", tc.location)
			read := captureVolgaStartup(t)
			runCaptchaSteps(t, s, "captcha")
			assertSafeStartupEvents(t, read(), []VolgaStartupEvent{VolgaAuthStart, VolgaCaptchaDetected, VolgaCaptchaStarted, VolgaCaptchaFailed})
		})
	}
}

func TestCompletionRedirectCookies(t *testing.T) {
	const target = fixtureOrigin + "/completion?token=SENTINEL"
	s := challengeSteps()
	s[0].headers.Set("Set-Cookie", "before=SENTINEL_BEFORE; Path=/; Secure")
	s[2].headers.Set("Set-Cookie", "after=SENTINEL_AFTER; Path=/; Secure")
	s[2].headers.Set("Location", target)
	n := completionBootstrapSteps(t, target)
	prior := n[0].check
	n[0].check = func(t *testing.T, r *http.Request) {
		prior(t, r)
		for name, value := range map[string]string{"before": "SENTINEL_BEFORE", "after": "SENTINEL_AFTER"} {
			c, err := r.Cookie(name)
			if err != nil || c.Value != value {
				t.Fatal("completion cookie continuity lost")
			}
		}
	}
	runCaptchaSteps(t, append(s, n...), "")
}

func TestCompletionRedirectBounds(t *testing.T) {
	t.Run("shared_bootstrap_budget", func(t *testing.T) {
		s := challengeSteps()
		s[2].headers.Set("Location", "/completion")
		for range maxVolgaBootstrapRequests - 1 {
			s = append(s, redirectStep("/completion", "/completion"))
		}
		runCaptchaSteps(t, s, "too many document redirects")
	})
	t.Run("no_second_solve", func(t *testing.T) {
		s := challengeSteps()
		s[2].headers.Set("Location", "/completion")
		s = append(s, redirectStep("/completion", fixtureChallenge))
		runCaptchaSteps(t, s, "captcha attempt limit")
	})
	t.Run("solver_returns_without_hidden_get", func(t *testing.T) {
		client := newVolgaAuthClient()
		defer client.CloseIdleConnections()
		s := challengeSteps()[1:]
		s[1].headers.Set("Location", "completion?state=SENTINEL")
		calls, closes := 0, 0
		client.Transport = captchaRoundTrip(func(r *http.Request) (*http.Response, error) {
			if calls >= len(s) {
				t.Fatal("solver issued a hidden completion request")
			}
			step := s[calls]
			calls++
			if r.Method != step.method || r.URL.Path != step.path || r.Header.Get("User-Agent") != "Mozilla/5.0" {
				t.Fatal("solver request sequence or UA changed")
			}
			return &http.Response{StatusCode: step.status, Header: step.headers, Body: countedCaptchaBody{strings.NewReader(step.body), &closes}, Request: r}, nil
		})
		u, _ := url.Parse(fixtureChallenge)
		completion, err := solveVolgaCaptcha(context.Background(), client, u)
		if err != nil || completion == nil || completion.String() != fixtureOrigin+"/completion?state=SENTINEL" || calls != 2 || closes != 2 {
			t.Fatal("typed completion result, request count or closure violated")
		}
	})
}

func TestCompletionRedirectPrivacyOnMissingConfig(t *testing.T) {
	const target = fixtureOrigin + "/SENTINEL_DOCUMENT_ID?token=SENTINEL_TOKEN&sessionId=SENTINEL_SESSION&userId=SENTINEL_USER"
	s := challengeSteps()
	s[2].headers.Set("Location", target)
	s[2].headers.Set("Set-Cookie", "private=SENTINEL_COOKIE; Path=/; Secure")
	s = append(s, captchaStep{method: "GET", path: "/SENTINEL_DOCUMENT_ID", status: 200, body: "<html>SENTINEL_PROVIDER_BODY</html>"})
	read := captureVolgaStartup(t)
	runCaptchaSteps(t, s, "client-config not found")
	assertSafeStartupEvents(t, read(), []VolgaStartupEvent{VolgaAuthStart, VolgaCaptchaDetected, VolgaCaptchaStarted, VolgaCaptchaCompleted, VolgaCaptchaCompletionFollowed, VolgaAuthRetry, VolgaClientConfigMissing})
}
