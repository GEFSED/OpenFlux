package io.openflux.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Intent;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.os.SystemClock;

import java.util.Locale;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;

import io.openflux.bridge.mobile.Mobile;

// Runs the local SOCKS5 proxy (Mobile.startProxy/stopProxy) as a plain
// foreground Service, not a VpnService: unlike OpenFluxVpnService, this mode
// never touches Android's VPN subsystem, so it needs no VPN permission
// dialog and doesn't show the system VPN key icon. Other apps must be
// pointed at the local SOCKS5 address manually.
public final class OpenFluxProxyService extends Service {
    public static final String ACTION_START = "io.openflux.app.PROXY_START";
    public static final String ACTION_STOP = "io.openflux.app.PROXY_STOP";
    public static final String EXTRA_DOCUMENT_URL = "document_url";
    public static final String EXTRA_ENCRYPTION_SECRET = "encryption_secret";
    public static final String EXTRA_TRANSPORT_TYPE = "transport_type";
    public static final String EXTRA_PORT = "port";
    public static final String EXTRA_LAN_ACCESS = "lan_access";
    public static final String EXTRA_USERNAME = "username";
    public static final String EXTRA_PASSWORD = "password";

    private static final String CHANNEL_ID = "openflux_proxy";
    private static final int NOTIFICATION_ID = 8;
    private static volatile boolean running;
    private static volatile String status = "Остановлено";
    private static volatile String lastError = "";
    private static volatile int activePort;
    // Lives on the service, not the Activity: MainActivity can be destroyed
    // and recreated (low memory, long time away) while this foreground
    // service keeps running, and the uptime shown on Home must survive that.
    private static volatile long connectedAtMillis;

    private final ExecutorService workers = Executors.newSingleThreadExecutor();
    private final AtomicInteger generation = new AtomicInteger();

    private final Handler notificationHandler = new Handler(Looper.getMainLooper());
    private long lastSampledSent;
    private long lastSampledReceived;
    private long lastSampledAt;
    private final Runnable speedUpdater = new Runnable() {
        @Override public void run() {
            long now = SystemClock.elapsedRealtime();
            long elapsedMs = Math.max(1, now - lastSampledAt);
            long sent = Mobile.proxyBytesSent();
            long received = Mobile.proxyBytesReceived();
            long sentPerSec = (sent - lastSampledSent) * 1000 / elapsedMs;
            long receivedPerSec = (received - lastSampledReceived) * 1000 / elapsedMs;
            lastSampledSent = sent;
            lastSampledReceived = received;
            lastSampledAt = now;
            updateNotification("↑ " + formatSpeed(sentPerSec) + "   ↓ " + formatSpeed(receivedPerSec));
            notificationHandler.postDelayed(this, 1000);
        }
    };

    // healthChecker keeps "Подключено" honest for the same reason as in
    // OpenFluxVpnService: once set at initial connect, status would never
    // reflect a later drop in the underlying transport without this.
    private final Runnable healthChecker = new Runnable() {
        @Override public void run() {
            if (running) {
                boolean connected = Mobile.proxyIsConnected();
                if (!connected && "Подключено".equals(status)) {
                    status = "Подключение…";
                    lastError = "Транспорт отключился, переподключение…";
                } else if (connected && "Подключение…".equals(status)) {
                    status = "Подключено";
                    lastError = "Транспорт восстановлен";
                }
            }
            notificationHandler.postDelayed(this, 2000);
        }
    };

    public static boolean isRunning() { return running; }
    public static String getStatus() { return status; }
    public static String getLastError() { return lastError; }
    public static int getActivePort() { return activePort; }
    public static long getConnectedAtMillis() { return connectedAtMillis; }

    private static String formatSpeed(long bytesPerSecond) {
        if (bytesPerSecond < 1024) return bytesPerSecond + " Б/с";
        if (bytesPerSecond < 1024 * 1024) return String.format(Locale.US, "%.0f КБ/с", bytesPerSecond / 1024.0);
        return String.format(Locale.US, "%.1f МБ/с", bytesPerSecond / (1024.0 * 1024.0));
    }

    private void startSpeedUpdates() {
        lastSampledSent = 0;
        lastSampledReceived = 0;
        lastSampledAt = SystemClock.elapsedRealtime();
        notificationHandler.removeCallbacks(speedUpdater);
        notificationHandler.post(speedUpdater);
        notificationHandler.removeCallbacks(healthChecker);
        notificationHandler.postDelayed(healthChecker, 2000);
    }

    private void stopSpeedUpdates() {
        notificationHandler.removeCallbacks(speedUpdater);
        notificationHandler.removeCallbacks(healthChecker);
    }

