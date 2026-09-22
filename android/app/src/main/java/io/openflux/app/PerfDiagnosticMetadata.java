package io.openflux.app;

import org.json.JSONException;
import org.json.JSONObject;

/** Static, credential-free metadata for the numeric diagnostics snapshot. */
final class PerfDiagnosticMetadata {
    private PerfDiagnosticMetadata() {}

    static void apply(JSONObject data) throws JSONException {
        data.put("measurement_scope", "packet VPN; SOCKS5 profiles not applied; rates sample every 2s while visible");
        data.put("carrier_status", ReturnPathStatus.describe(data));
        data.put("acceptance", "Perf Lab diagnostics; packet counters do not independently prove Internet access.");
        if ("optimized".equals(data.optString("profile"))) {
            data.put("profile_stage", "optimized_candidate");
        } else {
            data.remove("profile_stage");
        }
    }
}
