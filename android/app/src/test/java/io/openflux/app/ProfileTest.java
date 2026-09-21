package io.openflux.app;
import org.junit.Test;
import org.json.JSONObject;
import static org.junit.Assert.*;
public class ProfileTest {
    @Test public void oldProfilesDefaultToBaseline() throws Exception {
        Profile p = Profile.fromJson(new JSONObject("{\"codec\":\"legacy\",\"transportType\":\"vyandex\"}"));
        assertEquals("baseline", p.performanceProfile);
        assertEquals("legacy", p.codec);
        assertEquals("vyandex", p.transportType);
    }
    @Test public void candidatesRoundTripAndUnknownIsBaseline() throws Exception {
        for (String name : new String[]{"baseline", "balanced", "low_latency", "throughput"}) {
            Profile p = new Profile(); p.performanceProfile = name;
            assertEquals(name, Profile.fromJson(p.toJson()).performanceProfile);
        }
        assertEquals("baseline", Profile.fromJson(new JSONObject("{\"performanceProfile\":\"unknown\"}")).performanceProfile);
    }
}
