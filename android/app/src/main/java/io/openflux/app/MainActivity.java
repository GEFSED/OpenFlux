package io.openflux.app;

import android.Manifest;
import android.animation.Animator;
import android.animation.AnimatorListenerAdapter;
import android.animation.AnimatorSet;
import android.animation.ObjectAnimator;
import android.animation.PropertyValuesHolder;
import android.animation.ValueAnimator;
import android.app.Activity;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.content.pm.ResolveInfo;
import android.content.res.ColorStateList;
import android.content.res.Configuration;
import android.graphics.Bitmap;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.Drawable;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.RippleDrawable;
import android.net.Uri;
import android.net.VpnService;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.os.PowerManager;
import android.os.VibrationEffect;
import android.os.Vibrator;
import android.provider.Settings;
import android.text.InputType;
import android.text.Spannable;
import android.text.SpannableStringBuilder;
import android.text.method.PasswordTransformationMethod;
import android.text.style.ForegroundColorSpan;
import android.view.Gravity;
import android.view.HapticFeedbackConstants;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.view.animation.DecelerateInterpolator;
import android.view.animation.OvershootInterpolator;
import android.widget.BaseAdapter;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.HorizontalScrollView;
import android.widget.ImageButton;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ListView;
import android.widget.PopupWindow;
import android.widget.RadioButton;
import android.widget.RadioGroup;
import android.widget.ScrollView;
import android.widget.Switch;
import android.widget.TextView;
import android.widget.Toast;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.net.Inet4Address;
import java.net.HttpURLConnection;
import java.net.InetAddress;
import java.net.NetworkInterface;
import java.net.SocketException;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.WeakHashMap;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

import com.google.zxing.BarcodeFormat;
import com.google.zxing.WriterException;
import com.google.zxing.common.BitMatrix;
import com.google.zxing.qrcode.QRCodeWriter;

import org.json.JSONObject;

import android.util.Base64;

import io.openflux.bridge.mobile.Mobile;

public final class MainActivity extends Activity {
    // Named explicitly (instead of the implicit per-Activity-class file from
    // getPreferences()) so OpenFluxTileService can read the connection mode
    // and network settings without depending on Activity.getPreferences()'s
    // undocumented file-naming behavior.
    static final String SETTINGS_PREFS_NAME = "openflux_settings";
    private static final int TUNNEL_PERMISSION_REQUEST = 42;
    private static final int NOTIFICATION_PERMISSION_REQUEST = 43;
    private static final String DEFAULT_DNS = "1.1.1.1";
    private static final int DEFAULT_MTU = 1400;
    private static final int PAGE_HOME = 0;
    private static final int PAGE_PROFILES = 1;
    private static final int PAGE_LOGS = 2;
    private static final int PAGE_SETTINGS = 3;
    private static final int SETTINGS_MODE = 0;
    private static final int SETTINGS_NETWORK = 1;
    private static final int SETTINGS_APPS = 2;
    private static final int SETTINGS_INTERFACE = 3;
    private static final int SETTINGS_ABOUT = 4;
    private static final String[] PROFILE_ICON_KEYS = {
            "ic_public", "ic_link", "ic_lock", "ic_key", "ic_power",
            "ic_person", "ic_swap", "ic_terminal", "ic_apps", "ic_settings",
    };
    private static final String MODE_TUNNEL = "tunnel";
    private static final String MODE_PROXY = "proxy";
    private static final int DEFAULT_PROXY_PORT = 1080;
    private static final String MAIN_REPO_URL = "https://github.com/p1neappleXpress/OpenFlux";
    private static final String FORK_REPO_URL = "https://github.com/damnurmum/OpenFlux-Android";
    private static final String FORK_REPO_SLUG = "damnurmum/OpenFlux-Android";
    private static final int VERSION_CHECK_PENDING = 0;
    private static final int VERSION_CHECK_LATEST = 1;
    private static final int VERSION_CHECK_OUTDATED = 2;

    private final Handler handler = new Handler(Looper.getMainLooper());
    private boolean darkMode;
    private boolean urlVisible;
    private boolean autoScroll = true;
    private boolean showSensitiveLogs = true;
    private int currentPage = PAGE_HOME;
    private int settingsSubTab = SETTINGS_MODE;
    private boolean settingsDetailOpen;
    private int background;
    private int surface;
    private int text;
    private int secondary;
    private int border;
    private int accent;
    private int hint;
    private int logColor;

    private LinearLayout root;
    private FrameLayout content;
    private EditText urlInput;
    private EditText encryptionInput;
    private EditText maxTokenInput;
    private EditText maxUidInput;
    private View maxFieldsContainer;
    private EditText dnsInput;
    private EditText mtuInput;
    private EditText proxyPortInput;
    private EditText proxyUsernameInput;
    private EditText proxyPasswordInput;
    private ImageButton visibilityButton;
    private ImageButton encryptionVisibilityButton;
    private ImageButton proxyPasswordVisibilityButton;
    private TextView statusDot;
    private TextView statusView;
    private TextView statusDetail;
    private TextView logView;
    private ScrollView logScroll;
    private LinearLayout tunnelButton;
    private TextView tunnelButtonText;
    private String documentUrl;
    private String encryptionSecret;
    private String transportType = "yandex";
    private String codec = "batched";
    private String maxToken = "";
    private String maxUid = "";
    private ProfileStore profileStore;
    private List<Profile> profiles = new ArrayList<>();
    private long selectedProfileId = -1;
    private boolean profileEditorOpen;
    private Long editingProfileId;
    private String editorIcon = "ic_public";
    private String editorTransportType = "yandex";
    private String editorCodec = "batched";
    private String editorMaxToken = "";
    private String editorMaxUid = "";
    private EditText profileNameInput;
    private PopupWindow profileDropdown;
    private TextView uptimeView;
    private String dnsServer;
    private int mtu;
    private String connectionMode = MODE_TUNNEL;
    private int proxyPort = DEFAULT_PROXY_PORT;
    private boolean proxyLanAccess;
    private boolean proxyAuthEnabled;
    private String proxyUsername = "";
    private String proxyPassword = "";
    // Draft state for the Network/Mode settings sub-pages: edits only take
    // effect when "Применить" is pressed, not just by navigating away (see
    // applyNetworkSettings/applyModeSettings). Seeded from the live fields
    // each time the corresponding sub-page opens (openSettingsDetail).
    private String editorDnsServer = "";
    private int editorMtu;
    private String editorConnectionMode = MODE_TUNNEL;
    private int editorProxyPort = DEFAULT_PROXY_PORT;
    private boolean editorProxyLanAccess;
    private boolean editorProxyAuthEnabled;
    private String logs = "";
    private String lastShownError = "";
    private boolean encryptionVisible;
    private boolean proxyPasswordVisible;
    private SecureSettings secureSettings;
    private boolean shellAnimated;
    private ObjectAnimator dotPulse;
    private int lastTunnelButtonFill = -1;
    private Vibrator vibrator;
    private String lastAnnouncedState = "";
    private TextView versionBadge;
    private String appVersion = "";
    private String latestVersion;
    private int versionCheckState = VERSION_CHECK_PENDING;
    private final WeakHashMap<View, AnimatorSet> bounceAnimators = new WeakHashMap<>();

    private SharedPreferences appFilterPrefs;
    private String appFilterMode = AppFilter.MODE_OFF;
    private final LinkedHashSet<String> selectedApps = new LinkedHashSet<>();
    private List<AppEntry> installedAppsCache;

    private final Runnable refresh = new Runnable() {
        @Override public void run() {
            updateStatus();
            String pending = Mobile.readLogs();
            if (pending != null && !pending.isEmpty()) appendLog(pending);
            handler.postDelayed(this, 500);
        }
    };

    @Override protected void onCreate(Bundle state) {
        super.onCreate(state);
        vibrator = getSystemService(Vibrator.class);
        SharedPreferences prefs = getSharedPreferences(SETTINGS_PREFS_NAME, MODE_PRIVATE);
        secureSettings = new SecureSettings(this);
        // Older prototype builds used plain preferences. Remove those values:
        // connection credentials now live only in the Keystore-backed store.
        prefs.edit().remove("document_url").remove("connection_document_url").apply();
        documentUrl = secureSettings.getString("document_url", "");
        encryptionSecret = secureSettings.getString("encryption_secret", "");
        profileStore = new ProfileStore(secureSettings);
        profiles = profileStore.load();
        selectedProfileId = profileStore.getSelectedId();
        migrateLegacyProfileIfNeeded();
        applySelectedProfileToFields();
        dnsServer = prefs.getString("dns_server", DEFAULT_DNS);
        mtu = prefs.getInt("mtu", DEFAULT_MTU);
        connectionMode = MODE_PROXY.equals(prefs.getString("connection_mode", MODE_TUNNEL)) ? MODE_PROXY : MODE_TUNNEL;
        proxyPort = prefs.getInt("proxy_port", DEFAULT_PROXY_PORT);
        proxyLanAccess = prefs.getBoolean("proxy_lan_access", false);
        proxyAuthEnabled = prefs.getBoolean("proxy_auth_enabled", false);
        proxyUsername = prefs.getString("proxy_username", "");
        proxyPassword = secureSettings.getString("proxy_password", "");
        autoScroll = prefs.getBoolean("auto_scroll", true);
        showSensitiveLogs = prefs.getBoolean("show_sensitive_logs", true);
        darkMode = prefs.contains("dark_mode")
                ? prefs.getBoolean("dark_mode", isSystemDark())
                : isSystemDark();
        appFilterPrefs = getSharedPreferences(AppFilter.PREFS_NAME, MODE_PRIVATE);
        appFilterMode = appFilterPrefs.getString(AppFilter.KEY_MODE, AppFilter.MODE_OFF);
        selectedApps.addAll(appFilterPrefs.getStringSet(AppFilter.KEY_PACKAGES, Collections.emptySet()));
        appVersion = readAppVersion();
        applyPalette();
        configureSystemBars();
        buildShell();
        showPage(PAGE_HOME);
        appendLog("Готово. При первом запуске Android запросит разрешение на туннель.");
        checkForUpdates();
        requestNotificationPermissionIfNeeded();
    }

