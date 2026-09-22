package utils

// carrierDiagnostic uses constant format identifiers, never the formatting
// arguments (which can contain document IDs, URLs, tokens or network errors).
// These observations add no reconnect, retry, auth or transport behavior.
func carrierDiagnostic(format string) string {
	switch format {
	case "[VOLGA] WS connected: user=%s":
		return "[CARRIER-DIAG] ws_connected=1"
	case "[VOLGA] WS error: %v":
		return "[CARRIER-DIAG] ws_error=1"
	case "[VOLGA] WS reconnect in %v":
		return "[CARRIER-DIAG] ws_reconnect=1"
	case "[VOLGA] transport started: user=%d(%s) rp=%s":
		return "[CARRIER-DIAG] transport_started=1"
	case "[VOLGA] batch send failed: %v":
		return "[CARRIER-DIAG] relay_error=1"
	case "[PANIC] recovered in %s: %v":
		return "[CARRIER-DIAG] recovered_panic=1"
	default:
		return ""
	}
}
