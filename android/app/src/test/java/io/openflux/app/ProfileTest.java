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
        for (String name : Profile.performanceProfileValues()) {
            Profile p = new Profile(); p.performanceProfile = name;
            assertEquals(name, Profile.normalizePerformanceProfile(name));
            assertEquals(name, Profile.fromJson(p.toJson()).performanceProfile);
        }
        assertEquals("baseline", Profile.fromJson(new JSONObject("{\"performanceProfile\":\"unknown\"}")).performanceProfile);
        assertEquals("baseline", Profile.normalizePerformanceProfile(null));
    }
    @Test public void selectorLabelsAndPersistedValuesRemainDistinct() throws Exception {
        String[] expected = {"baseline", "balanced", "low_latency", "throughput",
                "throughput_current", "throughput_mem", "throughput_96",
                "throughput_w64", "throughput_w48", "throughput_w32", "throughput_w32_429guard"};
        String[] labels = {"Baseline", "Balanced", "Low latency", "Throughput",
                "Throughput current (64 / 4096)", "Throughput memory (64 / 2048)",
                "Throughput 96 workers (96 / 4096)", "Throughput 64 workers (64 / 4096)",
                "Throughput 48 workers (48 / 4096)", "Throughput 32 workers (32 / 4096)",
                "Throughput 32 + 429 guard"};
        assertArrayEquals(expected, Profile.performanceProfileValues());
        assertArrayEquals(labels, Profile.performanceProfileLabels());
        for (int position = 0; position < expected.length; position++) {
            Profile p = new Profile();
            p.performanceProfile = Profile.performanceProfileValues()[position];
            Profile saved = Profile.fromJson(new JSONObject(p.toJson().toString()));
            int restoredPosition = java.util.Arrays.asList(Profile.performanceProfileValues())
                    .indexOf(Profile.normalizePerformanceProfile(saved.performanceProfile));
            assertEquals(position, restoredPosition);
            assertEquals(labels[position], Profile.performanceProfileLabels()[restoredPosition]);
        }
    }
}