    // Android 13+ requires this runtime permission to actually display any
    // notification, including a foreground service's - without it the tunnel
    // and proxy services still run fine, they just show no ongoing
    // notification (no status, no speed indicator) for the user to see.
    private void requestNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT < 33) return;
        if (checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED) return;
        requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, NOTIFICATION_PERMISSION_REQUEST);
    }

    @Override public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        if (requestCode == NOTIFICATION_PERMISSION_REQUEST
                && (grantResults.length == 0 || grantResults[0] != PackageManager.PERMISSION_GRANTED)) {
            appendLog("Без разрешения на уведомления статус подключения не будет показан в шторке.");
        }
    }

    @Override public void onBackPressed() {
        if (currentPage == PAGE_SETTINGS && settingsDetailOpen) {
            closeSettingsDetail();
            return;
        }
        if (currentPage == PAGE_PROFILES && profileEditorOpen) {
            closeProfileEditor();
            return;
        }
        super.onBackPressed();
    }

    @Override protected void onStart() {
        super.onStart();
        handler.removeCallbacks(refresh);
        handler.post(refresh);
    }

    @Override protected void onStop() {
        handler.removeCallbacks(refresh);
        captureLogs();
        super.onStop();
    }

    private boolean isSystemDark() {
        return (getResources().getConfiguration().uiMode & Configuration.UI_MODE_NIGHT_MASK)
                == Configuration.UI_MODE_NIGHT_YES;
    }

    private void applyPalette() {
        if (darkMode) {
            background = Color.rgb(18, 18, 18);
            surface = Color.rgb(30, 30, 30);
            text = Color.rgb(241, 243, 244);
            secondary = Color.rgb(189, 193, 198);
            border = Color.rgb(60, 64, 67);
            accent = Color.rgb(255, 107, 107);
            hint = Color.rgb(154, 160, 166);
            logColor = Color.rgb(218, 220, 224);
        } else {
            background = Color.rgb(248, 249, 250);
            surface = Color.WHITE;
            text = Color.rgb(32, 33, 36);
            secondary = Color.rgb(95, 99, 104);
            border = Color.rgb(218, 220, 224);
            accent = Color.rgb(234, 26, 26);
            hint = Color.rgb(128, 134, 139);
            logColor = Color.rgb(60, 64, 67);
        }
    }

    private void configureSystemBars() {
        Window window = getWindow();
        window.setStatusBarColor(background);
        window.setNavigationBarColor(background);
        window.getDecorView().setSystemUiVisibility(darkMode ? 0
                : View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR | View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR);
    }

    private void buildShell() {
        int side = dp(20);
        int top = dp(16);
        int bottom = dp(6);
        root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(side, top, side, bottom);
        root.setBackgroundColor(background);
        root.setOnApplyWindowInsetsListener((view, insets) -> {
            view.setPadding(side, top + insets.getSystemWindowInsetTop(), side,
                    bottom + insets.getSystemWindowInsetBottom());
            return insets;
        });
        root.addView(buildCompactHeader(), new LinearLayout.LayoutParams(-1, dp(54)));

        content = new FrameLayout(this);
        LinearLayout.LayoutParams contentParams = new LinearLayout.LayoutParams(-1, 0, 1f);
        contentParams.topMargin = dp(16);
        root.addView(content, contentParams);
        root.addView(buildBottomNav(), new LinearLayout.LayoutParams(-1, dp(68)));
        setContentView(root);
        root.setAlpha(0f);
        root.animate().alpha(1f).setDuration(shellAnimated ? 200 : 340).start();
        shellAnimated = true;
    }

    private View buildCompactHeader() {
        LinearLayout header = new LinearLayout(this);
        header.setOrientation(LinearLayout.HORIZONTAL);
        header.setGravity(Gravity.CENTER_VERTICAL);

        ImageView logo = new ImageView(this);
        logo.setContentDescription("Логотип OpenFlux");
        logo.setScaleType(ImageView.ScaleType.FIT_CENTER);
        logo.setBackground(rounded(Color.rgb(234, 26, 26), Color.TRANSPARENT, 0, 10));
        logo.setImageResource(R.drawable.ic_openflux_foreground);
        logo.setClipToOutline(true);
        header.addView(logo, new LinearLayout.LayoutParams(dp(44), dp(44)));

        LinearLayout titles = new LinearLayout(this);
        titles.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams titlesParams = new LinearLayout.LayoutParams(0, -2, 1f);
        titlesParams.leftMargin = dp(12);
        TextView title = text("OpenFlux", 21, text, true);
        TextView subtitle = text("Туннель через Yandex Docs", 12, secondary, false);
        titles.addView(title);
        titles.addView(subtitle);
        header.addView(titles, titlesParams);

        versionBadge = text("", 10, accent, true);
        versionBadge.setGravity(Gravity.CENTER);
        versionBadge.setPadding(dp(9), dp(5), dp(9), dp(5));
        header.addView(versionBadge);
        updateVersionBadge();
        return header;
    }

    private void updateVersionBadge() {
        if (versionBadge == null) return;
        String base = appVersion.isEmpty() ? "-" : appVersion;
        int fg;
        int bg;
        String label;
        switch (versionCheckState) {
            case VERSION_CHECK_LATEST:
                fg = darkMode ? Color.rgb(129, 201, 149) : Color.rgb(24, 128, 56);
                bg = darkMode ? Color.rgb(30, 46, 36) : Color.rgb(230, 245, 234);
                label = base + " | Последняя версия";
                break;
            case VERSION_CHECK_OUTDATED:
                fg = darkMode ? Color.rgb(253, 214, 99) : Color.rgb(249, 171, 0);
                bg = darkMode ? Color.rgb(56, 46, 20) : Color.rgb(255, 243, 224);
                label = base + " | Доступно обновление";
                break;
            case VERSION_CHECK_PENDING:
            default:
                fg = accent;
                bg = darkMode ? Color.rgb(38, 50, 68) : Color.rgb(232, 240, 254);
                label = base;
        }
        versionBadge.setText(label);
        versionBadge.setTextColor(fg);
        versionBadge.setBackground(rounded(bg, Color.TRANSPARENT, 0, 12));
    }

    private String readAppVersion() {
        try {
            PackageInfo info = getPackageManager().getPackageInfo(getPackageName(), 0);
            return info.versionName != null ? info.versionName : "";
        } catch (PackageManager.NameNotFoundException e) {
            return "";
        }
    }

    private void checkForUpdates() {
        new Thread(() -> {
            String latest = fetchLatestGithubVersion();
            if (latest == null || latest.isEmpty()) return;
            handler.post(() -> {
                latestVersion = latest;
                versionCheckState = compareVersions(appVersion, latest) >= 0
                        ? VERSION_CHECK_LATEST : VERSION_CHECK_OUTDATED;
                updateVersionBadge();
            });
        }).start();
    }

    private String fetchLatestGithubVersion() {
        HttpURLConnection connection = null;
        try {
            URL url = new URL("https://api.github.com/repos/" + FORK_REPO_SLUG + "/releases/latest");
            connection = (HttpURLConnection) url.openConnection();
            connection.setConnectTimeout(5000);
            connection.setReadTimeout(5000);
            connection.setRequestProperty("Accept", "application/vnd.github+json");
            connection.setRequestProperty("User-Agent", "OpenFlux-Android");
            if (connection.getResponseCode() != 200) return null;
            StringBuilder body = new StringBuilder();
            try (BufferedReader reader = new BufferedReader(
                    new InputStreamReader(connection.getInputStream(), StandardCharsets.UTF_8))) {
                String line;
                while ((line = reader.readLine()) != null) body.append(line);
            }
            String tag = new JSONObject(body.toString()).optString("tag_name", "");
            return tag.startsWith("v") ? tag.substring(1) : tag;
        } catch (Exception ignored) {
            return null;
        } finally {
            if (connection != null) connection.disconnect();
        }
    }

    private int compareVersions(String a, String b) {
        String[] partsA = a.split("\\.");
        String[] partsB = b.split("\\.");
        int length = Math.max(partsA.length, partsB.length);
        for (int i = 0; i < length; i++) {
            int valueA = versionPart(partsA, i);
            int valueB = versionPart(partsB, i);
            if (valueA != valueB) return Integer.compare(valueA, valueB);
        }
        return 0;
    }

    private int versionPart(String[] parts, int index) {
        if (index >= parts.length) return 0;
        try {
            return Integer.parseInt(parts[index].replaceAll("[^0-9]", ""));
        } catch (NumberFormatException e) {
            return 0;
        }
    }

    private View buildBottomNav() {
        LinearLayout nav = new LinearLayout(this);
        nav.setOrientation(LinearLayout.HORIZONTAL);
        nav.setGravity(Gravity.CENTER);
        nav.setPadding(dp(4), dp(5), dp(4), dp(3));
        nav.setBackground(rounded(surface, border, 1, 16));
        nav.addView(navItem(R.drawable.ic_home, "Главная", PAGE_HOME), weighted());
        nav.addView(navItem(R.drawable.ic_public, "Профили", PAGE_PROFILES), weighted());
        nav.addView(navItem(R.drawable.ic_terminal, "Логи", PAGE_LOGS), weighted());
        nav.addView(navItem(R.drawable.ic_settings, "Настройки", PAGE_SETTINGS), weighted());
        return nav;
    }

    private View navItem(int icon, String label, int page) {
        LinearLayout item = new LinearLayout(this);
        item.setOrientation(LinearLayout.VERTICAL);
        item.setGravity(Gravity.CENTER);
        item.setBackground(ripple(Color.TRANSPARENT, 14));
        ImageView image = new ImageView(this);
        image.setImageResource(icon);
        boolean active = page == currentPage;
        image.setImageTintList(ColorStateList.valueOf(active ? accent : secondary));
        item.addView(image, new LinearLayout.LayoutParams(dp(24), dp(24)));
        if (active) {
            image.setScaleX(0.6f);
            image.setScaleY(0.6f);
            image.animate().scaleX(1f).scaleY(1f).setDuration(280)
                    .setInterpolator(new OvershootInterpolator(4f)).start();
        }
        TextView title = text(label, 11, active ? accent : secondary, active);
        LinearLayout.LayoutParams titleParams = new LinearLayout.LayoutParams(-2, -2);
        titleParams.topMargin = dp(2);
        item.addView(title, titleParams);
        item.setOnClickListener(v -> {
            if (page == currentPage) return;
            tap(v);
            showPage(page);
        });
        return item;
    }

    private void showPage(int page) {
        captureSettings();
        if (page != PAGE_SETTINGS) settingsDetailOpen = false;
        if (page != PAGE_PROFILES) profileEditorOpen = false;
        currentPage = page;
        View pageView = page == PAGE_HOME ? buildHomePage()
                : page == PAGE_PROFILES ? buildProfilesPage()
                : page == PAGE_LOGS ? buildLogsPage() : buildSettingsPage();
        crossfadeContent(pageView);
        LinearLayout oldNav = (LinearLayout) root.getChildAt(root.getChildCount() - 1);
        root.removeView(oldNav);
        root.addView(buildBottomNav(), new LinearLayout.LayoutParams(-1, dp(68)));
        updateStatus();
    }

    // crossfadeContent swaps the FrameLayout's page content with a short fade
    // + rise instead of an instant cut, used for both outer tab switches and
    // Settings sub-tab switches.
    private void crossfadeContent(View newView) {
        int staleCount = content.getChildCount();
        View[] stale = new View[staleCount];
        for (int i = 0; i < staleCount; i++) stale[i] = content.getChildAt(i);

        newView.setAlpha(0f);
        newView.setTranslationY(dp(8));
        content.addView(newView, new FrameLayout.LayoutParams(-1, -1));
        newView.animate().alpha(1f).translationY(0f).setDuration(220).setStartDelay(40).start();

        for (View old : stale) {
            old.animate().cancel();
            old.animate().alpha(0f).setDuration(140).withEndAction(() -> content.removeView(old)).start();
        }
    }

    private void openSettingsDetail(int tab) {
        if (settingsDetailOpen && settingsSubTab == tab) return;
        settingsDetailOpen = true;
        settingsSubTab = tab;
        if (tab == SETTINGS_NETWORK) {
            editorDnsServer = dnsServer;
            editorMtu = mtu;
        } else if (tab == SETTINGS_MODE) {
            editorConnectionMode = connectionMode;
            editorProxyPort = proxyPort;
            editorProxyLanAccess = proxyLanAccess;
            editorProxyAuthEnabled = proxyAuthEnabled;
        }
        showPage(PAGE_SETTINGS);
    }

    private void closeSettingsDetail() {
        if (!settingsDetailOpen) return;
        settingsDetailOpen = false;
        showPage(PAGE_SETTINGS);
    }

    // migrateLegacyProfileIfNeeded turns a pre-0.6.0 install's single global
    // document URL / secret into the first profile, so an existing user's
    // connection keeps working without having to re-enter anything.
    private void migrateLegacyProfileIfNeeded() {
        if (!profiles.isEmpty()) return;
        if (!isValidDocumentUrl(documentUrl)) return;
        Profile migrated = new Profile();
        migrated.id = System.currentTimeMillis();
        migrated.name = "Профиль 1";
        migrated.icon = "ic_public";
        migrated.transportType = "yandex";
        migrated.documentUrl = documentUrl;
        migrated.encryptionSecret = encryptionSecret;
        profiles.add(migrated);
        selectedProfileId = migrated.id;
        profileStore.save(profiles);
        profileStore.setSelectedId(selectedProfileId);
    }

    private Profile selectedProfile() {
        for (Profile p : profiles) if (p.id == selectedProfileId) return p;
        return null;
    }

    // applySelectedProfileToFields refreshes the plain documentUrl/
    // encryptionSecret/transportType fields that toggleConnection/startTunnel/
    // startProxy already read, from whichever profile is currently selected.
    private void applySelectedProfileToFields() {
        Profile p = selectedProfile();
        if (p != null) {
            documentUrl = p.documentUrl;
            encryptionSecret = p.encryptionSecret;
            transportType = p.transportType;
            codec = p.codec;
            maxToken = p.maxToken;
            maxUid = p.maxUid;
        } else {
            documentUrl = "";
            encryptionSecret = "";
            transportType = "yandex";
            codec = "batched";
            maxToken = "";
            maxUid = "";
        }
    }

    private int profileIconRes(String key) {
        if (key == null) return R.drawable.ic_public;
        switch (key) {
            case "ic_link": return R.drawable.ic_link;
            case "ic_lock": return R.drawable.ic_lock;
            case "ic_key": return R.drawable.ic_key;
            case "ic_power": return R.drawable.ic_power;
            case "ic_person": return R.drawable.ic_person;
            case "ic_swap": return R.drawable.ic_swap;
            case "ic_terminal": return R.drawable.ic_terminal;
            case "ic_apps": return R.drawable.ic_apps;
            case "ic_settings": return R.drawable.ic_settings;
            case "ic_public":
            default: return R.drawable.ic_public;
        }
    }

    private String transportLabel(String type) {
        if ("vyandex".equals(type)) return "Yandex Docs (Volga)";
        if ("mailru".equals(type)) return "Mail.ru Docs";
        if ("cupsonline".equals(type)) return "Cups.online";
        if ("oneme".equals(type)) return "MAX (OneMe)";
        return "Yandex Docs";
    }

    private void selectProfile(long id) {
        selectedProfileId = id;
        profileStore.setSelectedId(id);
        applySelectedProfileToFields();
        showPage(PAGE_HOME);
    }

    private void openProfileEditor(Profile existing) {
        editingProfileId = existing != null ? existing.id : null;
        editorIcon = existing != null ? existing.icon : "ic_public";
        editorTransportType = existing != null ? existing.transportType : "yandex";
        editorCodec = existing != null ? existing.codec : "batched";
        editorMaxToken = existing != null ? existing.maxToken : "";
        editorMaxUid = existing != null ? existing.maxUid : "";
        profileEditorOpen = true;
        showPage(PAGE_PROFILES);
    }

    private void closeProfileEditor() {
        profileEditorOpen = false;
        showPage(PAGE_PROFILES);
    }

    private void saveProfileFromEditor(String name, String docUrl, String secret, String token, String uid) {
        if (name.isEmpty()) {
            Toast.makeText(this, "Укажите название профиля", Toast.LENGTH_SHORT).show();
            return;
        }
        boolean isMax = "oneme".equals(editorTransportType);
        if (!isMax && !isValidDocumentUrl(docUrl)) {
            Toast.makeText(this, "Укажите корректную HTTPS-ссылку на документ", Toast.LENGTH_SHORT).show();
            return;
        }
        if (isMax && token.isEmpty()) {
            Toast.makeText(this, "Укажите MAX Web token", Toast.LENGTH_SHORT).show();
            return;
        }
        if (!secret.isEmpty() && secret.length() < 16) {
            Toast.makeText(this, "Ключ шифрования должен быть не короче 16 символов, либо оставьте поле пустым",
                    Toast.LENGTH_LONG).show();
            return;
        }
        Profile target = null;
        if (editingProfileId != null) {
            for (Profile p : profiles) if (p.id == editingProfileId) { target = p; break; }
        }
        boolean isNew = target == null;
        if (isNew) {
            target = new Profile();
            target.id = System.currentTimeMillis();
            profiles.add(target);
        }
        target.name = name;
        target.icon = editorIcon;
        target.transportType = editorTransportType;
        target.documentUrl = docUrl;
        target.encryptionSecret = secret;
        target.codec = editorCodec;
        target.maxToken = token;
        target.maxUid = uid;
        profileStore.save(profiles);
        if (isNew && selectedProfile() == null) selectProfile(target.id);
        if (target.id == selectedProfileId) applySelectedProfileToFields();
        Toast.makeText(this, "Профиль сохранён", Toast.LENGTH_SHORT).show();
        closeProfileEditor();
    }

    private void deleteProfile(Profile profile) {
        if (isConnectionRunning() && profile.id == selectedProfileId) {
            Toast.makeText(this, "Нельзя удалить активный профиль во время подключения", Toast.LENGTH_SHORT).show();
            return;
        }
        new android.app.AlertDialog.Builder(this)
                .setTitle("Удалить профиль?")
                .setMessage("«" + profile.name + "» будет удалён без возможности восстановления.")
                .setNegativeButton("Отмена", null)
                .setPositiveButton("Удалить", (dialog, which) -> {
                    profiles.remove(profile);
                    profileStore.save(profiles);
                    if (profile.id == selectedProfileId) {
                        selectedProfileId = profiles.isEmpty() ? -1 : profiles.get(0).id;
                        profileStore.setSelectedId(selectedProfileId);
                        applySelectedProfileToFields();
                    }
                    Toast.makeText(this, "Профиль удалён", Toast.LENGTH_SHORT).show();
                    profileEditorOpen = false;
                    showPage(PAGE_PROFILES);
                })
                .show();
    }

    private void showProfileDropdown(View anchor) {
        if (profiles.isEmpty()) {
            showPage(PAGE_PROFILES);
            return;
        }
        LinearLayout content = new LinearLayout(this);
        content.setOrientation(LinearLayout.VERTICAL);
        content.setBackground(rounded(surface, border, 1, 12));
        content.setPadding(dp(4), dp(4), dp(4), dp(4));
        for (Profile p : profiles) {
            boolean selected = p.id == selectedProfileId;
            LinearLayout row = new LinearLayout(this);
            row.setGravity(Gravity.CENTER_VERTICAL);
            row.setPadding(dp(12), dp(10), dp(12), dp(10));
            row.setBackground(ripple(Color.TRANSPARENT, 8));
            row.setClickable(true);
            row.addView(icon(profileIconRes(p.icon), selected ? accent : secondary),
                    new LinearLayout.LayoutParams(dp(22), dp(22)));
            TextView nameView = text(p.name, 14, text, selected);
            LinearLayout.LayoutParams nameParams = new LinearLayout.LayoutParams(0, -2, 1f);
            nameParams.leftMargin = dp(12);
            nameParams.rightMargin = dp(8);
            row.addView(nameView, nameParams);
            if (selected) row.addView(icon(R.drawable.ic_check, accent), new LinearLayout.LayoutParams(dp(18), dp(18)));
            row.setOnClickListener(v -> {
                tap(v);
                selectProfile(p.id);
                if (profileDropdown != null) profileDropdown.dismiss();
            });
            content.addView(row);
        }
        PopupWindow popup = new PopupWindow(content, ViewGroup.LayoutParams.WRAP_CONTENT,
                ViewGroup.LayoutParams.WRAP_CONTENT, true);
        popup.setElevation(dp(8));
        popup.setOutsideTouchable(true);
        popup.setFocusable(true);
        profileDropdown = popup;
        popup.setOnDismissListener(() -> profileDropdown = null);
        popup.showAsDropDown(anchor, 0, dp(4));
    }

    private View buildProfileSelectorRow() {
        Profile p = selectedProfile();
        LinearLayout row = new LinearLayout(this);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(16), dp(14), dp(16), dp(14));
        row.setBackground(rounded(surface, border, 1, 11));
        row.setClickable(true);
        row.setFocusable(true);
        row.addView(icon(p != null ? profileIconRes(p.icon) : R.drawable.ic_public, accent),
                new LinearLayout.LayoutParams(dp(26), dp(26)));
        LinearLayout copy = new LinearLayout(this);
        copy.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams copyParams = new LinearLayout.LayoutParams(0, -2, 1f);
        copyParams.leftMargin = dp(14);
        copy.addView(text(p != null ? p.name : "Профиль не выбран", 15, text, true));
        copy.addView(text(p != null ? transportLabel(p.transportType) : "Нажмите, чтобы создать профиль",
                12, secondary, false));
        row.addView(copy, copyParams);
        row.addView(icon(R.drawable.ic_chevron_right, hint), new LinearLayout.LayoutParams(dp(20), dp(20)));
        row.setOnClickListener(v -> {
            bounce(v);
            if (profiles.isEmpty()) showPage(PAGE_PROFILES);
            else showProfileDropdown(v);
        });
        return row;
    }

    private View buildHomePage() {
        if (dotPulse != null) {
            dotPulse.cancel();
            dotPulse = null;
        }
        lastTunnelButtonFill = -1;

        boolean proxyMode = MODE_PROXY.equals(connectionMode);
        LinearLayout page = page();
        TextView heading = text("Подключение", 25, text, true);
        page.addView(heading);
        TextView intro = text(proxyMode
                ? "Локальный SOCKS5-прокси через документ-транспорт, без системного туннеля."
                : "Защищённый системный туннель через документ-транспорт.", 13, secondary, false);
        LinearLayout.LayoutParams introParams = matchWrap();
        introParams.topMargin = dp(4);
        page.addView(intro, introParams);

        LinearLayout status = new LinearLayout(this);
        status.setOrientation(LinearLayout.HORIZONTAL);
        status.setGravity(Gravity.CENTER_VERTICAL);
        status.setPadding(dp(18), dp(18), dp(18), dp(18));
        status.setBackground(rounded(surface, border, 1, 12));
        status.setElevation(dp(1));
        statusDot = new TextView(this);
        LinearLayout.LayoutParams dot = new LinearLayout.LayoutParams(dp(13), dp(13));
        dot.rightMargin = dp(15);
        status.addView(statusDot, dot);
        LinearLayout statusCopy = new LinearLayout(this);
        statusCopy.setOrientation(LinearLayout.VERTICAL);
        statusView = text("Остановлено", 17, text, true);
        statusDetail = text("Туннель сейчас не используется", 13, secondary, false);
        statusCopy.addView(statusView);
        statusCopy.addView(statusDetail);
        status.addView(statusCopy, new LinearLayout.LayoutParams(0, -2, 1f));
        uptimeView = text("", 13, secondary, true);
        status.addView(uptimeView, new LinearLayout.LayoutParams(-2, -2));
        LinearLayout.LayoutParams statusParams = matchWrap();
        statusParams.topMargin = dp(28);
        page.addView(status, statusParams);
        staggerIn(status, 30);

        LinearLayout.LayoutParams selectorParams = matchWrap();
        selectorParams.topMargin = dp(14);
        View profileSelector = buildProfileSelectorRow();
        page.addView(profileSelector, selectorParams);
        staggerIn(profileSelector, 80);

        tunnelButton = new LinearLayout(this);
        tunnelButton.setOrientation(LinearLayout.HORIZONTAL);
        tunnelButton.setGravity(Gravity.CENTER);
        tunnelButton.setClickable(true);
        tunnelButton.setFocusable(true);
        tunnelButton.setElevation(dp(2));
        ImageView powerIcon = icon(R.drawable.ic_power, Color.WHITE);
        LinearLayout.LayoutParams powerParams = new LinearLayout.LayoutParams(dp(24), dp(24));
        powerParams.rightMargin = dp(10);
        tunnelButton.addView(powerIcon, powerParams);
        tunnelButtonText = text(proxyMode ? "Запустить прокси" : "Запустить туннель", 16, Color.WHITE, true);
        tunnelButton.addView(tunnelButtonText, new LinearLayout.LayoutParams(-2, -2));
        tunnelButton.setOnClickListener(v -> {
            bounce(v);
            toggleConnection();
        });
        LinearLayout.LayoutParams buttonParams = new LinearLayout.LayoutParams(-1, dp(58));
        buttonParams.topMargin = dp(16);
        page.addView(tunnelButton, buttonParams);
        staggerIn(tunnelButton, 130);

        TextView summaryTitle = label("АКТИВНЫЕ ПАРАМЕТРЫ");
        LinearLayout.LayoutParams summaryTitleParams = matchWrap();
        summaryTitleParams.topMargin = dp(30);
        summaryTitleParams.bottomMargin = dp(8);
        page.addView(summaryTitle, summaryTitleParams);
        View activeParams = paramsCard(activeParamRows(proxyMode));
        LinearLayout.LayoutParams activeParamsParams = matchWrap();
        activeParamsParams.bottomMargin = dp(8);
        page.addView(activeParams, activeParamsParams);
        staggerIn(activeParams, 180);
        return wrapScroll(page);
    }

    private String[][] activeParamRows(boolean proxyMode) {
        if (proxyMode) {
            return new String[][]{
                    {"Режим", "Прокси (SOCKS5)"},
                    {"DNS-сервер", dnsServer},
                    {"Локальный порт", String.valueOf(proxyPort)},
                    {"Доступ", proxyAccessSummary()},
            };
        }
        return new String[][]{
                {"Режим", "Туннель (весь трафик)"},
                {"DNS-сервер", dnsServer},
                {"MTU пакета", String.valueOf(mtu)},
                {"Приложения", appFilterSummary()},
        };
    }

    private String proxyAccessSummary() {
        if (!proxyLanAccess) return "Только это устройство";
        String localIp = getLocalIpAddress();
        String address = localIp != null ? localIp + ":" + proxyPort : "IP не определён";
        return address + (proxyAuthEnabled ? " (с паролем)" : " (без пароля)");
    }

    private String appFilterSummary() {
        if (AppFilter.MODE_WHITELIST.equals(appFilterMode)) return "Белый список (" + selectedApps.size() + ")";
        if (AppFilter.MODE_BLACKLIST.equals(appFilterMode)) return "Чёрный список (" + selectedApps.size() + ")";
        return "Все";
    }

    private View paramsCard(String[][] rows) {
        LinearLayout card = new LinearLayout(this);
        card.setOrientation(LinearLayout.VERTICAL);
        card.setBackground(rounded(surface, border, 1, 11));
        for (int i = 0; i < rows.length; i++) {
            card.addView(paramRow(rows[i][0], rows[i][1]));
            if (i < rows.length - 1) addDivider(card, 0);
        }
        return card;
    }

    private View paramRow(String labelValue, String value) {
        LinearLayout row = new LinearLayout(this);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(14), dp(11), dp(14), dp(11));
        row.addView(text(labelValue, 13, secondary, false), new LinearLayout.LayoutParams(0, -2, 1f));
        TextView valueView = text(value, 13, text, true);
        valueView.setGravity(Gravity.END);
        row.addView(valueView, new LinearLayout.LayoutParams(-2, -2));
        return row;
    }

    private void staggerIn(View view, int delayMs) {
        view.setAlpha(0f);
        view.setTranslationY(dp(14));
        view.animate().alpha(1f).translationY(0f).setStartDelay(delayMs).setDuration(260)
                .setInterpolator(new DecelerateInterpolator()).start();
    }

    private View buildLogsPage() {
        LinearLayout page = page();
        LinearLayout header = new LinearLayout(this);
        header.setGravity(Gravity.CENTER_VERTICAL);
        TextView heading = text("Журнал событий", 25, text, true);
        header.addView(heading, new LinearLayout.LayoutParams(0, -2, 1f));
        ImageButton clear = iconButton(R.drawable.ic_delete, "Очистить журнал");
        clear.setOnClickListener(v -> {
            tap(v);
            logs = "";
            logView.setText("");
        });
        header.addView(clear, new LinearLayout.LayoutParams(dp(48), dp(48)));
        page.addView(header);

        TextView note = text("Логи хранятся только до закрытия приложения.", 11, secondary, false);
        LinearLayout.LayoutParams noteParams = matchWrap();
        noteParams.topMargin = dp(4);
        page.addView(note, noteParams);

        logView = text(colorizeLogs(logs), 12, logColor, false);
        logView.setTypeface(Typeface.MONOSPACE);
        logView.setTextIsSelectable(true);
        logView.setPadding(dp(14), dp(12), dp(14), dp(12));
        logScroll = new ScrollView(this);
        logScroll.setFillViewport(true);
        logScroll.setBackground(rounded(surface, border, 1, 10));
        logScroll.addView(logView, new ScrollView.LayoutParams(-1, -2));
        LinearLayout.LayoutParams logParams = new LinearLayout.LayoutParams(-1, 0, 1f);
        logParams.topMargin = dp(12);
        logParams.bottomMargin = dp(10);
        page.addView(logScroll, logParams);
        return page;
    }

    private View buildSettingsPage() {
        LinearLayout page = page();
        page.addView(buildSettingsHeader());

        if (!settingsDetailOpen) {
            LinearLayout.LayoutParams listParams = new LinearLayout.LayoutParams(-1, 0, 1f);
            listParams.topMargin = dp(16);
            listParams.bottomMargin = dp(10);
            page.addView(buildSettingsList(), listParams);
            return page;
        }

        boolean showSave = settingsSubTab != SETTINGS_ABOUT;
        LinearLayout.LayoutParams contentParams = new LinearLayout.LayoutParams(-1, 0, 1f);
        contentParams.topMargin = dp(16);
        if (!showSave) contentParams.bottomMargin = dp(10);
        page.addView(buildSettingsSubTabContent(), contentParams);

        if (showSave) {
            Button save = new Button(this);
            save.setText("Сохранить настройки");
            save.setAllCaps(false);
            save.setTextColor(Color.WHITE);
            save.setTextSize(15);
            save.setTypeface(Typeface.DEFAULT_BOLD);
            save.setStateListAnimator(null);
            save.setBackground(buttonBackground(Color.rgb(234, 26, 26), Color.rgb(179, 18, 18)));
            save.setOnClickListener(v -> {
                bounce(v);
                if (settingsSubTab == SETTINGS_NETWORK) applyNetworkSettings();
                else if (settingsSubTab == SETTINGS_MODE) applyModeSettings();
                Toast.makeText(this, "Настройки сохранены", Toast.LENGTH_SHORT).show();
            });
            LinearLayout.LayoutParams saveParams = new LinearLayout.LayoutParams(-1, dp(52));
            saveParams.topMargin = dp(14);
            saveParams.bottomMargin = dp(12);
            page.addView(save, saveParams);
        }
        return page;
    }

    private View buildSettingsHeader() {
        LinearLayout header = new LinearLayout(this);
        header.setOrientation(LinearLayout.HORIZONTAL);
        header.setGravity(Gravity.CENTER_VERTICAL);
        if (settingsDetailOpen) {
            ImageButton back = iconButton(R.drawable.ic_arrow_back, "Назад к настройкам");
            back.setOnClickListener(v -> {
                bounce(v);
                closeSettingsDetail();
            });
            LinearLayout.LayoutParams backParams = new LinearLayout.LayoutParams(dp(44), dp(44));
            backParams.rightMargin = dp(6);
            header.addView(back, backParams);
            header.addView(text(settingsSectionTitle(settingsSubTab), 22, text, true),
                    new LinearLayout.LayoutParams(0, -2, 1f));
        } else {
            LinearLayout titles = new LinearLayout(this);
            titles.setOrientation(LinearLayout.VERTICAL);
            titles.addView(text("Настройки", 25, text, true));
            TextView hint = text("Параметры сети применяются при следующем подключении.", 12, secondary, false);
            LinearLayout.LayoutParams hintParams = matchWrap();
            hintParams.topMargin = dp(4);
            titles.addView(hint, hintParams);
            header.addView(titles, new LinearLayout.LayoutParams(0, -2, 1f));
        }
        return header;
    }

    private String settingsSectionTitle(int tab) {
        switch (tab) {
            case SETTINGS_NETWORK: return "Сеть";
            case SETTINGS_APPS: return "Приложения";
            case SETTINGS_INTERFACE: return "Вид";
            case SETTINGS_ABOUT: return "О проекте";
            case SETTINGS_MODE:
            default: return "Режим работы";
        }
    }

    private View buildSettingsList() {
        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        LinearLayout list = new LinearLayout(this);
        list.setOrientation(LinearLayout.VERTICAL);
        list.setBackground(rounded(surface, border, 1, 12));
        list.addView(settingsListRow(R.drawable.ic_swap, "Режим работы",
                MODE_PROXY.equals(connectionMode) ? "Прокси (SOCKS5)" : "Туннель (весь трафик)", SETTINGS_MODE));
        addDivider(list);
        list.addView(settingsListRow(R.drawable.ic_public, "Сеть",
                "DNS-сервер и MTU", SETTINGS_NETWORK));
        addDivider(list);
        list.addView(settingsListRow(R.drawable.ic_apps, "Приложения",
                "Какие приложения используют туннель", SETTINGS_APPS));
        addDivider(list);
        list.addView(settingsListRow(R.drawable.ic_dark_mode, "Вид",
                "Тема и автопрокрутка логов", SETTINGS_INTERFACE));
        addDivider(list);
        list.addView(settingsListRow(R.drawable.ic_info, "О проекте",
                "Репозитории проекта", SETTINGS_ABOUT));
        scroll.addView(list, new ScrollView.LayoutParams(-1, -2));
        return scroll;
    }

    private void addDivider(LinearLayout parent) {
        addDivider(parent, dp(56));
    }

    private void addDivider(LinearLayout parent, int leftMargin) {
        View line = new View(this);
        line.setBackgroundColor(border);
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(-1, dp(1));
        params.leftMargin = leftMargin;
        parent.addView(line, params);
    }

    private View settingsListRow(int iconRes, String titleValue, String detailValue, int tab) {
        LinearLayout row = new LinearLayout(this);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(16), dp(14), dp(14), dp(14));
        row.setBackground(ripple(Color.TRANSPARENT, 0));
        row.setClickable(true);
        row.setFocusable(true);
        row.addView(icon(iconRes, accent), new LinearLayout.LayoutParams(dp(24), dp(24)));
        LinearLayout copy = new LinearLayout(this);
        copy.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams copyParams = new LinearLayout.LayoutParams(0, -2, 1f);
        copyParams.leftMargin = dp(16);
        copy.addView(text(titleValue, 15, text, true));
        copy.addView(text(detailValue, 12, secondary, false));
        row.addView(copy, copyParams);
        row.addView(icon(R.drawable.ic_chevron_right, hint), new LinearLayout.LayoutParams(dp(20), dp(20)));
        row.setOnClickListener(v -> {
            bounce(v);
            openSettingsDetail(tab);
        });
        return row;
    }

    private View buildSettingsSubTabContent() {
        switch (settingsSubTab) {
            case SETTINGS_NETWORK:
                return wrapScroll(buildNetworkSettings());
            case SETTINGS_APPS:
                return buildAppsSettings();
            case SETTINGS_INTERFACE:
                return wrapScroll(buildInterfaceSettings());
            case SETTINGS_ABOUT:
                return wrapScroll(buildAboutSettings());
            case SETTINGS_MODE:
            default:
                return wrapScroll(buildModeSettings());
        }
    }

    private View buildModeSettings() {
        LinearLayout section = page();
        TextView hint = text(
                "Туннель направляет через систему весь трафик устройства. "
                        + "Прокси поднимает локальный SOCKS5-сервер без запроса разрешения на туннель - "
                        + "адрес нужно указать вручную в приложениях, которые поддерживают прокси.",
                12, secondary, false);
        section.addView(hint, matchWrap());

        RadioGroup modeGroup = new RadioGroup(this);
        modeGroup.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams modeGroupParams = matchWrap();
        modeGroupParams.topMargin = dp(14);
        section.addView(modeGroup, modeGroupParams);

        RadioButton tunnelOption = modeRadio("Туннель - весь трафик устройства");
        RadioButton proxyOption = modeRadio("Прокси (SOCKS5) - без системного туннеля");
        modeGroup.addView(tunnelOption);
        modeGroup.addView(proxyOption);
        if (MODE_PROXY.equals(editorConnectionMode)) proxyOption.setChecked(true);
        else tunnelOption.setChecked(true);

        boolean proxySelected = MODE_PROXY.equals(editorConnectionMode);

        proxyPortInput = settingInput("Порт", String.valueOf(editorProxyPort), InputType.TYPE_CLASS_NUMBER);
        View portRow = settingRow(R.drawable.ic_swap, "Локальный порт SOCKS5", proxyPortInput);
        setInitialVisibility(portRow, proxySelected);
        LinearLayout.LayoutParams portParams = matchWrap();
        portParams.topMargin = dp(14);
        section.addView(portRow, portParams);

        Switch lanSwitch = settingSwitch(R.drawable.ic_public, "Доступ из локальной сети",
                "Прокси станет виден другим устройствам в этой же Wi-Fi/LAN", editorProxyLanAccess);
        View lanRow = (View) lanSwitch.getTag();
        setInitialVisibility(lanRow, proxySelected);
        LinearLayout.LayoutParams lanParams = matchWrap();
        lanParams.topMargin = dp(8);
        section.addView(lanRow, lanParams);

        String localIp = getLocalIpAddress();
        TextView lanAddressHint = text(
                localIp != null
                        ? "Адрес в сети: " + localIp + ":" + editorProxyPort
                        : "Не удалось определить IP - проверьте подключение к Wi-Fi",
                13, accent, true);
        lanAddressHint.setPadding(dp(14), dp(12), dp(14), dp(12));
        lanAddressHint.setBackground(rounded(darkMode ? Color.rgb(38, 50, 68) : Color.rgb(232, 240, 254),
                Color.TRANSPARENT, 0, 10));
        setInitialVisibility(lanAddressHint, proxySelected && editorProxyLanAccess);
        LinearLayout.LayoutParams lanAddressParams = matchWrap();
        lanAddressParams.topMargin = dp(8);
        section.addView(lanAddressHint, lanAddressParams);

        Switch authSwitch = settingSwitch(R.drawable.ic_lock, "Логин и пароль",
                "Требовать авторизацию для подключения к прокси", editorProxyAuthEnabled);
        View authRow = (View) authSwitch.getTag();
        setInitialVisibility(authRow, proxySelected && editorProxyLanAccess);
        LinearLayout.LayoutParams authParams = matchWrap();
        authParams.topMargin = dp(8);
        section.addView(authRow, authParams);
        View lanWarningHint = fieldHint(
                "Без пароля прокси в локальной сети открыт для всех: любой в этой Wi-Fi сможет "
                        + "ходить в интернет через ваш туннель.");
        setInitialVisibility(lanWarningHint, proxySelected && editorProxyLanAccess);
        section.addView(lanWarningHint);

        LinearLayout credentialsBlock = new LinearLayout(this);
        credentialsBlock.setOrientation(LinearLayout.VERTICAL);
        setInitialVisibility(credentialsBlock, proxySelected && editorProxyLanAccess && editorProxyAuthEnabled);
        LinearLayout.LayoutParams credentialsParams = matchWrap();
        credentialsParams.topMargin = dp(10);
        section.addView(credentialsBlock, credentialsParams);

        proxyUsernameInput = settingInput("Логин", proxyUsername, InputType.TYPE_CLASS_TEXT);
        credentialsBlock.addView(iconTextField(R.drawable.ic_person, proxyUsernameInput, null),
                new LinearLayout.LayoutParams(-1, dp(56)));
        LinearLayout.LayoutParams passwordParams = new LinearLayout.LayoutParams(-1, dp(56));
        passwordParams.topMargin = dp(8);
        credentialsBlock.addView(buildProxyPasswordField(), passwordParams);

        Button generateCreds = new Button(this);
        generateCreds.setText("Сгенерировать логин и пароль");
        generateCreds.setAllCaps(false);
        generateCreds.setTextColor(accent);
        generateCreds.setTextSize(13);
        generateCreds.setStateListAnimator(null);
        generateCreds.setBackground(ripple(Color.TRANSPARENT, 9));
        generateCreds.setOnClickListener(v -> {
            bounce(v);
            generateProxyCredentials();
        });
        LinearLayout.LayoutParams generateParams = new LinearLayout.LayoutParams(-1, dp(44));
        generateParams.topMargin = dp(2);
        credentialsBlock.addView(generateCreds, generateParams);

        View shareCard = buildProxyShareCard();
        setInitialVisibility(shareCard, proxySelected);
        LinearLayout.LayoutParams shareParams = matchWrap();
        shareParams.topMargin = dp(14);
        section.addView(shareCard, shareParams);

        TextView reliabilityTitle = label("НАДЁЖНОСТЬ");
        LinearLayout.LayoutParams reliabilityTitleParams = matchWrap();
        reliabilityTitleParams.topMargin = dp(26);
        reliabilityTitleParams.bottomMargin = dp(8);
        section.addView(reliabilityTitle, reliabilityTitleParams);

        LinearLayout batteryRow = cardRow(R.drawable.ic_power, "Отключить оптимизацию батареи",
                "Чтобы система не убивала соединение в фоне");
        batteryRow.setClickable(true);
        batteryRow.setFocusable(true);
        batteryRow.setOnClickListener(v -> {
            bounce(v);
            requestIgnoreBatteryOptimizations();
        });
        section.addView(batteryRow, matchWrap());

        LinearLayout alwaysOnRow = cardRow(R.drawable.ic_lock, "Настройки Always-on VPN",
                "Включите \"Блокировать соединения без VPN\" для защиты от утечек при обрыве");
        alwaysOnRow.setClickable(true);
        alwaysOnRow.setFocusable(true);
        alwaysOnRow.setOnClickListener(v -> {
            bounce(v);
            startActivity(new Intent(Settings.ACTION_VPN_SETTINGS));
        });
        setInitialVisibility(alwaysOnRow, !proxySelected);
        LinearLayout.LayoutParams alwaysOnParams = matchWrap();
        alwaysOnParams.topMargin = dp(8);
        section.addView(alwaysOnRow, alwaysOnParams);

        modeGroup.setOnCheckedChangeListener((group, checkedId) -> {
            tap(group);
            editorConnectionMode = checkedId == proxyOption.getId() ? MODE_PROXY : MODE_TUNNEL;
            boolean nowProxy = MODE_PROXY.equals(editorConnectionMode);
            setViewVisibleAnimated(portRow, nowProxy);
            setViewVisibleAnimated(lanRow, nowProxy);
            setViewVisibleAnimated(lanAddressHint, nowProxy && editorProxyLanAccess);
            setViewVisibleAnimated(authRow, nowProxy && editorProxyLanAccess);
            setViewVisibleAnimated(lanWarningHint, nowProxy && editorProxyLanAccess);
            setViewVisibleAnimated(credentialsBlock, nowProxy && editorProxyLanAccess && editorProxyAuthEnabled);
            setViewVisibleAnimated(shareCard, nowProxy);
            setViewVisibleAnimated(alwaysOnRow, !nowProxy);
        });

        lanSwitch.setOnCheckedChangeListener((button, checked) -> {
            tap(button);
            editorProxyLanAccess = checked;
            setViewVisibleAnimated(lanAddressHint, checked);
            setViewVisibleAnimated(authRow, checked);
            setViewVisibleAnimated(lanWarningHint, checked);
            setViewVisibleAnimated(credentialsBlock, checked && editorProxyAuthEnabled);
        });

        authSwitch.setOnCheckedChangeListener((button, checked) -> {
            tap(button);
            editorProxyAuthEnabled = checked;
            setViewVisibleAnimated(credentialsBlock, checked);
        });

        return section;
    }

    // Commits the Mode/Proxy draft fields to the live settings. Called by the
    // generic "Сохранить настройки" button in buildSettingsPage() - editing
    // these fields and pressing back without it discards the draft.
    private void applyModeSettings() {
        // If the mode is actually changing while the OLD mode's service is
        // still connected, isConnectionRunning() below would start checking
        // the NEW mode's (not yet started) service and show "not connected" -
        // while the old service keeps running unseen in the background,
        // holding the VPN slot or the proxy port. Stop it explicitly instead
        // of orphaning it.
        boolean modeChanging = !editorConnectionMode.equals(connectionMode);
        boolean oldProxyMode = isProxyMode();
        boolean oldWasRunning = isConnectionRunning();

        connectionMode = editorConnectionMode;
        proxyLanAccess = editorProxyLanAccess;
        proxyAuthEnabled = editorProxyAuthEnabled;
        int newPort;
        try {
            newPort = Integer.parseInt(proxyPortInput.getText().toString().trim());
        } catch (NumberFormatException ignored) {
            newPort = DEFAULT_PROXY_PORT;
        }
        proxyPort = Math.max(1024, Math.min(65535, newPort));
        proxyUsername = proxyUsernameInput.getText().toString().trim();
        proxyPassword = proxyPasswordInput.getText().toString().trim();
        persistSettings();

        if (modeChanging && oldWasRunning) {
            Intent stop = new Intent(this, oldProxyMode ? OpenFluxProxyService.class : OpenFluxTunnelService.class);
            stop.setAction(oldProxyMode ? OpenFluxProxyService.ACTION_STOP : OpenFluxTunnelService.ACTION_STOP);
            startService(stop);
            appendLog("Режим изменён - предыдущее соединение (" + (oldProxyMode ? "прокси" : "туннель") + ") остановлено");
        }
    }

    // buildProxyShareCard renders the socks:// link (and a QR encoding it)
    // that another device can use to add this proxy in an app like Happ or
    // Telegram, using whatever host/port/credentials are currently saved.
    // It reflects state as of when this settings screen was built, not live
    // as the user edits fields above - consistent with how other settings
    // here only take effect after being saved/reopened.
    private View buildProxyShareCard() {
        LinearLayout card = new LinearLayout(this);
        card.setOrientation(LinearLayout.VERTICAL);
        card.setPadding(dp(16), dp(14), dp(16), dp(14));
        card.setBackground(rounded(surface, border, 1, 11));
        card.addView(text("Ссылка для подключения с другого устройства", 12, secondary, false));

        String link = proxyShareLink();
        TextView linkView = text(link, 14, text, true);
        linkView.setTextIsSelectable(true);
        LinearLayout.LayoutParams linkParams = matchWrap();
        linkParams.topMargin = dp(6);
        card.addView(linkView, linkParams);

        Button copyButton = new Button(this);
        copyButton.setText("Копировать ссылку");
        copyButton.setAllCaps(false);
        copyButton.setTextColor(accent);
        copyButton.setTextSize(13);
        copyButton.setStateListAnimator(null);
        copyButton.setBackground(ripple(Color.TRANSPARENT, 9));
        copyButton.setOnClickListener(v -> {
            bounce(v);
            ClipboardManager clipboard = (ClipboardManager) getSystemService(CLIPBOARD_SERVICE);
            if (clipboard != null) {
                clipboard.setPrimaryClip(ClipData.newPlainText("OpenFlux SOCKS5", link));
            }
            Toast.makeText(this, "Ссылка скопирована", Toast.LENGTH_SHORT).show();
        });
        LinearLayout.LayoutParams copyParams = new LinearLayout.LayoutParams(-1, dp(40));
        copyParams.topMargin = dp(2);
        card.addView(copyButton, copyParams);

        Bitmap qr = generateQrBitmap(link, dp(180));
        if (qr != null) {
            ImageView qrView = new ImageView(this);
            qrView.setImageBitmap(qr);
            LinearLayout.LayoutParams qrParams = new LinearLayout.LayoutParams(dp(180), dp(180));
            qrParams.topMargin = dp(10);
            qrParams.gravity = Gravity.CENTER_HORIZONTAL;
            card.addView(qrView, qrParams);
        }

        return card;
    }

    private String proxyShareLink() {
        String host = proxyLanAccess ? getLocalIpAddress() : null;
        if (host == null) host = "127.0.0.1";
        String auth = "";
        if (proxyLanAccess && proxyAuthEnabled && !proxyUsername.isEmpty()) {
            auth = proxyUsername + ":" + proxyPassword + "@";
        }
        return "socks://" + auth + host + ":" + proxyPort;
    }

    private Bitmap generateQrBitmap(String content, int sizePx) {
        try {
            BitMatrix matrix = new QRCodeWriter().encode(content, BarcodeFormat.QR_CODE, sizePx, sizePx);
            int foreground = darkMode ? Color.WHITE : Color.BLACK;
            int background = darkMode ? Color.BLACK : Color.WHITE;
            Bitmap bitmap = Bitmap.createBitmap(sizePx, sizePx, Bitmap.Config.RGB_565);
            for (int x = 0; x < sizePx; x++) {
                for (int y = 0; y < sizePx; y++) {
                    bitmap.setPixel(x, y, matrix.get(x, y) ? foreground : background);
                }
            }
            return bitmap;
        } catch (WriterException exception) {
            return null;
        }
    }

    private void requestIgnoreBatteryOptimizations() {
        PowerManager powerManager = (PowerManager) getSystemService(POWER_SERVICE);
        if (powerManager != null && powerManager.isIgnoringBatteryOptimizations(getPackageName())) {
            Toast.makeText(this, "Оптимизация батареи уже отключена для приложения", Toast.LENGTH_SHORT).show();
            return;
        }
        try {
            startActivity(new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
                    Uri.parse("package:" + getPackageName())));
        } catch (Exception exception) {
            startActivity(new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS));
        }
    }

    // setInitialVisibility sets a view's starting visibility/alpha without
    // animating, for use when building a page (as opposed to
    // setViewVisibleAnimated, which is for reacting to a toggle afterwards).
    private void setInitialVisibility(View view, boolean visible) {
        view.setVisibility(visible ? View.VISIBLE : View.GONE);
        view.setAlpha(visible ? 1f : 0f);
    }

    private View buildProxyPasswordField() {
        proxyPasswordInput = settingInput("Пароль", proxyPassword,
                InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
        proxyPasswordInput.setTransformationMethod(
                proxyPasswordVisible ? null : PasswordTransformationMethod.getInstance());
        proxyPasswordVisibilityButton = iconButton(
                proxyPasswordVisible ? R.drawable.ic_visibility_off : R.drawable.ic_visibility,
                proxyPasswordVisible ? "Скрыть пароль" : "Показать пароль");
        proxyPasswordVisibilityButton.setOnClickListener(v -> {
            tap(v);
            toggleProxyPasswordVisibility();
        });
        return iconTextField(R.drawable.ic_key, proxyPasswordInput, proxyPasswordVisibilityButton);
    }

    // iconTextField lays out a leading icon and an EditText that fills the
    // rest of the row (with a reasonable gap between them, rather than
    // settingRow's separate caption + far-right fixed-width value box, which
    // doesn't read well when the "value" is itself the thing being typed),
    // plus an optional trailing action button (e.g. a show/hide toggle).
    private View iconTextField(int iconRes, EditText input, ImageButton trailingButton) {
        LinearLayout row = new LinearLayout(this);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(14), 0, trailingButton != null ? dp(2) : dp(14), 0);
        row.setBackground(rounded(surface, border, 1, 10));
        row.addView(icon(iconRes, secondary), new LinearLayout.LayoutParams(dp(20), dp(20)));
        input.setPadding(0, 0, 0, 0);
        LinearLayout.LayoutParams inputParams = new LinearLayout.LayoutParams(0, -1, 1f);
        inputParams.leftMargin = dp(12);
        row.addView(input, inputParams);
        if (trailingButton != null) {
            row.addView(trailingButton, new LinearLayout.LayoutParams(dp(44), dp(44)));
        }
        return row;
    }

    private void toggleProxyPasswordVisibility() {
        int position = proxyPasswordInput.getSelectionStart();
        proxyPasswordVisible = !proxyPasswordVisible;
        proxyPasswordInput.setTransformationMethod(
                proxyPasswordVisible ? null : PasswordTransformationMethod.getInstance());
        proxyPasswordInput.setTypeface(Typeface.DEFAULT);
        proxyPasswordVisibilityButton.setImageResource(
                proxyPasswordVisible ? R.drawable.ic_visibility_off : R.drawable.ic_visibility);
        proxyPasswordVisibilityButton.setContentDescription(
                proxyPasswordVisible ? "Скрыть пароль" : "Показать пароль");
        proxyPasswordInput.setSelection(Math.max(0, Math.min(position, proxyPasswordInput.length())));
    }

    // Only fills the (still-unsaved) input fields - applyModeSettings() is
    // what actually commits them, same as editing the fields by hand would.
    private void generateProxyCredentials() {
        byte[] randomPass = new byte[16];
        new SecureRandom().nextBytes(randomPass);
        String generatedUser = "user" + (100 + new SecureRandom().nextInt(900));
        String generatedPass = Base64.encodeToString(randomPass, Base64.NO_WRAP | Base64.NO_PADDING | Base64.URL_SAFE);
        if (proxyUsernameInput != null) proxyUsernameInput.setText(generatedUser);
        if (proxyPasswordInput != null) {
            proxyPasswordInput.setText(generatedPass);
            proxyPasswordInput.setSelection(proxyPasswordInput.length());
        }
        Toast.makeText(this, "Логин и пароль созданы - нажмите «Применить»", Toast.LENGTH_SHORT).show();
    }

    private View buildAboutSettings() {
        LinearLayout section = page();
        TextView intro = text(
                "OpenFlux - экспериментальный туннель-клиент поверх документ-транспорта. "
                        + "Это доработанный форк общедоступного проекта под Android.",
                13, secondary, false);
        section.addView(intro, matchWrap());

        LinearLayout.LayoutParams mainRepoParams = matchWrap();
        mainRepoParams.topMargin = dp(20);
        section.addView(aboutLinkRow("Основной репозиторий", "p1neappleXpress/OpenFlux", MAIN_REPO_URL),
                mainRepoParams);

        LinearLayout.LayoutParams forkParams = matchWrap();
        forkParams.topMargin = dp(10);
        section.addView(aboutLinkRow("Наш форк", "damnurmum/OpenFlux-Android", FORK_REPO_URL), forkParams);
        return section;
    }

    private View aboutLinkRow(String titleValue, String detailValue, String url) {
        LinearLayout row = cardRow(R.drawable.ic_link, titleValue, detailValue);
        row.setClickable(true);
        row.setFocusable(true);
        row.setOnClickListener(v -> {
            bounce(v);
            openUrl(url);
        });
        return row;
    }

    private void openUrl(String url) {
        try {
            startActivity(new Intent(Intent.ACTION_VIEW, android.net.Uri.parse(url)));
        } catch (Exception e) {
            Toast.makeText(this, "Не удалось открыть ссылку", Toast.LENGTH_SHORT).show();
        }
    }

    private View wrapScroll(View sectionContent) {
        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        scroll.addView(sectionContent, new ScrollView.LayoutParams(-1, -2));
        return scroll;
    }

    private View buildProfilesPage() {
        LinearLayout page = page();
        if (profileEditorOpen) {
            page.addView(buildProfileEditorHeader());
            LinearLayout.LayoutParams editorParams = new LinearLayout.LayoutParams(-1, 0, 1f);
            editorParams.topMargin = dp(16);
            page.addView(wrapScroll(buildProfileEditor()), editorParams);
            return page;
        }

        LinearLayout header = new LinearLayout(this);
        header.setGravity(Gravity.CENTER_VERTICAL);
        LinearLayout titles = new LinearLayout(this);
        titles.setOrientation(LinearLayout.VERTICAL);
        titles.addView(text("Профили", 25, text, true));
        titles.addView(text("Наборы параметров для разных серверов", 12, secondary, false), matchWrap());
        header.addView(titles, new LinearLayout.LayoutParams(0, -2, 1f));
        ImageButton addButton = iconButton(R.drawable.ic_add, "Добавить профиль");
        addButton.setOnClickListener(v -> {
            tap(v);
            openProfileEditor(null);
        });
        header.addView(addButton, new LinearLayout.LayoutParams(dp(44), dp(44)));
        page.addView(header);

        if (profiles.isEmpty()) {
            TextView empty = text("Пока нет ни одного профиля. Нажмите + и добавьте первый.", 13, secondary, false);
            LinearLayout.LayoutParams emptyParams = matchWrap();
            emptyParams.topMargin = dp(24);
            page.addView(empty, emptyParams);
            return wrapScroll(page);
        }

        LinearLayout list = new LinearLayout(this);
        list.setOrientation(LinearLayout.VERTICAL);
        list.setBackground(rounded(surface, border, 1, 12));
        for (int i = 0; i < profiles.size(); i++) {
            list.addView(buildProfileListRow(profiles.get(i)));
            if (i < profiles.size() - 1) addDivider(list, 0);
        }
        LinearLayout.LayoutParams listParams = matchWrap();
        listParams.topMargin = dp(20);
        page.addView(list, listParams);
        return wrapScroll(page);
    }

    private View buildProfileListRow(Profile p) {
        boolean selected = p.id == selectedProfileId;
        LinearLayout row = new LinearLayout(this);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(16), dp(10), dp(6), dp(10));
        row.setBackground(ripple(Color.TRANSPARENT, 0));
        row.setClickable(true);
        row.setFocusable(true);
        row.addView(icon(profileIconRes(p.icon), selected ? accent : secondary),
                new LinearLayout.LayoutParams(dp(24), dp(24)));
        LinearLayout copy = new LinearLayout(this);
        copy.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams copyParams = new LinearLayout.LayoutParams(0, -2, 1f);
        copyParams.leftMargin = dp(16);
        copy.addView(text(p.name, 15, text, selected));
        copy.addView(text(transportLabel(p.transportType) + (selected ? " · активен" : ""), 12, secondary, false));
        row.addView(copy, copyParams);
        ImageButton editButton = iconButton(R.drawable.ic_settings, "Изменить профиль «" + p.name + "»");
        editButton.setOnClickListener(v -> {
            tap(v);
            openProfileEditor(p);
        });
        row.addView(editButton, new LinearLayout.LayoutParams(dp(40), dp(40)));
        ImageButton deleteButton = iconButton(R.drawable.ic_delete, "Удалить профиль «" + p.name + "»");
        deleteButton.setOnClickListener(v -> {
            tap(v);
            deleteProfile(p);
        });
        row.addView(deleteButton, new LinearLayout.LayoutParams(dp(40), dp(40)));
        row.setOnClickListener(v -> {
            bounce(v);
            selectProfile(p.id);
        });
        return row;
    }

    private View buildProfileEditorHeader() {
        LinearLayout header = new LinearLayout(this);
        header.setGravity(Gravity.CENTER_VERTICAL);
        ImageButton back = iconButton(R.drawable.ic_arrow_back, "Назад к профилям");
        back.setOnClickListener(v -> {
            bounce(v);
            closeProfileEditor();
        });
        LinearLayout.LayoutParams backParams = new LinearLayout.LayoutParams(dp(44), dp(44));
        backParams.rightMargin = dp(6);
        header.addView(back, backParams);
        header.addView(text(editingProfileId != null ? "Изменить профиль" : "Новый профиль", 22, text, true),
                new LinearLayout.LayoutParams(0, -2, 1f));
        return header;
    }

    private View buildProfileEditor() {
        Profile existing = null;
        if (editingProfileId != null) {
            for (Profile p : profiles) if (p.id == editingProfileId) { existing = p; break; }
        }
        String initialUrl = existing != null ? existing.documentUrl : "";
        String initialSecret = existing != null ? existing.encryptionSecret : "";

        LinearLayout section = page();

        FrameLayout nameField = new FrameLayout(this);
        nameField.setBackground(rounded(surface, border, 1, 10));
        profileNameInput = settingInput("Название профиля", existing != null ? existing.name : "",
                InputType.TYPE_CLASS_TEXT);
        profileNameInput.setPadding(dp(16), 0, dp(16), 0);
        nameField.addView(profileNameInput, new FrameLayout.LayoutParams(-1, -1));
        section.addView(nameField, new LinearLayout.LayoutParams(-1, dp(56)));

        TextView iconLabel = label("ЗНАЧОК");
        LinearLayout.LayoutParams iconLabelParams = matchWrap();
        iconLabelParams.topMargin = dp(18);
        iconLabelParams.bottomMargin = dp(8);
        section.addView(iconLabel, iconLabelParams);
        section.addView(buildIconPicker(), matchWrap());

        TextView transportTypeLabel = label("ТРАНСПОРТ");
        LinearLayout.LayoutParams transportTypeLabelParams = matchWrap();
        transportTypeLabelParams.topMargin = dp(18);
        transportTypeLabelParams.bottomMargin = dp(8);
        section.addView(transportTypeLabel, transportTypeLabelParams);
        section.addView(buildTransportTypeSelector(), matchWrap());

        LinearLayout.LayoutParams maxFieldsParams = matchWrap();
        maxFieldsParams.topMargin = dp(8);
        maxFieldsContainer = buildMaxFields(existing);
        maxFieldsContainer.setVisibility("oneme".equals(editorTransportType) ? View.VISIBLE : View.GONE);
        section.addView(maxFieldsContainer, maxFieldsParams);

        TextView codecLabel = label("КОДЕК");
        LinearLayout.LayoutParams codecLabelParams = matchWrap();
        codecLabelParams.topMargin = dp(18);
        codecLabelParams.bottomMargin = dp(8);
        section.addView(codecLabel, codecLabelParams);
        section.addView(buildCodecSelector(), matchWrap());

        LinearLayout.LayoutParams urlParams = new LinearLayout.LayoutParams(-1, dp(56));
        urlParams.topMargin = dp(18);
        section.addView(buildUrlField(initialUrl), urlParams);

        LinearLayout.LayoutParams encryptionParams = new LinearLayout.LayoutParams(-1, dp(56));
        encryptionParams.topMargin = dp(8);
        section.addView(buildEncryptionField(initialSecret), encryptionParams);
        TextView encryptionHint = text(
                "Необязательно: оставьте пустым, чтобы подключаться без сквозного шифрования "
                        + "(например, к обычному exit-node апстрима). Если заполняете - нужен "
                        + "одинаковый секрет (минимум 16 символов) на телефоне и VDS.",
                11, secondary, false);
        LinearLayout.LayoutParams encryptionHintParams = matchWrap();
        encryptionHintParams.topMargin = dp(5);
        encryptionHintParams.leftMargin = dp(4);
        encryptionHintParams.rightMargin = dp(4);
        section.addView(encryptionHint, encryptionHintParams);
        Button generateKey = new Button(this);
        generateKey.setText("Сгенерировать безопасный ключ");
        generateKey.setAllCaps(false);
        generateKey.setTextColor(accent);
        generateKey.setTextSize(13);
        generateKey.setStateListAnimator(null);
        generateKey.setBackground(ripple(Color.TRANSPARENT, 9));
        generateKey.setOnClickListener(v -> {
            tap(v);
            generateEncryptionSecret();
        });
        LinearLayout.LayoutParams generateParams = new LinearLayout.LayoutParams(-1, dp(44));
        generateParams.topMargin = dp(4);
        section.addView(generateKey, generateParams);

        Button save = new Button(this);
        save.setText("Сохранить профиль");
        save.setAllCaps(false);
        save.setTextColor(Color.WHITE);
        save.setTextSize(15);
        save.setTypeface(Typeface.DEFAULT_BOLD);
        save.setStateListAnimator(null);
        save.setBackground(buttonBackground(Color.rgb(234, 26, 26), Color.rgb(179, 18, 18)));
        save.setOnClickListener(v -> {
            bounce(v);
            String name = profileNameInput.getText().toString().trim();
            String url = urlInput.getText().toString().trim();
            String secret = encryptionInput.getText().toString().trim();
            String token = maxTokenInput.getText().toString().trim();
            String uid = maxUidInput.getText().toString().trim();
            saveProfileFromEditor(name, url, secret, token, uid);
        });
        LinearLayout.LayoutParams saveParams = new LinearLayout.LayoutParams(-1, dp(52));
        saveParams.topMargin = dp(22);
        section.addView(save, saveParams);

        if (existing != null) {
            Profile toDelete = existing;
            Button delete = new Button(this);
            delete.setText("Удалить профиль");
            delete.setAllCaps(false);
            delete.setTextColor(darkMode ? Color.rgb(242, 139, 130) : Color.rgb(217, 48, 37));
            delete.setTextSize(14);
            delete.setStateListAnimator(null);
            delete.setBackground(ripple(Color.TRANSPARENT, 9));
            delete.setOnClickListener(v -> {
                tap(v);
                deleteProfile(toDelete);
            });
            LinearLayout.LayoutParams deleteParams = new LinearLayout.LayoutParams(-1, dp(44));
            deleteParams.topMargin = dp(6);
            deleteParams.bottomMargin = dp(12);
            section.addView(delete, deleteParams);
        }
        return section;
    }

    private View buildIconPicker() {
        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        View[] cells = new View[PROFILE_ICON_KEYS.length];
        ImageView[] iconViews = new ImageView[PROFILE_ICON_KEYS.length];
        for (int i = 0; i < PROFILE_ICON_KEYS.length; i++) {
            FrameLayout cell = new FrameLayout(this);
            ImageView iconView = icon(profileIconRes(PROFILE_ICON_KEYS[i]), secondary);
            cells[i] = cell;
            iconViews[i] = iconView;
            cell.addView(iconView, new FrameLayout.LayoutParams(dp(22), dp(22), Gravity.CENTER));
            cell.setClickable(true);
            LinearLayout.LayoutParams cellParams = new LinearLayout.LayoutParams(dp(42), dp(42));
            cellParams.rightMargin = dp(8);
            row.addView(cell, cellParams);
        }
        Runnable refreshCells = () -> {
            for (int i = 0; i < PROFILE_ICON_KEYS.length; i++) {
                boolean selected = PROFILE_ICON_KEYS[i].equals(editorIcon);
                cells[i].setBackground(rounded(selected ? accent : surface, border, 1, 10));
                iconViews[i].setImageTintList(ColorStateList.valueOf(selected ? Color.WHITE : secondary));
            }
        };
        refreshCells.run();
        for (int i = 0; i < PROFILE_ICON_KEYS.length; i++) {
            String key = PROFILE_ICON_KEYS[i];
            cells[i].setOnClickListener(v -> {
                tap(v);
                editorIcon = key;
                refreshCells.run();
            });
        }
        HorizontalScrollView scroll = new HorizontalScrollView(this);
        scroll.setHorizontalScrollBarEnabled(false);
        scroll.addView(row, new HorizontalScrollView.LayoutParams(-2, -2));
        return scroll;
    }

    private View buildTransportTypeSelector() {
        RadioGroup group = new RadioGroup(this);
        group.setOrientation(LinearLayout.VERTICAL);
        RadioButton yandexButton = modeRadio("Yandex Docs");
        RadioButton vyandexButton = modeRadio("Yandex Docs (Volga, экспериментальный)");
        RadioButton mailruButton = modeRadio("Mail.ru Docs");
        RadioButton cupsButton = modeRadio("Cups.online");
        RadioButton maxButton = modeRadio("MAX (OneMe)");
        group.addView(yandexButton);
        group.addView(vyandexButton);
        group.addView(mailruButton);
        group.addView(cupsButton);
        group.addView(maxButton);
        if ("vyandex".equals(editorTransportType)) vyandexButton.setChecked(true);
        else if ("mailru".equals(editorTransportType)) mailruButton.setChecked(true);
        else if ("cupsonline".equals(editorTransportType)) cupsButton.setChecked(true);
        else if ("oneme".equals(editorTransportType)) maxButton.setChecked(true);
        else yandexButton.setChecked(true);
        group.setOnCheckedChangeListener((g, checkedId) -> {
            tap(g);
            if (checkedId == vyandexButton.getId()) editorTransportType = "vyandex";
            else if (checkedId == mailruButton.getId()) editorTransportType = "mailru";
            else if (checkedId == cupsButton.getId()) editorTransportType = "cupsonline";
            else if (checkedId == maxButton.getId()) editorTransportType = "oneme";
            else editorTransportType = "yandex";
            if (maxFieldsContainer != null) {
                maxFieldsContainer.setVisibility("oneme".equals(editorTransportType) ? View.VISIBLE : View.GONE);
            }
        });
        return group;
    }

    // MAX (OneMe) authenticates via a web token + numeric user id instead of
    // a document URL - only shown/required when that transport is selected.
    private View buildMaxFields(Profile existing) {
        LinearLayout box = new LinearLayout(this);
        box.setOrientation(LinearLayout.VERTICAL);

        FrameLayout tokenField = new FrameLayout(this);
        tokenField.setBackground(rounded(surface, border, 1, 10));
        maxTokenInput = settingInput("MAX Web token", existing != null ? existing.maxToken : "",
                InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
        maxTokenInput.setPadding(dp(16), 0, dp(16), 0);
        tokenField.addView(maxTokenInput, new FrameLayout.LayoutParams(-1, -1));
        box.addView(tokenField, new LinearLayout.LayoutParams(-1, dp(56)));

        FrameLayout uidField = new FrameLayout(this);
        uidField.setBackground(rounded(surface, border, 1, 10));
        maxUidInput = settingInput("MAX call user id", existing != null ? existing.maxUid : "",
                InputType.TYPE_CLASS_NUMBER);
        maxUidInput.setPadding(dp(16), 0, dp(16), 0);
        uidField.addView(maxUidInput, new FrameLayout.LayoutParams(-1, -1));
        LinearLayout.LayoutParams uidParams = new LinearLayout.LayoutParams(-1, dp(56));
        uidParams.topMargin = dp(8);
        box.addView(uidField, uidParams);

        return box;
    }

    private View buildCodecSelector() {
        RadioGroup group = new RadioGroup(this);
        group.setOrientation(LinearLayout.VERTICAL);
        RadioButton batchedButton = modeRadio("Batched + zstd (по умолчанию)");
        RadioButton legacyButton = modeRadio("Legacy (LZ4, для совместимости со старым exit-node)");
        group.addView(batchedButton);
        group.addView(legacyButton);
        if ("legacy".equals(editorCodec)) legacyButton.setChecked(true);
        else batchedButton.setChecked(true);
        group.setOnCheckedChangeListener((g, checkedId) -> {
            tap(g);
            editorCodec = checkedId == legacyButton.getId() ? "legacy" : "batched";
        });
        return group;
    }

    private View buildNetworkSettings() {
        LinearLayout section = page();
        dnsInput = settingInput("DNS-сервер", editorDnsServer,
                InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        section.addView(settingRow(R.drawable.ic_public, "DNS-сервер", dnsInput));
        section.addView(fieldHint(
                "Для чего: сюда уходят запросы «какой IP у сайта», резолвится локально на "
                        + "устройстве. Можно указать IP (1.1.1.1) или доменное имя (dns.google)."));

        mtuInput = settingInput("MTU", String.valueOf(editorMtu), InputType.TYPE_CLASS_NUMBER);
        LinearLayout.LayoutParams mtuParams = matchWrap();
        mtuParams.topMargin = dp(16);
        section.addView(settingRow(R.drawable.ic_settings, "MTU пакета", mtuInput), mtuParams);
        section.addView(fieldHint(
                "Для чего: максимальный размер пакета в туннеле. Трогать не обязательно - "
                        + "уменьшите (например, до 1280), если сайты грузятся не полностью "
                        + "или соединение обрывается."));

        return section;
    }

    // Commits the Network draft fields to the live settings. Called by the
    // generic "Сохранить настройки" button in buildSettingsPage() - editing
    // these fields and pressing back without it discards the draft.
    private void applyNetworkSettings() {
        String newDns = dnsInput.getText().toString().trim();
        dnsServer = newDns.isEmpty() ? DEFAULT_DNS : newDns;
        int newMtu;
        try {
            newMtu = Integer.parseInt(mtuInput.getText().toString().trim());
        } catch (NumberFormatException ignored) {
            newMtu = DEFAULT_MTU;
        }
        mtu = Math.max(576, Math.min(1500, newMtu));
        persistSettings();
    }

    private TextView fieldHint(String value) {
        TextView hint = text(value, 11, secondary, false);
        LinearLayout.LayoutParams params = matchWrap();
        params.topMargin = dp(5);
        params.leftMargin = dp(4);
        params.rightMargin = dp(4);
        hint.setLayoutParams(params);
        return hint;
    }

    private View buildInterfaceSettings() {
        LinearLayout section = page();
        Switch themeSwitch = settingSwitch(R.drawable.ic_dark_mode, "Тёмная тема",
                "До первого выбора используется тема телефона", darkMode);
        themeSwitch.setOnCheckedChangeListener((button, checked) -> {
            tap(button);
            switchTheme(checked);
        });
        section.addView((View) themeSwitch.getTag());
        Switch scrollSwitch = settingSwitch(R.drawable.ic_terminal, "Автопрокрутка логов",
                "Показывать последние события", autoScroll);
        scrollSwitch.setOnCheckedChangeListener((button, checked) -> {
            tap(button);
            autoScroll = checked;
            getSharedPreferences(SETTINGS_PREFS_NAME, MODE_PRIVATE).edit().putBoolean("auto_scroll", checked).apply();
        });
        LinearLayout.LayoutParams scrollSettingParams = matchWrap();
        scrollSettingParams.topMargin = dp(8);
        section.addView((View) scrollSwitch.getTag(), scrollSettingParams);

        Switch showSensitiveSwitch = settingSwitch(R.drawable.ic_lock, "Данные в логах",
                "Показывать ссылки, IP и WSS адреса. При выключении скрываются под HIDDEN-URL",
                showSensitiveLogs);
        showSensitiveSwitch.setOnCheckedChangeListener((button, checked) -> {
            tap(button);
            showSensitiveLogs = checked;
            getSharedPreferences(SETTINGS_PREFS_NAME, MODE_PRIVATE).edit()
                    .putBoolean("show_sensitive_logs", checked).apply();
        });
        LinearLayout.LayoutParams showSensitiveParams = matchWrap();
        showSensitiveParams.topMargin = dp(8);
        section.addView((View) showSensitiveSwitch.getTag(), showSensitiveParams);
        return section;
    }

    private View buildAppsSettings() {
        LinearLayout section = page();
        TextView hint = text(
                "Выберите, какие приложения используют туннель. По умолчанию - все приложения, кроме OpenFlux. "
                        + "Действует только в режиме туннеля - в режиме прокси приложения подключаются к SOCKS5 сами.",
                12, secondary, false);
        section.addView(hint, matchWrap());

        RadioGroup modeGroup = new RadioGroup(this);
        modeGroup.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams modeGroupParams = matchWrap();
        modeGroupParams.topMargin = dp(12);
        section.addView(modeGroup, modeGroupParams);

        RadioButton offButton = modeRadio("Все приложения");
        RadioButton whitelistButton = modeRadio("Только выбранные (белый список)");
        RadioButton blacklistButton = modeRadio("Все, кроме выбранных (чёрный список)");
        modeGroup.addView(offButton);
        modeGroup.addView(whitelistButton);
        modeGroup.addView(blacklistButton);
        if (AppFilter.MODE_WHITELIST.equals(appFilterMode)) whitelistButton.setChecked(true);
        else if (AppFilter.MODE_BLACKLIST.equals(appFilterMode)) blacklistButton.setChecked(true);
        else offButton.setChecked(true);

        LinearLayout listContainer = new LinearLayout(this);
        listContainer.setOrientation(LinearLayout.VERTICAL);
        listContainer.setVisibility(AppFilter.MODE_OFF.equals(appFilterMode) ? View.GONE : View.VISIBLE);
        LinearLayout.LayoutParams listContainerParams = new LinearLayout.LayoutParams(-1, 0, 1f);
        listContainerParams.topMargin = dp(14);

        ListView appListView = new ListView(this);
        appListView.setDivider(null);
        appListView.setAdapter(new AppListAdapter(loadInstalledAppsCached()));
        listContainer.addView(appListView, new LinearLayout.LayoutParams(-1, -1));
        section.addView(listContainer, listContainerParams);

        modeGroup.setOnCheckedChangeListener((group, checkedId) -> {
            tap(group);
            if (checkedId == whitelistButton.getId()) appFilterMode = AppFilter.MODE_WHITELIST;
            else if (checkedId == blacklistButton.getId()) appFilterMode = AppFilter.MODE_BLACKLIST;
            else appFilterMode = AppFilter.MODE_OFF;
            setViewVisibleAnimated(listContainer, !AppFilter.MODE_OFF.equals(appFilterMode));
            persistAppFilter();
        });

        return section;
    }

    private void setViewVisibleAnimated(View view, boolean visible) {
        view.animate().cancel();
        if (visible) {
            view.setVisibility(View.VISIBLE);
            view.setAlpha(0f);
            view.setTranslationY(dp(10));
            view.animate().alpha(1f).translationY(0f).setDuration(220)
                    .setInterpolator(new DecelerateInterpolator()).start();
        } else {
            view.animate().alpha(0f).translationY(dp(10)).setDuration(150)
                    .withEndAction(() -> view.setVisibility(View.GONE)).start();
        }
    }

    private RadioButton modeRadio(String labelValue) {
        RadioButton button = new RadioButton(this);
        button.setId(View.generateViewId());
        button.setText(labelValue);
        button.setTextColor(text);
        button.setTextSize(14);
        button.setPadding(dp(6), dp(10), dp(6), dp(10));
        button.setButtonTintList(ColorStateList.valueOf(accent));
        return button;
    }

    private List<AppEntry> loadInstalledAppsCached() {
        if (installedAppsCache == null) installedAppsCache = loadInstalledApps();
        return installedAppsCache;
    }

    private List<AppEntry> loadInstalledApps() {
        Intent launcherIntent = new Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER);
        List<ResolveInfo> resolved = getPackageManager().queryIntentActivities(launcherIntent, 0);
        LinkedHashMap<String, AppEntry> byPackage = new LinkedHashMap<>();
        for (ResolveInfo info : resolved) {
            String packageName = info.activityInfo.packageName;
            if (packageName.equals(getPackageName()) || byPackage.containsKey(packageName)) continue;
            String label = info.loadLabel(getPackageManager()).toString();
            Drawable icon = info.loadIcon(getPackageManager());
            byPackage.put(packageName, new AppEntry(packageName, label, icon));
        }
        List<AppEntry> apps = new ArrayList<>(byPackage.values());
        Collections.sort(apps, Comparator.comparing(entry -> entry.label.toLowerCase()));
        return apps;
    }

    private void persistAppFilter() {
        appFilterPrefs.edit()
                .putString(AppFilter.KEY_MODE, appFilterMode)
                .putStringSet(AppFilter.KEY_PACKAGES, new HashSet<>(selectedApps))
                .apply();
    }

    private View buildAppRow() {
        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(10), dp(8), dp(10), dp(8));
        row.setBackground(ripple(Color.TRANSPARENT, 8));
        ImageView icon = new ImageView(this);
        row.addView(icon, new LinearLayout.LayoutParams(dp(36), dp(36)));
        TextView labelView = text("", 14, text, false);
        LinearLayout.LayoutParams labelParams = new LinearLayout.LayoutParams(0, -2, 1f);
        labelParams.leftMargin = dp(12);
        row.addView(labelView, labelParams);
        CheckBox checkBox = new CheckBox(this);
        checkBox.setButtonTintList(ColorStateList.valueOf(accent));
        row.addView(checkBox, new LinearLayout.LayoutParams(-2, -2));
        return row;
    }

    private static final class AppEntry {
        final String packageName;
        final String label;
        final Drawable icon;

        AppEntry(String packageName, String label, Drawable icon) {
            this.packageName = packageName;
            this.label = label;
            this.icon = icon;
        }
    }

    private static final class AppRowHolder {
        final ImageView icon;
        final TextView label;
        final CheckBox checkBox;

        AppRowHolder(View row) {
            LinearLayout layout = (LinearLayout) row;
            icon = (ImageView) layout.getChildAt(0);
            label = (TextView) layout.getChildAt(1);
            checkBox = (CheckBox) layout.getChildAt(2);
        }
    }

    private final class AppListAdapter extends BaseAdapter {
        private final List<AppEntry> apps;

        AppListAdapter(List<AppEntry> apps) {
            this.apps = apps;
        }

        @Override public int getCount() {
            return apps.size();
        }

        @Override public Object getItem(int position) {
            return apps.get(position);
        }

        @Override public long getItemId(int position) {
            return position;
        }

        @Override public View getView(int position, View convertView, ViewGroup parent) {
            View row;
            AppRowHolder holder;
            if (convertView != null && convertView.getTag() instanceof AppRowHolder) {
                row = convertView;
                holder = (AppRowHolder) row.getTag();
            } else {
                row = buildAppRow();
                holder = new AppRowHolder(row);
                row.setTag(holder);
            }
            AppEntry entry = apps.get(position);
            holder.icon.setImageDrawable(entry.icon);
            holder.label.setText(entry.label);
            holder.checkBox.setOnCheckedChangeListener(null);
            holder.checkBox.setChecked(selectedApps.contains(entry.packageName));
            holder.checkBox.setOnCheckedChangeListener((button, checked) -> {
                tap(button);
                if (checked) selectedApps.add(entry.packageName);
                else selectedApps.remove(entry.packageName);
                persistAppFilter();
            });
            row.setOnClickListener(v -> holder.checkBox.setChecked(!holder.checkBox.isChecked()));
            return row;
        }
    }

    private View buildUrlField(String initialValue) {
        FrameLayout field = new FrameLayout(this);
        field.setBackground(rounded(surface, border, 1, 10));
        urlInput = settingInput("HTTPS-ссылка на документ", initialValue,
                InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        urlInput.setTransformationMethod(urlVisible ? null : PasswordTransformationMethod.getInstance());
        urlInput.setPadding(dp(16), 0, dp(56), 0);
        field.addView(urlInput, new FrameLayout.LayoutParams(-1, -1));
        visibilityButton = iconButton(urlVisible ? R.drawable.ic_visibility_off : R.drawable.ic_visibility,
                urlVisible ? "Скрыть ссылку" : "Показать ссылку");
        visibilityButton.setOnClickListener(v -> {
            tap(v);
            toggleUrlVisibility();
        });
        FrameLayout.LayoutParams eye = new FrameLayout.LayoutParams(dp(48), dp(48), Gravity.END | Gravity.CENTER_VERTICAL);
        eye.rightMargin = dp(4);
        field.addView(visibilityButton, eye);
        return field;
    }

    private View buildEncryptionField(String initialValue) {
        FrameLayout field = new FrameLayout(this);
        field.setBackground(rounded(surface, border, 1, 10));
        encryptionInput = settingInput("Ключ сквозного шифрования (необязательно)", initialValue,
                InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
        encryptionInput.setTransformationMethod(encryptionVisible ? null : PasswordTransformationMethod.getInstance());
        encryptionInput.setPadding(dp(16), 0, dp(56), 0);
        field.addView(encryptionInput, new FrameLayout.LayoutParams(-1, -1));
        encryptionVisibilityButton = iconButton(
                encryptionVisible ? R.drawable.ic_visibility_off : R.drawable.ic_visibility,
                encryptionVisible ? "Скрыть ключ" : "Показать ключ");
        encryptionVisibilityButton.setOnClickListener(v -> {
            tap(v);
            toggleEncryptionVisibility();
        });
        FrameLayout.LayoutParams eye = new FrameLayout.LayoutParams(
                dp(48), dp(48), Gravity.END | Gravity.CENTER_VERTICAL);
        eye.rightMargin = dp(4);
        field.addView(encryptionVisibilityButton, eye);
        return field;
    }

    private LinearLayout cardRow(int iconRes, String titleValue, String detailValue) {
        LinearLayout row = new LinearLayout(this);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(16), dp(14), dp(16), dp(14));
        row.setBackground(rounded(surface, border, 1, 11));
        ImageView icon = icon(iconRes, accent);
        row.addView(icon, new LinearLayout.LayoutParams(dp(26), dp(26)));
        LinearLayout copy = new LinearLayout(this);
        copy.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams copyParams = new LinearLayout.LayoutParams(0, -2, 1f);
        copyParams.leftMargin = dp(14);
        copy.addView(text(titleValue, 15, text, true));
        copy.addView(text(detailValue, 12, secondary, false));
        row.addView(copy, copyParams);
        return row;
    }

    private View settingRow(int iconRes, String labelValue, EditText input) {
        LinearLayout row = new LinearLayout(this);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(14), dp(7), dp(10), dp(7));
        row.setBackground(rounded(surface, border, 1, 10));
        row.addView(icon(iconRes, secondary), new LinearLayout.LayoutParams(dp(24), dp(24)));
        TextView title = text(labelValue, 14, text, false);
        LinearLayout.LayoutParams titleParams = new LinearLayout.LayoutParams(0, -2, 1f);
        titleParams.leftMargin = dp(12);
        row.addView(title, titleParams);
        row.addView(input, new LinearLayout.LayoutParams(dp(120), dp(46)));
        return row;
    }

    private Switch settingSwitch(int iconRes, String titleValue, String detailValue, boolean checked) {
        LinearLayout row = new LinearLayout(this);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(14), dp(10), dp(10), dp(10));
        row.setBackground(rounded(surface, border, 1, 10));
        row.addView(icon(iconRes, secondary), new LinearLayout.LayoutParams(dp(24), dp(24)));
        LinearLayout copy = new LinearLayout(this);
        copy.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams copyParams = new LinearLayout.LayoutParams(0, -2, 1f);
        copyParams.leftMargin = dp(12);
        copy.addView(text(titleValue, 14, text, false));
        copy.addView(text(detailValue, 11, secondary, false));
        row.addView(copy, copyParams);
        Switch toggle = new Switch(this);
        toggle.setChecked(checked);
        toggle.setContentDescription(titleValue);
        row.addView(toggle, new LinearLayout.LayoutParams(-2, dp(42)));
        toggle.setTag(row);
        return toggle;
    }

    private EditText settingInput(String fieldHint, String value, int inputType) {
        EditText input = new EditText(this);
        input.setHint(fieldHint);
        input.setHintTextColor(hint);
        input.setText(value);
        input.setSingleLine(true);
        input.setTextSize(14);
        input.setTextColor(text);
        input.setInputType(inputType);
        input.setBackgroundColor(Color.TRANSPARENT);
        input.setPadding(dp(8), 0, dp(8), 0);
        return input;
    }

    private void switchTheme(boolean checked) {
        if (darkMode == checked) return;
        captureSettings();
        View oldRoot = root;
        oldRoot.animate().cancel();
        oldRoot.animate().alpha(0.15f).setDuration(110).withEndAction(() -> {
            darkMode = checked;
            getSharedPreferences(SETTINGS_PREFS_NAME, MODE_PRIVATE).edit().putBoolean("dark_mode", darkMode).apply();
            applyPalette();
            configureSystemBars();
            buildShell();
            showPage(currentPage);
        }).start();
    }

    private void toggleUrlVisibility() {
        int position = urlInput.getSelectionStart();
        urlVisible = !urlVisible;
        urlInput.setTransformationMethod(urlVisible ? null : PasswordTransformationMethod.getInstance());
        urlInput.setTypeface(Typeface.DEFAULT);
        visibilityButton.setImageResource(urlVisible ? R.drawable.ic_visibility_off : R.drawable.ic_visibility);
        visibilityButton.setContentDescription(urlVisible ? "Скрыть ссылку" : "Показать ссылку");
        urlInput.setSelection(Math.max(0, Math.min(position, urlInput.length())));
    }

    private void toggleEncryptionVisibility() {
        int position = encryptionInput.getSelectionStart();
        encryptionVisible = !encryptionVisible;
        encryptionInput.setTransformationMethod(
                encryptionVisible ? null : PasswordTransformationMethod.getInstance());
        encryptionInput.setTypeface(Typeface.DEFAULT);
        encryptionVisibilityButton.setImageResource(
                encryptionVisible ? R.drawable.ic_visibility_off : R.drawable.ic_visibility);
        encryptionVisibilityButton.setContentDescription(
                encryptionVisible ? "Скрыть ключ" : "Показать ключ");
        encryptionInput.setSelection(Math.max(0, Math.min(position, encryptionInput.length())));
    }

    private void generateEncryptionSecret() {
        byte[] random = new byte[32];
        new SecureRandom().nextBytes(random);
        String generated = Base64.encodeToString(
                random, Base64.NO_WRAP | Base64.NO_PADDING | Base64.URL_SAFE);
        if (encryptionInput != null) {
            encryptionInput.setText(generated);
            encryptionInput.setSelection(encryptionInput.length());
        }
        Toast.makeText(this, "Создан ключ на 256 бит. Передайте его на VDS.", Toast.LENGTH_LONG).show();
    }

    // Called on every navigation (showPage), not just when leaving a settings
    // page with unsaved edits - so it must NOT commit the Network/Mode draft
    // fields (applyNetworkSettings/applyModeSettings do that, only when the
    // save button is pressed). Just preserves the log scrollback text across
    // the page rebuild and drops view references before they're rebuilt.
    private void captureSettings() {
        captureLogs();
        urlInput = null;
        encryptionInput = null;
        profileNameInput = null;
        dnsInput = null;
        mtuInput = null;
        proxyPortInput = null;
        proxyUsernameInput = null;
        proxyPasswordInput = null;
        logView = null;
        logScroll = null;
    }

    private void captureLogs() {
        if (logView != null) logs = logView.getText().toString();
    }

    private void persistSettings() {
        if (dnsServer.isEmpty()) dnsServer = DEFAULT_DNS;
        secureSettings.putString("document_url", documentUrl);
        secureSettings.putString("encryption_secret", encryptionSecret);
        secureSettings.putString("proxy_password", proxyPassword);
        getSharedPreferences(SETTINGS_PREFS_NAME, MODE_PRIVATE).edit()
                .remove("connection_document_url")
                .putString("dns_server", dnsServer)
                .putInt("mtu", mtu)
                .putString("connection_mode", connectionMode)
                .putInt("proxy_port", proxyPort)
                .putBoolean("proxy_lan_access", proxyLanAccess)
                .putBoolean("proxy_auth_enabled", proxyAuthEnabled)
                .putString("proxy_username", proxyUsername)
                .putBoolean("auto_scroll", autoScroll)
                .putBoolean("dark_mode", darkMode)
                .putBoolean("show_sensitive_logs", showSensitiveLogs)
                .commit();
    }

    private boolean isProxyMode() {
        return MODE_PROXY.equals(connectionMode);
    }

    private boolean isConnectionRunning() {
        return isProxyMode() ? OpenFluxProxyService.isRunning() : OpenFluxTunnelService.isRunning();
    }

    private String connectionStatus() {
        return isProxyMode() ? OpenFluxProxyService.getStatus() : OpenFluxTunnelService.getStatus();
    }

    private String connectionLastError() {
        return isProxyMode() ? OpenFluxProxyService.getLastError() : OpenFluxTunnelService.getLastError();
    }

    private void toggleConnection() {
        if (isConnectionRunning()) {
            boolean proxyMode = isProxyMode();
            Intent stop = new Intent(this, proxyMode ? OpenFluxProxyService.class : OpenFluxTunnelService.class);
            stop.setAction(proxyMode ? OpenFluxProxyService.ACTION_STOP : OpenFluxTunnelService.ACTION_STOP);
            startService(stop);
            appendLog(proxyMode ? "Запрошена остановка прокси" : "Запрошена остановка туннеля");
            return;
        }
        boolean isMax = "oneme".equals(transportType);
        if (!isMax && !isValidDocumentUrl(documentUrl)) {
            Toast.makeText(this, "Выберите или создайте профиль с корректной HTTPS-ссылкой", Toast.LENGTH_LONG).show();
            showPage(PAGE_PROFILES);
            return;
        }
        if (isMax && (maxToken == null || maxToken.isEmpty())) {
            Toast.makeText(this, "Выберите или создайте профиль с MAX Web token", Toast.LENGTH_LONG).show();
            showPage(PAGE_PROFILES);
            return;
        }
        if (encryptionSecret != null && !encryptionSecret.isEmpty() && encryptionSecret.length() < 16) {
            Toast.makeText(this, "Ключ шифрования профиля должен быть не короче 16 символов, либо пустым", Toast.LENGTH_LONG).show();
            showPage(PAGE_PROFILES);
            return;
        }
        if (isProxyMode() && proxyLanAccess && proxyAuthEnabled
                && (proxyUsername.isEmpty() || proxyPassword.isEmpty())) {
            Toast.makeText(this, "Укажите логин и пароль для авторизации прокси", Toast.LENGTH_LONG).show();
            openSettingsDetail(SETTINGS_MODE);
            return;
        }
        persistSettings();
        if (isProxyMode()) {
            startProxy();
            return;
        }
        Intent permission = VpnService.prepare(this);
        if (permission != null) startActivityForResult(permission, TUNNEL_PERMISSION_REQUEST);
        else startTunnel();
    }

    @Override protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == TUNNEL_PERMISSION_REQUEST && resultCode == RESULT_OK) startTunnel();
        else if (requestCode == TUNNEL_PERMISSION_REQUEST) appendLog("[ERROR] Разрешение на создание туннеля не выдано");
    }

    private void startTunnel() {
        Intent intent = new Intent(this, OpenFluxTunnelService.class);
        intent.setAction(OpenFluxTunnelService.ACTION_START);
        intent.putExtra(OpenFluxTunnelService.EXTRA_DOCUMENT_URL, documentUrl);
        intent.putExtra(OpenFluxTunnelService.EXTRA_ENCRYPTION_SECRET, encryptionSecret);
        intent.putExtra(OpenFluxTunnelService.EXTRA_TRANSPORT_TYPE, transportType);
        intent.putExtra(OpenFluxTunnelService.EXTRA_CODEC, codec);
        intent.putExtra(OpenFluxTunnelService.EXTRA_MAX_TOKEN, maxToken);
        intent.putExtra(OpenFluxTunnelService.EXTRA_MAX_UID, maxUid);
        intent.putExtra(OpenFluxTunnelService.EXTRA_DNS_SERVER, dnsServer);
        intent.putExtra(OpenFluxTunnelService.EXTRA_MTU, mtu);
        startForegroundService(intent);
        appendLog("Запуск туннеля…");
    }

    private void startProxy() {
        Intent intent = new Intent(this, OpenFluxProxyService.class);
        intent.setAction(OpenFluxProxyService.ACTION_START);
        intent.putExtra(OpenFluxProxyService.EXTRA_DOCUMENT_URL, documentUrl);
        intent.putExtra(OpenFluxProxyService.EXTRA_ENCRYPTION_SECRET, encryptionSecret);
        intent.putExtra(OpenFluxProxyService.EXTRA_TRANSPORT_TYPE, transportType);
        intent.putExtra(OpenFluxProxyService.EXTRA_CODEC, codec);
        intent.putExtra(OpenFluxProxyService.EXTRA_MAX_TOKEN, maxToken);
        intent.putExtra(OpenFluxProxyService.EXTRA_MAX_UID, maxUid);
        intent.putExtra(OpenFluxProxyService.EXTRA_PORT, proxyPort);
        intent.putExtra(OpenFluxProxyService.EXTRA_LAN_ACCESS, proxyLanAccess);
        if (proxyLanAccess && proxyAuthEnabled) {
            intent.putExtra(OpenFluxProxyService.EXTRA_USERNAME, proxyUsername);
            intent.putExtra(OpenFluxProxyService.EXTRA_PASSWORD, proxyPassword);
        }
        startForegroundService(intent);
        appendLog("Запуск прокси…");
    }

    private void updateStatus() {
        if (statusView == null || tunnelButton == null) return;
        boolean proxyMode = isProxyMode();
        String state = connectionStatus();
        boolean running = isConnectionRunning();
        statusView.setText(state);
        tunnelButtonText.setText(running
                ? (proxyMode ? "Остановить прокси" : "Остановить туннель")
                : (proxyMode ? "Запустить прокси" : "Запустить туннель"));
        int stateColor;
        boolean transitional = false;
        long connectedAt = proxyMode ? OpenFluxProxyService.getConnectedAtMillis() : OpenFluxTunnelService.getConnectedAtMillis();
        if (uptimeView != null) {
            uptimeView.setText(connectedAt == 0L ? "" : formatUptime(System.currentTimeMillis() - connectedAt));
        }
        if ("Подключено".equals(state)) {
            stateColor = darkMode ? Color.rgb(129, 201, 149) : Color.rgb(24, 128, 56);
            statusDetail.setText(proxyMode
                    ? "SOCKS5 на 127.0.0.1:" + proxyPort
                    : "Трафик направляется через OpenFlux");
        } else if ("Ошибка".equals(state)) {
            stateColor = darkMode ? Color.rgb(242, 139, 130) : Color.rgb(217, 48, 37);
            statusDetail.setText("Откройте вкладку «Логи»");
        } else if (state != null && (state.contains("Подключ") || state.contains("Останав"))) {
            stateColor = darkMode ? Color.rgb(253, 214, 99) : Color.rgb(249, 171, 0);
            statusDetail.setText("Подождите несколько секунд…");
            transitional = true;
        } else {
            stateColor = Color.rgb(154, 160, 166);
            statusDetail.setText(proxyMode ? "Прокси сейчас не используется" : "Туннель сейчас не используется");
        }
        if (state != null && !state.equals(lastAnnouncedState)) {
            if ("Подключено".equals(state)) {
                vibrateSuccess();
                appendLog("[SUCCESS] Подключено (" + (proxyMode ? "прокси" : "туннель") + ")");
            } else if ("Ошибка".equals(state)) {
                vibrateError();
            }
            lastAnnouncedState = state;
        }

        statusDot.setBackground(rounded(stateColor, Color.TRANSPARENT, 0, 8));
        setStatusDotPulsing(transitional);

        // Not-running uses the brand red (matches the accent); running keeps
        // a distinct neutral tone instead of also going red, so "tap to
        // disconnect" doesn't read as an alarm/error state on top of the
        // now-red "tap to connect" button.
        int tunnelFill = running ? Color.rgb(66, 66, 66) : Color.rgb(234, 26, 26);
        int tunnelPressed = running ? Color.rgb(45, 45, 45) : Color.rgb(179, 18, 18);
        animateTunnelButtonFill(tunnelFill, tunnelPressed);

        String error = connectionLastError();
        if (error != null && !error.isEmpty() && !error.equals(lastShownError)) {
            lastShownError = error;
            appendLog("[ERROR] " + error);
        }
    }

    private String formatUptime(long millis) {
        long totalSeconds = millis / 1000;
        long hours = totalSeconds / 3600;
        long minutes = (totalSeconds % 3600) / 60;
        long seconds = totalSeconds % 60;
        return hours > 0
                ? String.format(java.util.Locale.US, "%d:%02d:%02d", hours, minutes, seconds)
                : String.format(java.util.Locale.US, "%02d:%02d", minutes, seconds);
    }

    private void setStatusDotPulsing(boolean pulsing) {
        if (statusDot == null) return;
        if (pulsing) {
            if (dotPulse != null && dotPulse.isRunning()) return;
            statusDot.setAlpha(1f);
            dotPulse = ObjectAnimator.ofFloat(statusDot, "alpha", 1f, 0.28f);
            dotPulse.setDuration(650);
            dotPulse.setRepeatMode(ValueAnimator.REVERSE);
            dotPulse.setRepeatCount(ValueAnimator.INFINITE);
            dotPulse.start();
        } else if (dotPulse != null) {
            dotPulse.cancel();
            dotPulse = null;
            statusDot.setAlpha(1f);
        }
    }

    private void animateTunnelButtonFill(int fill, int pressed) {
        if (tunnelButton == null) return;
        if (lastTunnelButtonFill == fill) return;
        int from = lastTunnelButtonFill == -1 ? fill : lastTunnelButtonFill;
        lastTunnelButtonFill = fill;
        ValueAnimator animator = ValueAnimator.ofArgb(from, fill);
        animator.setDuration(260);
        animator.addUpdateListener(a -> tunnelButton.setBackground(buttonBackground((int) a.getAnimatedValue(), pressed)));
        animator.start();
    }

    // getLocalIpAddress finds this device's IPv4 address on whatever network
    // it's currently attached to (Wi-Fi, a hotspot it joined, Ethernet, ...)
    // by scanning network interfaces directly, so it works the same way
    // regardless of connection type and needs no extra permission.
    private String getLocalIpAddress() {
        try {
            for (NetworkInterface intf : Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!intf.isUp() || intf.isLoopback()) continue;
                for (InetAddress address : Collections.list(intf.getInetAddresses())) {
                    if (!address.isLoopbackAddress() && address instanceof Inet4Address) {
                        return address.getHostAddress();
                    }
                }
            }
        } catch (SocketException ignored) {
        }
        return null;
    }

    private boolean isValidDocumentUrl(String value) {
        if (value == null || !value.startsWith("https://")) return false;
        try {
            android.net.Uri uri = android.net.Uri.parse(value);
            return uri.getHost() != null && !uri.getHost().isEmpty();
        } catch (Exception ignored) {
            return false;
        }
    }

    private static final String HIDDEN_URL_LABEL = "HIDDEN-URL";
    private static final Pattern SENSITIVE_URL_PATTERN = Pattern.compile("(?i)\\b(?:https?|wss?)://\\S+");
    private static final Pattern SENSITIVE_IP_PATTERN = Pattern.compile("\\b(?:\\d{1,3}\\.){3}\\d{1,3}(?::\\d{1,5})?\\b");
    // Catches bare hostnames without a scheme (e.g. the "WebSocket connected
    // to <host>" debug line, which logs transport.YandexDocsInfo.Host on its
    // own, never as a full wss:// URL). Yandex's own internal hostnames use
    // underscores in a label (e.g. "ota5..._vla_808_....sas.yp-c.yandex.net"),
    // which isn't valid DNS but does show up in these logs, so labels allow
    // '_' too - otherwise the match breaks there and only the tail after the
    // last underscore gets hidden.
    private static final Pattern SENSITIVE_HOST_PATTERN =
            Pattern.compile("\\b(?:[a-zA-Z0-9_](?:[a-zA-Z0-9_-]*[a-zA-Z0-9_])?\\.)+[a-zA-Z]{2,}\\b");

    private void appendLog(String value) {
        value = redactSensitive(value);
        String timestamp = "[" + new java.text.SimpleDateFormat("HH:mm:ss.SSS", java.util.Locale.US)
                .format(new java.util.Date()) + "]";
        String[] incoming = value.split("\n", -1);
        StringBuilder stamped = new StringBuilder();
        for (int i = 0; i < incoming.length; i++) {
            if (i > 0) stamped.append('\n');
            stamped.append(timestamp).append(' ').append(incoming[i]);
        }
        value = stamped.toString();
        if (!logs.isEmpty()) logs += "\n";
        logs += value;
        if (logs.length() > 60000) logs = logs.substring(logs.length() - 40000);
        if (logView != null) {
            logView.setText(colorizeLogs(logs));
            if (autoScroll && logScroll != null) logScroll.post(() -> logScroll.fullScroll(View.FOCUS_DOWN));
        }
    }

    // Document URLs are effectively passwords (docs/GUIDE*.md: "this link is
    // equivalent to your tunnel password"), and the transport's own debug lines
    // ([YDOCS]/[VOLGA]) print full URLs, WebSocket endpoints and resolved IPs
    // verbatim for diagnostics. Opt-in (off by default, Settings -> "Вид")
    // since it makes the log noisier and less useful for real debugging -
    // strips anything URL- or IP-shaped before the line ever reaches the
    // stored/displayed log text, so a screenshot or copy-paste can't leak it.
    private String redactSensitive(String value) {
        if (showSensitiveLogs) return value;
        value = SENSITIVE_URL_PATTERN.matcher(value).replaceAll(HIDDEN_URL_LABEL);
        value = SENSITIVE_IP_PATTERN.matcher(value).replaceAll(HIDDEN_URL_LABEL);
        value = SENSITIVE_HOST_PATTERN.matcher(value).replaceAll(HIDDEN_URL_LABEL);
        return value;
    }

    private static final Pattern LOG_TAG_PATTERN =
            Pattern.compile("^(?:\\[\\d{2}:\\d{2}:\\d{2}\\.\\d{3}\\] )?(\\[[A-Z0-9_]+\\])");

    // Colors the leading [TAG] of each log line so errors/successes/the
    // Yandex Docs transport's own debug tag stand out at a glance instead of
    // blending into a wall of monospace text.
    private int logTagColor(String tag) {
        switch (tag) {
            case "[ERROR]":
            case "[PANIC]":
                return darkMode ? Color.rgb(242, 139, 130) : Color.rgb(217, 48, 37);
            case "[SUCCESS]":
                return darkMode ? Color.rgb(129, 201, 149) : Color.rgb(24, 128, 56);
            case "[YDOCS]":
                return darkMode ? Color.rgb(253, 214, 99) : Color.rgb(249, 171, 0);
            case "[ANDROID]":
            case "[VOLGA]":
            case "[MAX]":
            case "[CUPS]":
            case "[M-DOCS]":
                return accent;
            default:
                return logColor;
        }
    }

    private CharSequence colorizeLogs(String rawLogs) {
        SpannableStringBuilder builder = new SpannableStringBuilder();
        String[] lines = rawLogs.split("\n", -1);
        for (int i = 0; i < lines.length; i++) {
            String line = lines[i];
            int start = builder.length();
            builder.append(line);
            Matcher matcher = LOG_TAG_PATTERN.matcher(line);
            if (matcher.find()) {
                builder.setSpan(new ForegroundColorSpan(logTagColor(matcher.group(1))),
                        start + matcher.start(1), start + matcher.end(1), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
            }
            int hiddenColor = darkMode ? Color.rgb(242, 139, 130) : Color.rgb(217, 48, 37);
            int searchFrom = 0;
            int idx;
            while ((idx = line.indexOf(HIDDEN_URL_LABEL, searchFrom)) >= 0) {
                builder.setSpan(new ForegroundColorSpan(hiddenColor),
                        start + idx, start + idx + HIDDEN_URL_LABEL.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
                searchFrom = idx + HIDDEN_URL_LABEL.length();
            }
            if (i < lines.length - 1) builder.append("\n");
        }
        return builder;
    }

    private LinearLayout page() {
        LinearLayout page = new LinearLayout(this);
        page.setOrientation(LinearLayout.VERTICAL);
        return page;
    }

    private TextView text(CharSequence value, int size, int color, boolean bold) {
        TextView view = new TextView(this);
        view.setText(value);
        view.setTextSize(size);
        view.setTextColor(color);
        if (bold) view.setTypeface(Typeface.DEFAULT_BOLD);
        return view;
    }

    private TextView label(String value) {
        TextView view = text(value, 11, secondary, true);
        view.setLetterSpacing(0.08f);
        return view;
    }

    private ImageView icon(int resource, int color) {
        ImageView view = new ImageView(this);
        view.setImageResource(resource);
        view.setImageTintList(ColorStateList.valueOf(color));
        return view;
    }

    private ImageButton iconButton(int resource, String description) {
        ImageButton button = new ImageButton(this);
        button.setImageResource(resource);
        button.setImageTintList(ColorStateList.valueOf(secondary));
        button.setContentDescription(description);
        button.setPadding(dp(11), dp(11), dp(11), dp(11));
        button.setBackground(ripple(Color.TRANSPARENT, 24));
        return button;
    }

    private GradientDrawable rounded(int fill, int stroke, int strokeWidth, int radius) {
        GradientDrawable drawable = new GradientDrawable();
        drawable.setColor(fill);
        drawable.setCornerRadius(dp(radius));
        if (strokeWidth > 0) drawable.setStroke(dp(strokeWidth), stroke);
        return drawable;
    }

    private RippleDrawable ripple(int fill, int radius) {
        return new RippleDrawable(ColorStateList.valueOf(darkMode ? 0x2FFFFFFF : 0x1F1A73E8),
                rounded(fill, Color.TRANSPARENT, 0, radius), rounded(Color.WHITE, Color.TRANSPARENT, 0, radius));
    }

    private RippleDrawable buttonBackground(int fill, int pressed) {
        return new RippleDrawable(ColorStateList.valueOf(pressed), rounded(fill, Color.TRANSPARENT, 0, 9),
                rounded(Color.WHITE, Color.TRANSPARENT, 0, 9));
    }

    // tap gives a light click haptic for a direct user interaction (button
    // press, toggle, list selection). Respects the system's haptic feedback
    // setting automatically and needs no permission.
    private void tap(View view) {
        view.performHapticFeedback(HapticFeedbackConstants.VIRTUAL_KEY);
    }

    // bounce gives a tap haptic plus a scale-down/scale-up feedback animation.
    // Repeated or rapid taps on the same view would otherwise stack multiple
    // overlapping ViewPropertyAnimator sequences (cancel() still runs a
    // pending withEndAction on API 23+), so any animation already running for
    // this exact view is fully cancelled and replaced before starting a new
    // one, and the scale is reset synchronously rather than relying on the
    // cancelled animation to leave it in a known state.
    private void bounce(View view) {
        tap(view);
        AnimatorSet running = bounceAnimators.remove(view);
        if (running != null) running.cancel();
        view.setScaleX(1f);
        view.setScaleY(1f);
        ObjectAnimator shrink = ObjectAnimator.ofPropertyValuesHolder(view,
                PropertyValuesHolder.ofFloat(View.SCALE_X, 0.96f),
                PropertyValuesHolder.ofFloat(View.SCALE_Y, 0.96f));
        shrink.setDuration(80);
        ObjectAnimator grow = ObjectAnimator.ofPropertyValuesHolder(view,
                PropertyValuesHolder.ofFloat(View.SCALE_X, 1f),
                PropertyValuesHolder.ofFloat(View.SCALE_Y, 1f));
        grow.setDuration(140);
        grow.setInterpolator(new OvershootInterpolator(3f));
        AnimatorSet set = new AnimatorSet();
        set.playSequentially(shrink, grow);
        set.addListener(new AnimatorListenerAdapter() {
            @Override public void onAnimationEnd(Animator animation) {
                bounceAnimators.remove(view);
            }
        });
        bounceAnimators.put(view, set);
        set.start();
    }

    // vibrateSuccess/vibrateError are for state changes that aren't a direct
    // touch response (e.g. the tunnel finishing connecting a second later),
    // so they go through the Vibrator instead of View.performHapticFeedback.
    private void vibrateSuccess() {
        if (vibrator == null || !vibrator.hasVibrator()) return;
        if (Build.VERSION.SDK_INT >= 29) {
            vibrator.vibrate(VibrationEffect.createPredefined(VibrationEffect.EFFECT_TICK));
        } else {
            vibrator.vibrate(VibrationEffect.createOneShot(35, VibrationEffect.DEFAULT_AMPLITUDE));
        }
    }

    private void vibrateError() {
        if (vibrator == null || !vibrator.hasVibrator()) return;
        vibrator.vibrate(VibrationEffect.createWaveform(new long[]{0, 45, 60, 45}, -1));
    }

    private LinearLayout.LayoutParams matchWrap() { return new LinearLayout.LayoutParams(-1, -2); }
    private LinearLayout.LayoutParams weighted() { return new LinearLayout.LayoutParams(0, -1, 1f); }
    private int dp(int value) { return Math.round(value * getResources().getDisplayMetrics().density); }
}
