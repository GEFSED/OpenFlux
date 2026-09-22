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
        assertEquals("baseline", new Profile().performanceProfile);
    }

    @Test public void acceptedValuesRoundTripAndUnknownIsBaseline() throws Exception {
        String[] expected = {"baseline", "balanced", "low_latency", "throughput",
                "throughput_current", "throughput_mem", "throughput_96",
                "throughput_w64", "throughput_w48", "throughput_w32", "throughput_w32_429guard",
                "optimized"};
        assertArrayEquals(expected, Profile.performanceProfileValues());
        for (String name : expected) {
            Profile p = new Profile();
            p.performanceProfile = name;
            assertEquals(name, Profile.normalizePerformanceProfile(name));
            assertEquals(name, Profile.fromJson(new JSONObject(p.toJson().toString())).performanceProfile);
        }
        assertEquals("baseline", Profile.fromJson(new JSONObject("{\"performanceProfile\":\"unknown\"}")).performanceProfile);
        assertEquals("baseline", Profile.normalizePerformanceProfile(null));
    }

    @Test public void normalSelectorHasOnlyFiveProfiles() {
        assertArrayEquals(new String[]{"baseline", "balanced", "low_latency", "throughput", "optimized"},
                Profile.visiblePerformanceProfileValues());
        assertArrayEquals(new String[]{"Baseline", "Balanced", "Low latency", "Throughput", "Optimized"},
                Profile.visiblePerformanceProfileLabels());
        for (int i = 0; i < Profile.visiblePerformanceProfileValues().length; i++) {
            String value = Profile.visiblePerformanceProfileValues()[i];
            assertEquals(i, Profile.visiblePerformanceProfileIndex(value));
            assertEquals(Profile.visiblePerformanceProfileLabels()[i], Profile.performanceProfileDisplayLabel(value));
        }
    }

    @Test public void hiddenValuesHaveTruthfulLabelAndNoNormalSelection() throws Exception {
        for (String name : new String[]{"throughput_current", "throughput_mem", "throughput_96",
                "throughput_w64", "throughput_w48", "throughput_w32", "throughput_w32_429guard"}) {
            Profile p = new Profile();
            p.performanceProfile = name;
            assertEquals(-1, Profile.visiblePerformanceProfileIndex(p.performanceProfile));
            assertEquals("Historical experiment: " + name, Profile.performanceProfileDisplayLabel(p.performanceProfile));
            // Rendering a current value must not normalize it to a visible/default profile.
            Profile saved = Profile.fromJson(new JSONObject(p.toJson().toString()));
            assertEquals(name, saved.performanceProfile);
        }
    }
}
