package io.openflux.app;

import android.animation.ObjectAnimator;
import android.animation.ValueAnimator;
import android.app.Activity;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.ResolveInfo;
import android.content.res.ColorStateList;
import android.content.res.Configuration;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.Drawable;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.RippleDrawable;
import android.net.VpnService;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.os.SystemClock;
import android.os.VibrationEffect;
import android.os.Vibrator;
import android.text.InputType;
import android.text.method.PasswordTransformationMethod;
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
import android.widget.ImageButton;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ListView;
import android.widget.RadioButton;
import android.widget.RadioGroup;
import android.widget.ScrollView;
import android.widget.Switch;
import android.widget.TextView;
import android.widget.Toast;

import java.security.SecureRandom;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;

import android.util.Base64;

import io.openflux.bridge.mobile.Mobile;

public final class MainActivity extends Activity {
    private static final int VPN_PERMISSION_REQUEST = 42;
    private static final String DEFAULT_DNS = "1.1.1.1";
    private static final int DEFAULT_MTU = 1400;
    private static final int PAGE_HOME = 0;
    private static final int PAGE_LOGS = 1;
    private static final int PAGE_SETTINGS = 2;
    private static final int SETTINGS_TRANSPORT = 0;
    private static final int SETTINGS_NETWORK = 1;
    private static final int SETTINGS_APPS = 2;
    private static final int SETTINGS_INTERFACE = 3;

    private final Handler handler = new Handler(Looper.getMainLooper());
    private boolean darkMode;
    private boolean urlVisible;
    private boolean autoScroll = true;
    private int currentPage = PAGE_HOME;
    private int settingsSubTab = SETTINGS_TRANSPORT;
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
    private EditText dnsInput;
    private EditText mtuInput;
    private ImageButton visibilityButton;
    private ImageButton encryptionVisibilityButton;
    private TextView statusDot;
    private TextView statusView;
    private TextView statusDetail;
    private LinearLayout pingPanel;
    private TextView pingValue;
    private PingGraphView pingGraph;
    private TextView logView;
    private ScrollView logScroll;
    private LinearLayout vpnButton;
    private TextView vpnButtonText;
    private String documentUrl;
    private String encryptionSecret;
    private String dnsServer;
    private int mtu;
    private String logs = "";
    private String lastShownError = "";
    private boolean encryptionVisible;
    private SecureSettings secureSettings;
    private final ArrayList<Float> pingHistory = new ArrayList<>();
    private long lastPingRequestAt;
    private long lastPingSequence;
    private boolean pingPanelShown;
    private String lastCountry = "";
    private boolean shellAnimated;
    private ObjectAnimator dotPulse;
    private int lastVpnButtonFill = -1;
    private Vibrator vibrator;
    private String lastAnnouncedState = "";

    private SharedPreferences appFilterPrefs;
    private String appFilterMode = AppFilter.MODE_OFF;
    private final LinkedHashSet<String> selectedApps = new LinkedHashSet<>();
    private List<AppEntry> installedAppsCache;

    private final Runnable refresh = new Runnable() {
        @Override public void run() {
            updateStatus();
            updatePing();
            String pending = Mobile.readLogs();
            if (pending != null && !pending.isEmpty()) appendLog(pending);
            handler.postDelayed(this, 500);
        }
    };

    @Override protected void onCreate(Bundle state) {
        super.onCreate(state);
        vibrator = getSystemService(Vibrator.class);
        SharedPreferences prefs = getPreferences(MODE_PRIVATE);
        secureSettings = new SecureSettings(this);
        // Older prototype builds used plain preferences. Remove those values:
        // connection credentials now live only in the Keystore-backed store.
        prefs.edit().remove("document_url").remove("connection_document_url").apply();
        documentUrl = secureSettings.getString("document_url", "");
        encryptionSecret = secureSettings.getString("encryption_secret", "");
        dnsServer = prefs.getString("dns_server", DEFAULT_DNS);
        mtu = prefs.getInt("mtu", DEFAULT_MTU);
        autoScroll = prefs.getBoolean("auto_scroll", true);
        darkMode = prefs.contains("dark_mode")
                ? prefs.getBoolean("dark_mode", isSystemDark())
                : isSystemDark();
        appFilterPrefs = getSharedPreferences(AppFilter.PREFS_NAME, MODE_PRIVATE);
        appFilterMode = appFilterPrefs.getString(AppFilter.KEY_MODE, AppFilter.MODE_OFF);
        selectedApps.addAll(appFilterPrefs.getStringSet(AppFilter.KEY_PACKAGES, Collections.emptySet()));
        applyPalette();
        configureSystemBars();
        buildShell();
        showPage(PAGE_HOME);
        appendLog("Готово. При первом запуске Android запросит разрешение на VPN.");
    }