    @Override public IBinder onBind(Intent intent) { return null; }

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && ACTION_STOP.equals(intent.getAction())) {
            stopProxy();
            return START_NOT_STICKY;
        }
        if (running) return START_STICKY;

        // A foreground service started via startForegroundService() must call
        // startForeground() right away - any early stopSelf() before that
        // (e.g. on a validation error below) would otherwise crash the app
        // with ForegroundServiceDidNotStartInTimeException.
        createNotificationChannel();
        startForeground(NOTIFICATION_ID, notification("Подключение…"));

        String url = intent == null ? null : intent.getStringExtra(EXTRA_DOCUMENT_URL);
        if (url == null || !url.startsWith("https://")) {
            lastError = "Некорректная ссылка на документ";
            status = "Ошибка";
            stopSelf();
            return START_NOT_STICKY;
        }
        String encryptionSecretExtra = intent.getStringExtra(EXTRA_ENCRYPTION_SECRET);
        final String encryptionSecret = encryptionSecretExtra == null ? "" : encryptionSecretExtra;
        if (!encryptionSecret.isEmpty() && encryptionSecret.length() < 16) {
            lastError = "Ключ шифрования должен быть не короче 16 символов, либо оставьте поле пустым";
            status = "Ошибка";
            stopSelf();
            return START_NOT_STICKY;
        }
        String transportTypeExtra = intent.getStringExtra(EXTRA_TRANSPORT_TYPE);
        final String transportType = transportTypeExtra == null || transportTypeExtra.isEmpty()
                ? "yandex" : transportTypeExtra;
        int port = intent.getIntExtra(EXTRA_PORT, 1080);
        boolean lanAccess = intent.getBooleanExtra(EXTRA_LAN_ACCESS, false);
        String username = intent.getStringExtra(EXTRA_USERNAME);
        String password = intent.getStringExtra(EXTRA_PASSWORD);
        if (username == null) username = "";
        if (password == null) password = "";
        String bindHost = lanAccess ? "0.0.0.0" : "127.0.0.1";

        running = true;
        status = "Подключение…";
        lastError = "";
        activePort = port;
        int session = generation.incrementAndGet();
        String selectedUser = username;
        String selectedPassword = password;
        workers.execute(() -> startProxyTransport(transportType, url, encryptionSecret, bindHost, port,
                selectedUser, selectedPassword, session));
        return START_STICKY;
    }

    private void startProxyTransport(String transportType, String url, String encryptionSecret, String bindHost, int port,
            String username, String password, int session) {
        if (!isCurrent(session)) return;
        String error = Mobile.startProxy(transportType, url, encryptionSecret, bindHost + ":" + port, username, password);
        if (error != null && !error.isEmpty()) {
            fail(session, error);
            return;
        }

        for (int attempt = 0; isCurrent(session) && !Mobile.proxyIsConnected() && attempt < 120; attempt++) {
            try { Thread.sleep(250); }
            catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
                return;
            }
        }
        if (!isCurrent(session)) return;
        if (!Mobile.proxyIsConnected()) {
            fail(session, "Yandex-транспорт не подключился за 30 секунд");
            return;
        }

        status = "Подключено";
        if (connectedAtMillis == 0L) connectedAtMillis = System.currentTimeMillis();
        startSpeedUpdates();
    }

    private boolean isCurrent(int session) {
        return running && generation.get() == session;
    }

    private synchronized void fail(int session, String message) {
        if (generation.get() != session) return;
        lastError = message == null ? "Неизвестная ошибка" : message;
        status = "Ошибка";
        connectedAtMillis = 0L;
        generation.incrementAndGet();
        stopSpeedUpdates();
        Mobile.stopProxy();
        running = false;
        stopForeground(STOP_FOREGROUND_REMOVE);
        stopSelf();
    }

    private synchronized void stopProxy() {
        status = "Останавливается…";
        generation.incrementAndGet();
        stopSpeedUpdates();
        Mobile.stopProxy();
        running = false;
        status = "Остановлено";
        lastError = "";
        connectedAtMillis = 0L;
        stopForeground(STOP_FOREGROUND_REMOVE);
        stopSelf();
    }

    @Override public void onDestroy() {
        generation.incrementAndGet();
        stopSpeedUpdates();
        Mobile.stopProxy();
        running = false;
        if (!"Ошибка".equals(status)) status = "Остановлено";
        connectedAtMillis = 0L;
        workers.shutdownNow();
        super.onDestroy();
    }

    private void createNotificationChannel() {
        NotificationManager manager = getSystemService(NotificationManager.class);
        manager.createNotificationChannel(new NotificationChannel(
                CHANNEL_ID, "OpenFlux Прокси", NotificationManager.IMPORTANCE_LOW));
    }

    private Notification notification(String text) {
        Intent open = new Intent(this, MainActivity.class);
        PendingIntent content = PendingIntent.getActivity(
                this, 0, open, PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        Intent stop = new Intent(this, OpenFluxProxyService.class).setAction(ACTION_STOP);
        PendingIntent stopIntent = PendingIntent.getService(
                this, 0, stop, PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        return new Notification.Builder(this, CHANNEL_ID)
                .setContentTitle("OpenFlux")
                .setContentText(text)
                .setSmallIcon(R.drawable.ic_openflux_notification)
                .setOngoing(true)
                .setContentIntent(content)
                .addAction(R.drawable.ic_power, "Отключить", stopIntent)
                .build();
    }

    private void updateNotification(String text) {
        getSystemService(NotificationManager.class).notify(NOTIFICATION_ID, notification(text));
    }
}
