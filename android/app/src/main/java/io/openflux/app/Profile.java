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
        if ("balanced".equals(value) || "low_latency".equals(value) || "throughput".equals(value)) return value;
        return "baseline";
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