    @Override protected void onStart() {
        super.onStart();
        handler.removeCallbacks(refresh);
        handler.post(refresh);
    }

    @Override protected void onStop() {
        handler.removeCallbacks(refresh);
        readSettingsFromViews();
        persistSettings();
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
            accent = Color.rgb(138, 180, 248);
            hint = Color.rgb(154, 160, 166);
            logColor = Color.rgb(218, 220, 224);
        } else {
            background = Color.rgb(248, 249, 250);
            surface = Color.WHITE;
            text = Color.rgb(32, 33, 36);
            secondary = Color.rgb(95, 99, 104);
            border = Color.rgb(218, 220, 224);
            accent = Color.rgb(26, 115, 232);
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
        logo.setBackground(rounded(Color.rgb(43, 43, 43), Color.TRANSPARENT, 0, 10));
        logo.setImageResource(R.drawable.ic_openflux_foreground);
        logo.setClipToOutline(true);
        header.addView(logo, new LinearLayout.LayoutParams(dp(44), dp(44)));

        LinearLayout titles = new LinearLayout(this);
        titles.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams titlesParams = new LinearLayout.LayoutParams(0, -2, 1f);
        titlesParams.leftMargin = dp(12);
        TextView title = text("OpenFlux", 21, text, true);
        TextView subtitle = text("VPN через Yandex Docs", 12, secondary, false);
        titles.addView(title);
        titles.addView(subtitle);
        header.addView(titles, titlesParams);

        TextView beta = text("BETA", 10, accent, true);
        beta.setGravity(Gravity.CENTER);
        beta.setPadding(dp(9), dp(5), dp(9), dp(5));
        beta.setBackground(rounded(darkMode ? Color.rgb(38, 50, 68) : Color.rgb(232, 240, 254),
                Color.TRANSPARENT, 0, 12));
        header.addView(beta);
        return header;
    }

