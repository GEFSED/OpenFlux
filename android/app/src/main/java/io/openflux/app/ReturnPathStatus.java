package io.openflux.app;

import org.json.JSONObject;

/** Read-only interpretation. No change to the base VPN lifecycle/status API. */
final class ReturnPathStatus {
    private ReturnPathStatus() {}

    static String describe(JSONObject data) {
        if (!data.optBoolean("transport_started")) return "transport_not_started";
        if (!"vyandex".equals(data.optString("transport"))) return "not_volga";
        if (!data.optBoolean("ws_connected")) return "transport_started_waiting_for_ws";
        if (data.optLong("mobile_callback_packets") == 0) return "ws_open_no_usable_return_packet";
        return "return_packets_observed_internet_not_verified";
    }
}
