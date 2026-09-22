package io.openflux.app;

import org.json.JSONObject;
import org.junit.Test;
import static org.junit.Assert.*;

public class PerformanceModeTest {
    @Test public void normalValuesAndLabels() {
        assertArrayEquals(new String[]{"standard", "speed", "optimized"}, PerformanceMode.values());
        assertEquals("Standard", PerformanceMode.label("standard"));
        assertEquals("Speed", PerformanceMode.label("speed"));
        assertEquals("Optimized", PerformanceMode.label("optimized"));
        for (String name : PerformanceMode.values()) {
            assertEquals(name, PerformanceMode.normalize(name));
            assertEquals(name, PerformanceMode.effective("vyandex", name));
            assertEquals("standard", PerformanceMode.effective("yandex", name));
        }
    }
    @Test public void oldProfilesRemainStandard() throws Exception {
        assertEquals("standard", Profile.fromJson(new JSONObject()).performanceMode);
        assertEquals("standard", new Profile().performanceMode);
        for (String name : new String[]{"throughput_w32", "throughput_w32_429guard", "balanced", "unknown"}) {
            assertEquals("standard", PerformanceMode.normalize(name));
        }
        assertEquals("standard", PerformanceMode.normalize(null));
    }
    @Test public void savedModesRoundTripWithoutChangingCredentialsOrCodec() throws Exception {
        Profile p = new Profile();
        p.id = 17;
        p.name = "Synthetic profile";
        p.icon = "ic_public";
        p.transportType = "vyandex";
        p.documentUrl = "synthetic-context";
        p.encryptionSecret = "synthetic-test-secret";
        p.codec = "legacy";
        p.maxToken = "synthetic-token";
        p.maxUid = "synthetic-id";
        JSONObject original = p.toJson();
        for (String mode : PerformanceMode.values()) {
            p.performanceMode = mode;
            Profile actual = Profile.fromJson(p.toJson());
            assertEquals(mode, actual.performanceMode);
            JSONObject expected = new JSONObject(original.toString());
            expected.put("performanceMode", mode);
            assertTrue(JsonTestAssertions.semanticEquals(expected, actual.toJson()));
        }
    }
}