    private View buildBottomNav() {
        LinearLayout nav = new LinearLayout(this);
        nav.setOrientation(LinearLayout.HORIZONTAL);
        nav.setGravity(Gravity.CENTER);
        nav.setPadding(dp(4), dp(5), dp(4), dp(3));
        nav.setBackground(rounded(surface, border, 1, 16));
        nav.addView(navItem(R.drawable.ic_home, "Главная", PAGE_HOME), weighted());
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
            if (page != currentPage) tap(v);
            showPage(page);
        });
        return item;
    }

    private void showPage(int page) {
        captureSettings();
        currentPage = page;
        View pageView = page == PAGE_HOME ? buildHomePage()
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

    private void showSettingsSubTab(int tab) {
        if (tab == settingsSubTab) return;
        settingsSubTab = tab;
        showPage(PAGE_SETTINGS);
    }

    private View buildHomePage() {
        if (dotPulse != null) {
            dotPulse.cancel();
            dotPulse = null;
        }
        lastVpnButtonFill = -1;

        LinearLayout page = page();
        TextView heading = text("Подключение", 25, text, true);
        page.addView(heading);
        TextView intro = text("Защищённый системный VPN-туннель через документ-транспорт.", 13, secondary, false);
        LinearLayout.LayoutParams introParams = matchWrap();
        introParams.topMargin = dp(4);
        page.addView(intro, introParams);

        boolean documentConfigured = isValidDocumentUrl(documentUrl);
        boolean encryptionConfigured = encryptionSecret != null && encryptionSecret.length() >= 16;
        String transportTitle = documentConfigured ? "Yandex Docs" : "Документ не указан";
        String transportDetail = !documentConfigured
                ? "Укажите HTTPS-ссылку во вкладке «Настройки»"
                : encryptionConfigured
                ? "Документ и сквозное шифрование настроены"
                : "Укажите ключ сквозного шифрования";
        LinearLayout transport = cardRow(R.drawable.ic_link, transportTitle, transportDetail);
        transport.setClickable(true);
        transport.setFocusable(true);
        transport.setOnClickListener(v -> showPage(PAGE_SETTINGS));
        LinearLayout.LayoutParams transportParams = matchWrap();
        transportParams.topMargin = dp(28);
        page.addView(transport, transportParams);
        staggerIn(transport, 30);

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
        pingPanelShown = false;
        pingPanel = new LinearLayout(this);
        pingPanel.setOrientation(LinearLayout.VERTICAL);
        pingPanel.setVisibility(View.GONE);
        pingPanel.setAlpha(0f);
        pingValue = text("Пинг до VDS: —", 13, accent, true);
        pingPanel.addView(pingValue);
        pingGraph = new PingGraphView(this, accent, border);
        pingGraph.setHistory(pingHistory);
        LinearLayout.LayoutParams graphParams = new LinearLayout.LayoutParams(-1, dp(48));
        graphParams.topMargin = dp(5);
        graphParams.bottomMargin = dp(9);
        pingPanel.addView(pingGraph, graphParams);
        statusCopy.addView(pingPanel, new LinearLayout.LayoutParams(-1, -2));
        statusView = text("Остановлено", 17, text, true);
        statusDetail = text("VPN сейчас не используется", 13, secondary, false);
        statusCopy.addView(statusView);
        statusCopy.addView(statusDetail);
        status.addView(statusCopy, new LinearLayout.LayoutParams(0, -2, 1f));
        LinearLayout.LayoutParams statusParams = matchWrap();
        statusParams.topMargin = dp(14);
        page.addView(status, statusParams);
        staggerIn(status, 80);

        vpnButton = new LinearLayout(this);
        vpnButton.setOrientation(LinearLayout.HORIZONTAL);
        vpnButton.setGravity(Gravity.CENTER);
        vpnButton.setClickable(true);
        vpnButton.setFocusable(true);
        vpnButton.setElevation(dp(2));
        ImageView powerIcon = icon(R.drawable.ic_power, Color.WHITE);
        LinearLayout.LayoutParams powerParams = new LinearLayout.LayoutParams(dp(24), dp(24));
        powerParams.rightMargin = dp(10);
        vpnButton.addView(powerIcon, powerParams);
        vpnButtonText = text("Запустить VPN", 16, Color.WHITE, true);
        vpnButton.addView(vpnButtonText, new LinearLayout.LayoutParams(-2, -2));
        vpnButton.setOnClickListener(v -> {
            tap(v);
            v.animate().cancel();
            v.animate().scaleX(0.96f).scaleY(0.96f).setDuration(80).withEndAction(() ->
                    v.animate().scaleX(1f).scaleY(1f).setDuration(140)
                            .setInterpolator(new OvershootInterpolator(3f)).start()).start();
            toggleVpn();
        });
        LinearLayout.LayoutParams buttonParams = new LinearLayout.LayoutParams(-1, dp(58));
        buttonParams.topMargin = dp(16);
        page.addView(vpnButton, buttonParams);
        staggerIn(vpnButton, 130);

        TextView summaryTitle = label("АКТИВНЫЕ ПАРАМЕТРЫ");
        LinearLayout.LayoutParams summaryTitleParams = matchWrap();
        summaryTitleParams.topMargin = dp(30);
        summaryTitleParams.bottomMargin = dp(8);
        page.addView(summaryTitle, summaryTitleParams);
        View activeParams = infoCard("DNS-сервер", dnsServer, "MTU пакета", String.valueOf(mtu));
        page.addView(activeParams);
        staggerIn(activeParams, 180);
        return page;
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

        logView = text(logs, 12, logColor, false);
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
        TextView note = text("Логи хранятся только до закрытия приложения.", 11, secondary, false);
        note.setGravity(Gravity.CENTER);
        page.addView(note);
        return page;
    }

    private View buildSettingsPage() {
        LinearLayout page = page();
        page.addView(text("Настройки", 25, text, true));
        TextView restartHint = text("Параметры сети применяются при следующем подключении.", 12, secondary, false);
        LinearLayout.LayoutParams hintParams = matchWrap();
        hintParams.topMargin = dp(4);
        hintParams.bottomMargin = dp(16);
        page.addView(restartHint, hintParams);

        page.addView(buildSettingsTabStrip());

        LinearLayout.LayoutParams contentParams = new LinearLayout.LayoutParams(-1, 0, 1f);
        contentParams.topMargin = dp(14);
        page.addView(buildSettingsSubTabContent(), contentParams);

        Button save = new Button(this);
        save.setText("Сохранить настройки");
        save.setAllCaps(false);
        save.setTextColor(Color.WHITE);
        save.setTextSize(15);
        save.setTypeface(Typeface.DEFAULT_BOLD);
        save.setStateListAnimator(null);
        save.setBackground(buttonBackground(Color.rgb(26, 115, 232), Color.rgb(23, 78, 166)));
        save.setOnClickListener(v -> {
            tap(v);
            readSettingsFromViews();
            persistSettings();
            Toast.makeText(this, "Настройки сохранены", Toast.LENGTH_SHORT).show();
        });
        LinearLayout.LayoutParams saveParams = new LinearLayout.LayoutParams(-1, dp(52));
        saveParams.topMargin = dp(14);
        saveParams.bottomMargin = dp(12);
        page.addView(save, saveParams);
        return page;
    }

    private View buildSettingsTabStrip() {
        LinearLayout strip = new LinearLayout(this);
        strip.setOrientation(LinearLayout.HORIZONTAL);
        strip.setBackground(rounded(surface, border, 1, 12));
        strip.setPadding(dp(4), dp(4), dp(4), dp(4));
        strip.addView(settingsTabItem("Транспорт", SETTINGS_TRANSPORT), weighted());
        strip.addView(settingsTabItem("Сеть", SETTINGS_NETWORK), weighted());
        strip.addView(settingsTabItem("Приложения", SETTINGS_APPS), weighted());
        strip.addView(settingsTabItem("Вид", SETTINGS_INTERFACE), weighted());
        return strip;
    }

    private View settingsTabItem(String labelValue, int tab) {
        boolean active = tab == settingsSubTab;
        TextView item = text(labelValue, 12, active ? Color.WHITE : secondary, active);
        item.setGravity(Gravity.CENTER);
        item.setPadding(dp(6), dp(9), dp(6), dp(9));
        item.setBackground(active ? rounded(accent, Color.TRANSPARENT, 0, 9) : ripple(Color.TRANSPARENT, 9));
        item.setOnClickListener(v -> {
            if (tab != settingsSubTab) tap(v);
            showSettingsSubTab(tab);
        });
        if (active) {
            item.setScaleX(0.88f);
            item.setScaleY(0.88f);
            item.animate().scaleX(1f).scaleY(1f).setDuration(220)
                    .setInterpolator(new OvershootInterpolator(3f)).start();
        }
        return item;
    }

    private View buildSettingsSubTabContent() {
        switch (settingsSubTab) {
            case SETTINGS_NETWORK:
                return wrapScroll(buildNetworkSettings());
            case SETTINGS_APPS:
                return buildAppsSettings();
            case SETTINGS_INTERFACE:
                return wrapScroll(buildInterfaceSettings());
            case SETTINGS_TRANSPORT:
            default:
                return wrapScroll(buildTransportSettings());
        }
    }

    private View wrapScroll(View sectionContent) {
        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        scroll.addView(sectionContent, new ScrollView.LayoutParams(-1, -2));
        return scroll;
    }

    private View buildTransportSettings() {
        LinearLayout section = page();
        section.addView(buildUrlField(), new LinearLayout.LayoutParams(-1, dp(56)));

        LinearLayout.LayoutParams encryptionParams = new LinearLayout.LayoutParams(-1, dp(56));
        encryptionParams.topMargin = dp(8);
        section.addView(buildEncryptionField(), encryptionParams);
        TextView encryptionHint = text(
                "Одинаковый секрет (минимум 16 символов) должен быть настроен на телефоне и VDS.",
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
        return section;
    }

    private View buildNetworkSettings() {
        LinearLayout section = page();
        dnsInput = settingInput("DNS-сервер", dnsServer, InputType.TYPE_CLASS_PHONE);
        section.addView(settingRow(R.drawable.ic_public, "DNS-сервер", dnsInput));
        mtuInput = settingInput("MTU", String.valueOf(mtu), InputType.TYPE_CLASS_NUMBER);
        LinearLayout.LayoutParams mtuParams = matchWrap();
        mtuParams.topMargin = dp(8);
        section.addView(settingRow(R.drawable.ic_settings, "MTU пакета", mtuInput), mtuParams);
        return section;
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
            getPreferences(MODE_PRIVATE).edit().putBoolean("auto_scroll", checked).apply();
        });
        LinearLayout.LayoutParams scrollSettingParams = matchWrap();
        scrollSettingParams.topMargin = dp(8);
        section.addView((View) scrollSwitch.getTag(), scrollSettingParams);
        return section;
    }

    private View buildAppsSettings() {
        LinearLayout section = page();
        TextView hint = text(
                "Выберите, какие приложения используют VPN-туннель. По умолчанию — все приложения, кроме OpenFlux.",
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

    private View buildUrlField() {
        FrameLayout field = new FrameLayout(this);
        field.setBackground(rounded(surface, border, 1, 10));
        urlInput = settingInput("HTTPS-ссылка на документ", documentUrl,
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

    private View buildEncryptionField() {
        FrameLayout field = new FrameLayout(this);
        field.setBackground(rounded(surface, border, 1, 10));
        encryptionInput = settingInput("Ключ сквозного шифрования", encryptionSecret,
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

    private View infoCard(String leftTitle, String leftValue, String rightTitle, String rightValue) {
        LinearLayout card = new LinearLayout(this);
        card.setPadding(dp(16), dp(14), dp(16), dp(14));
        card.setBackground(rounded(surface, border, 1, 11));
        card.addView(infoColumn(leftTitle, leftValue), weighted());
        card.addView(infoColumn(rightTitle, rightValue), weighted());
        return card;
    }

    private View infoColumn(String titleValue, String value) {
        LinearLayout column = new LinearLayout(this);
        column.setOrientation(LinearLayout.VERTICAL);
        column.addView(text(titleValue, 11, secondary, false));
        TextView valueView = text(value, 16, text, true);
        LinearLayout.LayoutParams valueParams = matchWrap();
        valueParams.topMargin = dp(3);
        column.addView(valueView, valueParams);
        return column;
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
            getPreferences(MODE_PRIVATE).edit().putBoolean("dark_mode", darkMode).apply();
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
        encryptionSecret = Base64.encodeToString(
                random, Base64.NO_WRAP | Base64.NO_PADDING | Base64.URL_SAFE);
        if (encryptionInput != null) {
            encryptionInput.setText(encryptionSecret);
            encryptionInput.setSelection(encryptionInput.length());
        }
        persistSettings();
        Toast.makeText(this, "Создан ключ на 256 бит. Передайте его на VDS.", Toast.LENGTH_LONG).show();
    }

    private void captureSettings() {
        readSettingsFromViews();
        persistSettings();
        urlInput = null;
        encryptionInput = null;
        dnsInput = null;
        mtuInput = null;
        logView = null;
        logScroll = null;
    }

    private void readSettingsFromViews() {
        if (urlInput != null) documentUrl = urlInput.getText().toString().trim();
        if (encryptionInput != null) encryptionSecret = encryptionInput.getText().toString().trim();
        if (dnsInput != null) dnsServer = dnsInput.getText().toString().trim();
        if (mtuInput != null) {
            try { mtu = Integer.parseInt(mtuInput.getText().toString()); }
            catch (NumberFormatException ignored) { mtu = DEFAULT_MTU; }
            mtu = Math.max(576, Math.min(1500, mtu));
        }
        if (logView != null) logs = logView.getText().toString();
    }

    private void persistSettings() {
        if (dnsServer.isEmpty()) dnsServer = DEFAULT_DNS;
        secureSettings.putString("document_url", documentUrl);
        secureSettings.putString("encryption_secret", encryptionSecret);
        getPreferences(MODE_PRIVATE).edit()
                .remove("connection_document_url")
                .putString("dns_server", dnsServer)
                .putInt("mtu", mtu)
                .putBoolean("auto_scroll", autoScroll)
                .putBoolean("dark_mode", darkMode)
                .commit();
    }

    private void toggleVpn() {
        if (OpenFluxVpnService.isRunning()) {
            Intent stop = new Intent(this, OpenFluxVpnService.class);
            stop.setAction(OpenFluxVpnService.ACTION_STOP);
            startService(stop);
            appendLog("Запрошена остановка VPN");
            return;
        }
        if (!isValidDocumentUrl(documentUrl)) {
            Toast.makeText(this, "Укажите корректную HTTPS-ссылку в настройках", Toast.LENGTH_LONG).show();
            showPage(PAGE_SETTINGS);
            return;
        }
        if (encryptionSecret == null || encryptionSecret.length() < 16) {
            Toast.makeText(this, "Укажите ключ шифрования: минимум 16 символов", Toast.LENGTH_LONG).show();
            showPage(PAGE_SETTINGS);
            return;
        }
        persistSettings();
        Intent permission = VpnService.prepare(this);
        if (permission != null) startActivityForResult(permission, VPN_PERMISSION_REQUEST);
        else startVpn();
    }

    @Override protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == VPN_PERMISSION_REQUEST && resultCode == RESULT_OK) startVpn();
        else if (requestCode == VPN_PERMISSION_REQUEST) appendLog("Разрешение на создание VPN не выдано");
    }

    private void startVpn() {
        Intent intent = new Intent(this, OpenFluxVpnService.class);
        intent.setAction(OpenFluxVpnService.ACTION_START);
        intent.putExtra(OpenFluxVpnService.EXTRA_DOCUMENT_URL, documentUrl);
        intent.putExtra(OpenFluxVpnService.EXTRA_ENCRYPTION_SECRET, encryptionSecret);
        intent.putExtra(OpenFluxVpnService.EXTRA_DNS_SERVER, dnsServer);
        intent.putExtra(OpenFluxVpnService.EXTRA_MTU, mtu);
        startForegroundService(intent);
        appendLog("Запуск VPN…");
    }

    private void updateStatus() {
        if (statusView == null || vpnButton == null) return;
        String state = OpenFluxVpnService.getStatus();
        boolean running = OpenFluxVpnService.isRunning();
        statusView.setText(state);
        vpnButtonText.setText(running ? "Остановить VPN" : "Запустить VPN");
        int stateColor;
        boolean transitional = false;
        if ("Подключено".equals(state)) {
            stateColor = darkMode ? Color.rgb(129, 201, 149) : Color.rgb(24, 128, 56);
            statusDetail.setText("Трафик направляется через OpenFlux");
        } else if ("Ошибка".equals(state)) {
            stateColor = darkMode ? Color.rgb(242, 139, 130) : Color.rgb(217, 48, 37);
            statusDetail.setText("Откройте вкладку «Логи»");
        } else if (state != null && (state.contains("Подключ") || state.contains("Останав"))) {
            stateColor = darkMode ? Color.rgb(253, 214, 99) : Color.rgb(249, 171, 0);
            statusDetail.setText("Подождите несколько секунд…");
            transitional = true;
        } else {
            stateColor = Color.rgb(154, 160, 166);
            statusDetail.setText("VPN сейчас не используется");
        }
        if (state != null && !state.equals(lastAnnouncedState)) {
            if ("Подключено".equals(state)) vibrateSuccess();
            else if ("Ошибка".equals(state)) vibrateError();
            lastAnnouncedState = state;
        }

        statusDot.setBackground(rounded(stateColor, Color.TRANSPARENT, 0, 8));
        setStatusDotPulsing(transitional);

        int vpnFill = running ? Color.rgb(217, 48, 37) : Color.rgb(26, 115, 232);
        int vpnPressed = running ? Color.rgb(183, 28, 28) : Color.rgb(23, 78, 166);
        animateVpnButtonFill(vpnFill, vpnPressed);

        String error = OpenFluxVpnService.getLastError();
        if (error != null && !error.isEmpty() && !error.equals(lastShownError)) {
            lastShownError = error;
            appendLog("Ошибка: " + error);
        }
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

    private void animateVpnButtonFill(int fill, int pressed) {
        if (vpnButton == null) return;
        if (lastVpnButtonFill == fill) return;
        int from = lastVpnButtonFill == -1 ? fill : lastVpnButtonFill;
        lastVpnButtonFill = fill;
        ValueAnimator animator = ValueAnimator.ofArgb(from, fill);
        animator.setDuration(260);
        animator.addUpdateListener(a -> vpnButton.setBackground(buttonBackground((int) a.getAnimatedValue(), pressed)));
        animator.start();
    }

    private void updatePing() {
        boolean connected = OpenFluxVpnService.isRunning()
                && "Подключено".equals(OpenFluxVpnService.getStatus());
        if (!connected) {
            lastPingRequestAt = 0;
            lastPingSequence = 0;
            lastCountry = "";
            setPingPanelVisible(false);
            return;
        }

        setPingPanelVisible(true);
        long now = SystemClock.elapsedRealtime();
        if (now - lastPingRequestAt >= 2000) {
            lastPingRequestAt = now;
            Mobile.ping();
        }
        long sequence = Mobile.pingSequence();
        if (sequence == 0 || sequence == lastPingSequence) return;
        lastPingSequence = sequence;
        long milliseconds = Mobile.pingMillis();
        if (milliseconds < 0) return;
        if (pingHistory.size() >= 32) pingHistory.remove(0);
        pingHistory.add((float) milliseconds);
        String country = Mobile.serverCountry();
        boolean countryJustArrived = country != null && !country.isEmpty() && lastCountry.isEmpty();
        if (country != null && !country.isEmpty()) lastCountry = country;
        if (pingValue != null) {
            String value = "Пинг до VDS: " + milliseconds + " мс";
            if (!lastCountry.isEmpty()) value += "  ·  " + lastCountry;
            pingValue.setText(value);
            if (countryJustArrived) {
                pingValue.animate().cancel();
                pingValue.setScaleX(0.92f);
                pingValue.setScaleY(0.92f);
                pingValue.animate().scaleX(1f).scaleY(1f).setDuration(260)
                        .setInterpolator(new OvershootInterpolator(3f)).start();
            }
        }
        if (pingGraph != null) pingGraph.addSample(milliseconds);
    }

    private void setPingPanelVisible(boolean visible) {
        if (pingPanel == null || pingPanelShown == visible) return;
        pingPanelShown = visible;
        pingPanel.animate().cancel();
        if (visible) {
            pingPanel.setVisibility(View.VISIBLE);
            pingPanel.setAlpha(0f);
            pingPanel.setTranslationY(dp(8));
            pingPanel.animate().alpha(1f).translationY(0f).setDuration(450).start();
        } else {
            pingPanel.animate().alpha(0f).translationY(dp(8)).setDuration(220)
                    .withEndAction(() -> {
                        if (!pingPanelShown && pingPanel != null) pingPanel.setVisibility(View.GONE);
                    }).start();
        }
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

    private void appendLog(String value) {
        if (!logs.isEmpty()) logs += "\n";
        logs += value;
        if (logs.length() > 60000) logs = logs.substring(logs.length() - 40000);
        if (logView != null) {
            logView.setText(logs);
            if (autoScroll && logScroll != null) logScroll.post(() -> logScroll.fullScroll(View.FOCUS_DOWN));
        }
    }

    private LinearLayout page() {
        LinearLayout page = new LinearLayout(this);
        page.setOrientation(LinearLayout.VERTICAL);
        return page;
    }

    private TextView text(String value, int size, int color, boolean bold) {
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
