package mobile

import (
	"strings"
	"sync"
	"testing"

	"openflux/transport/yandex"
	"openflux/utils"
)

func TestVolgaSafeDiagnosticsReachReadLogsWithDebugDisabled(t *testing.T) {
	Stop()
	installSession(nil)
	ReadLogs()
	configureLogging()
	if utils.IsVerbose() {
		t.Fatal("debug enabled")
	}
	// Invalid URL fails before any network I/O while exercising real Standard Start.
	err := StartWithMode("vyandex", "https://%SENTINEL_DOCUMENT", "", "legacy", "", "", "standard")
	if err != "Ошибка транспорта" {
		t.Fatal("generic UI error changed")
	}
	got := ReadLogs()
	for _, want := range []string{"[MODE] mode=standard", "event=VOLGA_AUTH_START", "event=VOLGA_DOCUMENT_REQUEST_FAILED class=AUTH_DOCUMENT_REQUEST", "[ERROR] Ошибка запуска транспорта"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing safe field %s", want)
		}
	}
	assertNoPrivateVolgaLogs(t, got)
	Stop()
	ReadLogs()
}

func assertNoPrivateVolgaLogs(t *testing.T, got string) {
	t.Helper()
	for _, denied := range []string{"SENTINEL", "https://", "http://", "Cookie", "Bearer", "<html", "<script", "sessionId", "userId", "access_token", "challenge-state"} {
		if strings.Contains(got, denied) {
			t.Fatal("private provider value in ReadLogs")
		}
	}
	if utils.IsVerbose() {
		t.Fatal("debug enabled")
	}
}

func TestVolgaSafeDiagnosticsMobileAllowlistAndConcurrency(t *testing.T) {
	ReadLogs()
	var wg sync.WaitGroup
	for i := 0; i < 256; i++ {
		wg.Add(1)
		go func(e yandex.VolgaStartupEvent) { defer wg.Done(); appendVolgaStartupEvent(e) }(yandex.VolgaStartupEvent(i))
	}
	wg.Wait()
	got := ReadLogs()
	if len(strings.Split(got, "\n")) != 16 {
		t.Fatal("unknown event retained or valid event lost")
	}
	for _, want := range []string{"VOLGA_CAPTCHA_DETECTED", "VOLGA_CAPTCHA_STARTED", "VOLGA_CAPTCHA_COMPLETED", "VOLGA_CAPTCHA_FAILED", "VOLGA_AUTH_RETRY", "class=AUTH_CLIENT_CONFIG", "class=AUTH_INITIAL", "class=AUTH_SESSION", "VOLGA_AUTH_SUCCESS"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s", want)
		}
	}
	assertNoPrivateVolgaLogs(t, got)
	if ReadLogs() != "" {
		t.Fatal("ReadLogs drain changed")
	}
}
