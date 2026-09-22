package yandex

import "sync/atomic"

type volgaReceiveCounters struct {
	wsConnected                                                                             atomic.Bool
	wsConnectSuccess, wsConnectFailures, wsRawMessages, wsPingMessages                      atomic.Uint64
	wsSessionMessages, wsWorkerMessages, wsSelfIgnored, wsRelayMessages, wsExchangeMessages atomic.Uint64
	wsJSONErrors, wsBase64Errors, innerPackets, innerBytes                                  atomic.Uint64
}

// VolgaReceiveDiagnostics distinguishes a started sender from an established
// receive WebSocket. A connected WS still does not prove end-to-end IP traffic.
// Ping counts application JSON pings, not WebSocket control frames.
type VolgaReceiveDiagnostics struct {
	WSConnected        bool   `json:"ws_connected"`
	WSConnectSuccess   uint64 `json:"ws_connect_success"`
	WSConnectFailures  uint64 `json:"ws_connect_failures"`
	WSRawMessages      uint64 `json:"ws_raw_messages"`
	WSPingMessages     uint64 `json:"ws_ping_messages"`
	WSSessionMessages  uint64 `json:"ws_session_messages"`
	WSWorkerMessages   uint64 `json:"ws_worker_messages"`
	WSSelfIgnored      uint64 `json:"ws_self_messages_ignored"`
	WSRelayMessages    uint64 `json:"ws_relay_messages"`
	WSExchangeMessages uint64 `json:"ws_exchange_messages"`
	WSJSONErrors       uint64 `json:"ws_json_errors"`
	WSBase64Errors     uint64 `json:"ws_base64_errors"`
	InnerPackets       uint64 `json:"volga_inner_packets_decoded"`
	InnerBytes         uint64 `json:"volga_inner_bytes_decoded"`
}

func (c *volgaReceiveCounters) snapshot() VolgaReceiveDiagnostics {
	return VolgaReceiveDiagnostics{c.wsConnected.Load(), c.wsConnectSuccess.Load(), c.wsConnectFailures.Load(), c.wsRawMessages.Load(), c.wsPingMessages.Load(), c.wsSessionMessages.Load(), c.wsWorkerMessages.Load(), c.wsSelfIgnored.Load(), c.wsRelayMessages.Load(), c.wsExchangeMessages.Load(), c.wsJSONErrors.Load(), c.wsBase64Errors.Load(), c.innerPackets.Load(), c.innerBytes.Load()}
}
