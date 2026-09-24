package yandex

import "sync"

// VolgaStartupEvent carries no provider data. Unknown numeric values are dropped.
// This channel is independent of verbose/debug logging.
type VolgaStartupEvent uint8

const (
	VolgaAuthStart VolgaStartupEvent = iota + 1
	VolgaDocumentRequestFailed
	VolgaRedirectRejected
	VolgaCaptchaDetected
	VolgaCaptchaStarted
	VolgaCaptchaCompleted
	VolgaCaptchaFailed
	VolgaAuthRetry
	VolgaClientConfigMissing
	VolgaClientConfigInvalid
	VolgaOfficeActionMissing
	VolgaActionURLMissing
	VolgaAccessTokenMissing
	VolgaAuthInitialFailed
	VolgaSessionFailed
	VolgaAuthSuccess
)

func (e VolgaStartupEvent) String() string {
	switch e {
	case VolgaAuthStart:
		return "VOLGA_AUTH_START"
	case VolgaDocumentRequestFailed:
		return "VOLGA_DOCUMENT_REQUEST_FAILED"
	case VolgaRedirectRejected:
		return "VOLGA_REDIRECT_REJECTED"
	case VolgaCaptchaDetected:
		return "VOLGA_CAPTCHA_DETECTED"
	case VolgaCaptchaStarted:
		return "VOLGA_CAPTCHA_STARTED"
	case VolgaCaptchaCompleted:
		return "VOLGA_CAPTCHA_COMPLETED"
	case VolgaCaptchaFailed:
		return "VOLGA_CAPTCHA_FAILED"
	case VolgaAuthRetry:
		return "VOLGA_AUTH_RETRY"
	case VolgaClientConfigMissing:
		return "VOLGA_CLIENT_CONFIG_MISSING"
	case VolgaClientConfigInvalid:
		return "VOLGA_CLIENT_CONFIG_INVALID"
	case VolgaOfficeActionMissing:
		return "VOLGA_OFFICE_ACTION_MISSING"
	case VolgaActionURLMissing:
		return "VOLGA_ACTION_URL_MISSING"
	case VolgaAccessTokenMissing:
		return "VOLGA_ACCESS_TOKEN_MISSING"
	case VolgaAuthInitialFailed:
		return "VOLGA_AUTH_INITIAL_FAILED"
	case VolgaSessionFailed:
		return "VOLGA_SESSION_FAILED"
	case VolgaAuthSuccess:
		return "VOLGA_AUTH_SUCCESS"
	default:
		return ""
	}
}

// FailureClass is a fixed allowlist, never an error string or provider value.
// Non-failures and unknown events have no failure class.
func (e VolgaStartupEvent) FailureClass() string {
	switch e {
	case VolgaDocumentRequestFailed:
		return "AUTH_DOCUMENT_REQUEST"
	case VolgaRedirectRejected:
		return "AUTH_REDIRECT"
	case VolgaCaptchaFailed:
		return "AUTH_CAPTCHA"
	case VolgaClientConfigMissing, VolgaClientConfigInvalid, VolgaOfficeActionMissing,
		VolgaActionURLMissing, VolgaAccessTokenMissing:
		return "AUTH_CLIENT_CONFIG"
	case VolgaAuthInitialFailed:
		return "AUTH_INITIAL"
	case VolgaSessionFailed:
		return "AUTH_SESSION"
	default:
		return ""
	}
}

var volgaStartupSink struct {
	sync.RWMutex
	callback func(VolgaStartupEvent)
}

// SetVolgaStartupSink installs the embedding application's bounded, nonblocking
// observer. It receives only fixed enums, including while provider debug is off.
func SetVolgaStartupSink(sink func(VolgaStartupEvent)) {
	volgaStartupSink.Lock()
	volgaStartupSink.callback = sink
	volgaStartupSink.Unlock()
}

func emitVolgaStartup(event VolgaStartupEvent) {
	if event.String() == "" {
		return
	}
	volgaStartupSink.RLock()
	sink := volgaStartupSink.callback
	volgaStartupSink.RUnlock()
	if sink != nil {
		// Observability must not turn a successful auth into an error or leak a
		// callback's panic payload. Never hold the registration lock in a callback.
		defer func() { _ = recover() }()
		sink(event)
	}
}
