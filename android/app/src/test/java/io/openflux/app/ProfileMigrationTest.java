package io.openflux.app;

import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import static org.junit.Assert.*;

public class ProfileMigrationTest {
    private static JSONObject fixture(String value) throws Exception {
        Profile p = new Profile();
        p.id = 42;
        p.name = "Synthetic profile";
        p.icon = "ic_lock";
        p.transportType = "vyandex";
        p.documentUrl = "https://example.invalid/synthetic-document";
        p.encryptionSecret = "synthetic-secret-not-real";
        p.codec = "legacy";
        p.maxToken = "synthetic-max-token";
        p.maxUid = "synthetic-max-uid";
        p.performanceProfile = value;
        return p.toJson().put("future_setting", new JSONObject().put("keep", true));
    }

    @Test public void perfLabMigrationChangesOnlyTheEquivalentName() throws Exception {
        JSONObject original = fixture("throughput_w32_429guard");
        JSONArray stored = new JSONArray().put(new JSONObject(original.toString()));
        assertTrue(ProfileMigration.migrate(stored, true));
        // Model the exact JSON persisted by ProfileStore, including unknown fields.
        JSONArray reloaded = new JSONArray(stored.toString());
        JSONObject migrated = reloaded.getJSONObject(0);
        assertEquals("optimized", migrated.getString("performanceProfile"));
        assertEquals("optimized", Profile.fromJson(migrated).performanceProfile);
        assertEquals(original.length(), migrated.length());
        JSONObject restored = new JSONObject(migrated.toString());
        restored.put("performanceProfile", "throughput_w32_429guard");
        assertTrue("migration changed another saved field", JsonTestAssertions.semanticEquals(original, restored));
        assertFalse("migration must be idempotent", ProfileMigration.migrate(reloaded, true));
    }

    @Test public void ordinaryAppDoesNotMigrateAnyName() throws Exception {
        JSONArray stored = new JSONArray();
        for (String name : Profile.performanceProfileValues()) stored.put(fixture(name));
        JSONArray original = new JSONArray(stored.toString());
        assertFalse(ProfileMigration.migrate(stored, false));
        assertTrue(JsonTestAssertions.semanticEquals(original, stored));
    }

    @Test public void otherHistoricalAndNormalNamesStayUnchanged() throws Exception {
        JSONArray stored = new JSONArray();
        for (String name : Profile.performanceProfileValues()) {
            if (!"throughput_w32_429guard".equals(name)) stored.put(fixture(name));
        }
        stored.put(new JSONObject().put("name", "older profile without a performance field"));
        stored.put(new JSONObject().put("performanceProfile", "future_unknown_value"));
        JSONArray original = new JSONArray(stored.toString());
        assertFalse(ProfileMigration.migrate(stored, true));
        assertTrue(JsonTestAssertions.semanticEquals(original, stored));
    }

    @Test public void mixedListRetainsOrderAndEveryNonTargetProfile() throws Exception {
        JSONArray stored = new JSONArray().put(fixture("throughput_w48"))
                .put(fixture("throughput_w32_429guard")).put(fixture("throughput_w32"));
        JSONArray original = new JSONArray(stored.toString());
        assertTrue(ProfileMigration.migrate(stored, true));
        assertEquals(3, stored.length());
        assertTrue(JsonTestAssertions.semanticEquals(original.getJSONObject(0), stored.getJSONObject(0)));
        assertTrue(JsonTestAssertions.semanticEquals(original.getJSONObject(2), stored.getJSONObject(2)));
        assertEquals("optimized", stored.getJSONObject(1).getString("performanceProfile"));
    }
}
