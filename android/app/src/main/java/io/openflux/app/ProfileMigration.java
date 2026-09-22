package io.openflux.app;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

/** One semantic alias migration. Never reads or logs connection credentials. */
final class ProfileMigration {
    private ProfileMigration() {}

    static boolean migrate(JSONArray profiles, boolean perfLab) throws JSONException {
        if (!perfLab) return false;
        boolean changed = false;
        for (int i = 0; i < profiles.length(); i++) {
            JSONObject profile = profiles.getJSONObject(i);
            if ("throughput_w32_429guard".equals(profile.optString("performanceProfile"))) {
                profile.put("performanceProfile", "optimized");
                changed = true;
            }
        }
        return changed;
    }
}
