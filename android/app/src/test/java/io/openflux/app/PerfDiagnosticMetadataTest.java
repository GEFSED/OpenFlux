package io.openflux.app;

import java.util.Arrays;
import java.util.HashSet;
import java.util.Iterator;
import java.util.Set;
import org.json.JSONObject;
import org.junit.Test;
import static org.junit.Assert.*;

public class PerfDiagnosticMetadataTest {
    @Test public void candidateStageAppliesOnlyToExactOptimizedName() throws Exception {
        for (String profile : Profile.performanceProfileValues()) {
            JSONObject data = new JSONObject().put("profile", profile).put("profile_stage", "stale");
            PerfDiagnosticMetadata.apply(data);
            assertEquals(profile, data.getString("profile"));
            if ("optimized".equals(profile)) {
                assertEquals("optimized_candidate", data.getString("profile_stage"));
            } else {
                assertFalse(data.has("profile_stage"));
            }
        }
        JSONObject stopped = new JSONObject();
        PerfDiagnosticMetadata.apply(stopped);
        assertFalse(stopped.has("profile_stage"));
    }

    @Test public void httpGuardAndPacketCountersArePreserved() throws Exception {
        JSONObject data = new JSONObject().put("profile", "optimized");
        String[] counters = {"http_requests", "http_successes", "http_failures_total",
                "http_failures_network", "http_failures_timeout", "http_failures_4xx",
                "http_failures_429", "http_failures_5xx", "http_failures_other_status",
                "rate_limit_429_events", "rate_limit_wait_events", "rate_limit_wait_ns",
                "rate_limit_retry_after_used", "rate_limit_retry_after_invalid", "rate_limit_fallback_used",
                "rate_limit_current_gate_remaining_ns", "rate_limit_max_cooldown_ns",
                "upload_bytes", "download_bytes", "encrypted_receive_success", "mobile_callback_packets",
                "queue_drops", "tun_write_failures"};
        for (int i = 0; i < counters.length; i++) data.put(counters[i], i + 1);
        data.put("http_failure_rate", 0.125);
        data.put("rate_limit_guard_enabled", true);
        JSONObject original = new JSONObject(data.toString());
        PerfDiagnosticMetadata.apply(data);
        for (String key : new String[]{"measurement_scope", "carrier_status", "acceptance", "profile_stage"}) {
            data.remove(key);
        }
        assertTrue("metadata changed a diagnostic field", JsonTestAssertions.semanticEquals(original, data));
    }

    @Test public void copyIsNeutralAndDoesNotAssertInternetAccess() throws Exception {
        JSONObject data = new JSONObject().put("profile", "optimized")
                .put("connected", true).put("transport_started", true).put("transport", "vyandex")
                .put("ws_connected", true).put("mobile_callback_packets", 1);
        PerfDiagnosticMetadata.apply(data);
        assertEquals("packet VPN; SOCKS5 profiles not applied; rates sample every 2s while visible",
                data.getString("measurement_scope"));
        assertEquals("Perf Lab diagnostics; packet counters do not independently prove Internet access.",
                data.getString("acceptance"));
        assertEquals("return_packets_observed_internet_not_verified", data.getString("carrier_status"));
        for (String stale : new String[]{"A/B", "suspended", "TUN classification", "Speedtest", "Elisa"}) {
            assertFalse(data.toString().contains(stale));
        }
    }

    @Test public void metadataAddsOnlySafeFixedFields() throws Exception {
        JSONObject data = new JSONObject().put("profile", "optimized");
        PerfDiagnosticMetadata.apply(data);
        Set<String> expected = new HashSet<>(Arrays.asList("profile", "measurement_scope",
                "carrier_status", "acceptance", "profile_stage"));
        Set<String> actual = new HashSet<>();
        Iterator<String> keys = data.keys();
        while (keys.hasNext()) actual.add(keys.next());
        assertEquals(expected, actual);
        String encoded = data.toString().toLowerCase(java.util.Locale.ROOT);
        for (String forbidden : new String[]{"http://", "https://", "cookie", "token", "document",
                "retry-after", "encryption", "secret", "header"}) {
            assertFalse("unexpected sensitive metadata", encoded.contains(forbidden));
        }
    }
}
