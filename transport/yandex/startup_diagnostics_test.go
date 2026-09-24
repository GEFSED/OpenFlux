package yandex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"openflux/utils"
)

func captureVolgaStartup(t *testing.T) func() []VolgaStartupEvent {
	t.Helper()
	var mu sync.Mutex
	var events []VolgaStartupEvent
	utils.SetDebug(false)
	SetVolgaStartupSink(func(e VolgaStartupEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	t.Cleanup(func() { SetVolgaStartupSink(nil) })
	return func() []VolgaStartupEvent {
		mu.Lock()
		defer mu.Unlock()
		return append([]VolgaStartupEvent(nil), events...)
	}
}

func assertSafeStartupEvents(t *testing.T, got, want []VolgaStartupEvent) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events: got %v, want %v", got, want)
	}
	for _, event := range got {
		output := event.String() + " " + event.FailureClass()
		for _, denied := range []string{"SENTINEL", "https://", "http://", "Cookie", "Bearer", "<script", "12345"} {
			if strings.Contains(output, denied) {
				t.Fatal("private input leaked through safe diagnostics")
			}
		}
	}
	if utils.IsVerbose() {
		t.Fatal("provider debug enabled")
	}
}

func TestVolgaStartupDiagnosticsPrivacyAndStages(t *testing.T) {
	config := func(body string) []captchaStep {
		return []captchaStep{{method: "GET", path: "/fixture-doc", status: 200, body: body}}
	}
	wrap := func(body string) string { return `<script id="client-config">` + body + `</script>` }
	initial := func(status int, location string, err error) []captchaStep {
		s := normalSteps()[:2]
		s[1].status, s[1].err = status, err
		s[1].headers.Set("Location", location)
		return s
	}
	var loop []captchaStep
	for range maxVolgaBootstrapRequests {
		loop = append(loop, redirectStep("/fixture-doc", fixtureDoc))
	}
	failedCaptcha := challengeSteps()[:2]
	failedCaptcha[1].body = "SENTINEL_BODY <html>private challenge state</html>"
	cookieSteps := append(challengeSteps(), normalSteps()...)
	cookieSteps[0].headers.Set("Set-Cookie", "before=SENTINEL_COOKIE; Path=/; Secure")
	cookieSteps[2].headers.Set("Set-Cookie", "passed=SENTINEL_PASSED; Path=/; Secure")
	cookieSteps[3].check = func(t *testing.T, r *http.Request) {
		for _, name := range []string{"before", "passed"} {
			if _, err := r.Cookie(name); err != nil {
				t.Fatal("cookie continuity changed")
			}
		}
	}
	cases := []struct {
		name   string
		steps  []captchaStep
		err    string
		events []VolgaStartupEvent
	}{
		{"normal", normalSteps(), "", []VolgaStartupEvent{VolgaAuthSuccess}},
		{"captcha", append(challengeSteps(), normalSteps()...), "", []VolgaStartupEvent{VolgaCaptchaDetected, VolgaCaptchaStarted, VolgaCaptchaCompleted, VolgaAuthRetry, VolgaAuthSuccess}},
		{"captcha_cookies", cookieSteps, "", []VolgaStartupEvent{VolgaCaptchaDetected, VolgaCaptchaStarted, VolgaCaptchaCompleted, VolgaAuthRetry, VolgaAuthSuccess}},
		{"captcha_failed", failedCaptcha, "captcha bootstrap missing", []VolgaStartupEvent{VolgaCaptchaDetected, VolgaCaptchaStarted, VolgaCaptchaFailed}},
		{"repeated_captcha", append(challengeSteps(), redirectStep("/fixture-doc", fixtureChallenge)), "captcha attempt limit", []VolgaStartupEvent{VolgaCaptchaDetected, VolgaCaptchaStarted, VolgaCaptchaCompleted, VolgaAuthRetry, VolgaCaptchaDetected, VolgaCaptchaFailed}},
		{"request", []captchaStep{{method: "GET", path: "/fixture-doc", err: errors.New("SENTINEL_PROVIDER https://private.invalid/?token=SENTINEL")}}, "document request failed", []VolgaStartupEvent{VolgaDocumentRequestFailed}},
		{"missing_location", []captchaStep{redirectStep("/fixture-doc", "")}, "without Location", []VolgaStartupEvent{VolgaRedirectRejected}},
		{"rejected_redirect", []captchaStep{redirectStep("/fixture-doc", "ftp://private.invalid/SENTINEL")}, "invalid authorization redirect", []VolgaStartupEvent{VolgaRedirectRejected}},
		// net/http rejects malformed Location internally before returning a response.
		// This is a client request failure, not an observed resolveAuthRedirect branch.
		{"client_redirect_parse", []captchaStep{redirectStep("/fixture-doc", "https://%")}, "document request failed", []VolgaStartupEvent{VolgaDocumentRequestFailed}},
		{"redirect_budget", loop, "too many document redirects", []VolgaStartupEvent{VolgaRedirectRejected}},
		{"captcha_at_budget", append(append([]captchaStep(nil), loop[:len(loop)-1]...), redirectStep("/fixture-doc", fixtureChallenge)), "too many document redirects", []VolgaStartupEvent{VolgaCaptchaDetected, VolgaRedirectRejected}},
		{"config_missing", config("<html>SENTINEL_BODY</html>"), "client-config not found", []VolgaStartupEvent{VolgaClientConfigMissing}},
		{"config_invalid", config(wrap("SENTINEL_INVALID_JSON")), "invalid client-config JSON", []VolgaStartupEvent{VolgaClientConfigInvalid}},
		{"office_missing", config(wrap(`{"SENTINEL_USER_ID":"SENTINEL_SESSION_ID"}`)), "officeActionData missing", []VolgaStartupEvent{VolgaOfficeActionMissing}},
		{"action_missing", config(strings.ReplaceAll(fixtureBootstrap(), fixtureOrigin+"/auth", "")), "action_url missing", []VolgaStartupEvent{VolgaActionURLMissing}},
		{"token_missing", config(strings.ReplaceAll(fixtureBootstrap(), "SENTINEL_ACCESS_TOKEN", "")), "access_token missing", []VolgaStartupEvent{VolgaAccessTokenMissing}},
		{"initial_request", initial(302, fixtureSessionLocation(), errors.New("SENTINEL_PROVIDER_TOKEN")), "document authorization request failed", []VolgaStartupEvent{VolgaAuthInitialFailed}},
		{"initial_status", initial(403, "", nil), "auth/initial status", []VolgaStartupEvent{VolgaAuthInitialFailed}},
		{"initial_location_missing", initial(302, "", nil), "auth/initial no Location", []VolgaStartupEvent{VolgaAuthInitialFailed}},
		{"initial_error_route", initial(302, fixtureOrigin+"/document/error/SENTINEL", nil), "auth/initial returned", []VolgaStartupEvent{VolgaAuthInitialFailed}},
		{"session_json_missing", initial(302, fixtureOrigin+"/session?token=SENTINEL", nil), "no json in Location", []VolgaStartupEvent{VolgaSessionFailed}},
		{"session_json_invalid", initial(302, fixtureOrigin+"/session?json=SENTINEL", nil), "invalid authorization redirect JSON", []VolgaStartupEvent{VolgaSessionFailed}},
	}
	sessionFailure := normalSteps()
	sessionFailure[2].err = errors.New("SENTINEL_SESSION https://private.invalid")
	cases = append(cases, struct {
		name   string
		steps  []captchaStep
		err    string
		events []VolgaStartupEvent
	}{"session_request", sessionFailure, "document session request failed", []VolgaStartupEvent{VolgaSessionFailed}})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			SetVolgaStartupSink(nil)
			baseline := runCaptchaSteps(t, tc.steps, tc.err)
			read := captureVolgaStartup(t)
			observed := runCaptchaSteps(t, tc.steps, tc.err)
			// Both runs already assert exact HTTP sequence/count, headers/forms and
			// body closure. Compare auth results too, excluding the distinct clients.
			if baseline != nil {
				baseline.Session = nil
			}
			if observed != nil {
				observed.Session = nil
			}
			if !reflect.DeepEqual(baseline, observed) {
				t.Fatal("observer changed authorization result")
			}
			assertSafeStartupEvents(t, read(), append([]VolgaStartupEvent{VolgaAuthStart}, tc.events...))
		})
	}
}

