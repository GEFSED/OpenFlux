package io.openflux.app;

// Ordinary app values only. Missing/unknown saved values retain Standard.
final class PerformanceMode {
    private PerformanceMode() {}
    static String normalize(String mode) {
        return "speed".equals(mode) || "optimized".equals(mode) ? mode : "standard";
    }
    static String effective(String transport, String mode) {
        return "vyandex".equals(transport) ? normalize(mode) : "standard";
    }
    static String[] values() { return new String[] {"standard", "speed", "optimized"}; }
    static String label(String mode) {
        switch (normalize(mode)) {
            case "speed": return "Speed";
            case "optimized": return "Optimized";
            default: return "Standard";
        }
    }
}
