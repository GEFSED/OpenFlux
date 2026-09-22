package io.openflux.app;

import org.json.JSONException;
import org.json.JSONObject;

// A saved connection configuration: which transport, which document/secret,
// a display name and an icon. Lets a user keep several exit nodes configured
// and switch between them without re-typing anything.
final class Profile {
    long id;
    String name = "";
    String icon = "ic_public";
    String transportType = "yandex";
    String documentUrl = "";
    String encryptionSecret = "";
    String codec = "batched";
    String performanceProfile = "baseline";

    static String normalizePerformanceProfile(String value) {
        for (String name : performanceProfileValues()) if (name.equals(value)) return name;
        return "baseline";
    }

    // Used by the Perf Lab selector and shared profile persistence/parser.
    static String[] performanceProfileValues() {
        return new String[]{"baseline", "balanced", "low_latency", "throughput",
                "throughput_current", "throughput_mem", "throughput_96",
                "throughput_w64", "throughput_w48", "throughput_w32"};
    }

    static String[] performanceProfileLabels() {
        return new String[]{"Baseline", "Balanced", "Low latency", "Throughput",
                "Throughput current (64 / 4096)", "Throughput memory (64 / 2048)",
                "Throughput 96 workers (96 / 4096)", "Throughput 64 workers (64 / 4096)",
                "Throughput 48 workers (48 / 4096)", "Throughput 32 workers (32 / 4096)"};
    }
    String maxToken = "";
    String maxUid = "";

    JSONObject toJson() throws JSONException {
        JSONObject o = new JSONObject();
        o.put("id", id);
        o.put("name", name);
        o.put("icon", icon);
        o.put("transportType", transportType);
        o.put("documentUrl", documentUrl);
        o.put("encryptionSecret", encryptionSecret);
        o.put("codec", codec);
        o.put("performanceProfile", normalizePerformanceProfile(performanceProfile));
        o.put("maxToken", maxToken);
        o.put("maxUid", maxUid);
        return o;
    }

    static Profile fromJson(JSONObject o) {
        Profile p = new Profile();
        p.id = o.optLong("id");
        p.name = o.optString("name", "");
        p.icon = o.optString("icon", "ic_public");
        p.transportType = o.optString("transportType", "yandex");
        p.documentUrl = o.optString("documentUrl", "");
        p.encryptionSecret = o.optString("encryptionSecret", "");
        p.codec = o.optString("codec", "batched");
        p.performanceProfile = normalizePerformanceProfile(o.optString("performanceProfile", "baseline"));
        p.maxToken = o.optString("maxToken", "");
        p.maxUid = o.optString("maxUid", "");
        return p;
    }
}