type startupErrorReader struct{}

func (startupErrorReader) Read([]byte) (int, error) { return 0, errors.New("SENTINEL_BODY_READ_TOKEN") }

func TestVolgaStartupDiagnosticsRequestConstructionAndBody(t *testing.T) {
	for _, malformed := range []bool{true, false} {
		t.Run(map[bool]string{true: "construction", false: "body_read"}[malformed], func(t *testing.T) {
			read := captureVolgaStartup(t)
			client := newVolgaAuthClient()
			defer client.CloseIdleConnections()
			calls, closes := 0, 0
			client.Transport = captchaRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: countedCaptchaBody{startupErrorReader{}, &closes}, Request: r}, nil
			})
			doc := fixtureDoc
			if malformed {
				doc = "https://%SENTINEL"
			}
			if _, err := authorizeWithClient(context.Background(), doc, client); err == nil {
				t.Fatal("expected failure")
			}
			if malformed && calls != 0 || !malformed && (calls != 1 || closes != 1) {
				t.Fatal("request/body lifecycle changed")
			}
			assertSafeStartupEvents(t, read(), []VolgaStartupEvent{VolgaAuthStart, VolgaDocumentRequestFailed})
		})
	}
}

func TestVolgaStartupDiagnosticsAllowlist(t *testing.T) {
	names := []string{"VOLGA_AUTH_START", "VOLGA_DOCUMENT_REQUEST_FAILED", "VOLGA_REDIRECT_REJECTED", "VOLGA_CAPTCHA_DETECTED", "VOLGA_CAPTCHA_STARTED", "VOLGA_CAPTCHA_COMPLETED", "VOLGA_CAPTCHA_FAILED", "VOLGA_AUTH_RETRY", "VOLGA_CLIENT_CONFIG_MISSING", "VOLGA_CLIENT_CONFIG_INVALID", "VOLGA_OFFICE_ACTION_MISSING", "VOLGA_ACTION_URL_MISSING", "VOLGA_ACCESS_TOKEN_MISSING", "VOLGA_AUTH_INITIAL_FAILED", "VOLGA_SESSION_FAILED", "VOLGA_AUTH_SUCCESS"}
	classes := []string{"", "AUTH_DOCUMENT_REQUEST", "AUTH_REDIRECT", "", "", "", "AUTH_CAPTCHA", "", "AUTH_CLIENT_CONFIG", "AUTH_CLIENT_CONFIG", "AUTH_CLIENT_CONFIG", "AUTH_CLIENT_CONFIG", "AUTH_CLIENT_CONFIG", "AUTH_INITIAL", "AUTH_SESSION", ""}
	read := captureVolgaStartup(t)
	for i := 0; i < 256; i++ {
		e := VolgaStartupEvent(i)
		emitVolgaStartup(e)
		if i > 0 && i <= len(names) {
			if e.String() != names[i-1] || e.FailureClass() != classes[i-1] {
				t.Fatal("allowlist changed")
			}
		} else if e.String() != "" || e.FailureClass() != "" {
			t.Fatal("unknown enum exposed")
		}
	}
	if len(read()) != len(names) {
		t.Fatal("invalid event reached sink")
	}
}

func TestVolgaStartupDiagnosticsConcurrentAndPanic(t *testing.T) {
	t.Cleanup(func() { SetVolgaStartupSink(nil) })
	var count atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				SetVolgaStartupSink(func(VolgaStartupEvent) { count.Add(1) })
				emitVolgaStartup(VolgaAuthStart)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 800 {
		t.Fatal("lost event")
	}
	SetVolgaStartupSink(func(VolgaStartupEvent) { SetVolgaStartupSink(nil); panic("SENTINEL_CALLBACK") })
	emitVolgaStartup(VolgaAuthStart)
	// Observer panics cannot change existing auth/captcha request counts or results.
	SetVolgaStartupSink(func(VolgaStartupEvent) { panic("SENTINEL_CALLBACK") })
	runCaptchaSteps(t, append(challengeSteps(), normalSteps()...), "")
}

// Keep io in this test's scope as a compile-time check of the failing body fixture.
var _ io.Reader = startupErrorReader{}
