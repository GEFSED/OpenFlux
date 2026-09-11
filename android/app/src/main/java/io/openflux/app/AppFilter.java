package io.openflux.app;

/**
 * Shared constants for the per-app VPN routing filter, read/written by
 * MainActivity's "Приложения" settings tab and applied by
 * OpenFluxVpnService when it builds the tunnel.
 */
final class AppFilter {
    static final String PREFS_NAME = "openflux_app_filter";
    static final String KEY_MODE = "mode";
    static final String KEY_PACKAGES = "packages";

    static final String MODE_OFF = "off";
    static final String MODE_WHITELIST = "whitelist";
    static final String MODE_BLACKLIST = "blacklist";

    private AppFilter() {
    }
}
