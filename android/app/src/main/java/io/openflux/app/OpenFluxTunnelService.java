package io.openflux.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.net.VpnService;
import android.os.Handler;
import android.os.Looper;
import android.os.ParcelFileDescriptor;
import android.os.SystemClock;

import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.SocketTimeoutException;
import java.util.Arrays;
import java.util.Collections;
import java.util.Locale;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

import io.openflux.bridge.mobile.Mobile;

public final class OpenFluxTunnelService extends VpnService {
    public static final String ACTION_START = "io.openflux.app.START";
    public static final String ACTION_STOP = "io.openflux.app.STOP";
    public static final String EXTRA_DOCUMENT_URL = "document_url";
    public static final String EXTRA_ENCRYPTION_SECRET = "encryption_secret";
    public static final String EXTRA_TRANSPORT_TYPE = "transport_type";
    public static final String EXTRA_DNS_SERVER = "dns_server";
    public static final String EXTRA_MTU = "mtu";
    public static final String EXTRA_CODEC = "codec";
    public static final String EXTRA_MAX_TOKEN = "max_token";
    public static final String EXTRA_MAX_UID = "max_uid";

    private static final String CHANNEL_ID = "openflux_tunnel";
    private static final int NOTIFICATION_ID = 7;
    private static volatile boolean running;
    private static volatile String status = "Остановлено";
    private static volatile String lastError = "";
    // Lives on the service, not the Activity: MainActivity can be destroyed
    // and recreated (low memory, long time away) while this foreground
    // service keeps running, and the uptime shown on Home must survive that.
    private static volatile long connectedAtMillis;

    private final ExecutorService workers = Executors.newCachedThreadPool();
    private final Object outputLock = new Object();
    private final AtomicInteger generation = new AtomicInteger();
    private volatile boolean active;
    private ParcelFileDescriptor tunnel;
    private FileInputStream tunnelInput;
    private FileOutputStream tunnelOutput;

    private final AtomicLong bytesSent = new AtomicLong();
    private final AtomicLong bytesReceived = new AtomicLong();
    private final Handler notificationHandler = new Handler(Looper.getMainLooper());
    private long lastSampledSent;
    private long lastSampledReceived;
    private long lastSampledAt;
    private final Runnable speedUpdater = new Runnable() {
        @Override public void run() {
            long now = SystemClock.elapsedRealtime();
            long elapsedMs = Math.max(1, now - lastSampledAt);
            long sent = bytesSent.get();
            long received = bytesReceived.get();
            long sentPerSec = (sent - lastSampledSent) * 1000 / elapsedMs;
            long receivedPerSec = (received - lastSampledReceived) * 1000 / elapsedMs;
            lastSampledSent = sent;
            lastSampledReceived = received;
            lastSampledAt = now;
            updateNotification("↑ " + formatSpeed(sentPerSec) + "   ↓ " + formatSpeed(receivedPerSec));
            notificationHandler.postDelayed(this, 1000);
        }
    };

    // healthChecker keeps "Подключено" honest: without it, status is set
    // once on initial connect and never revisited, so if the underlying
    // Yandex Docs transport drops and silently retries (it keeps retrying
    // on its own - see transport.DefaultConfig's MaxReconnectAttempts) the
    // UI would keep showing a green "connected" while every packet is
    // actually being dropped (Mobile.send fails silently, so nothing leaks
    // unencrypted - the TUN just stops passing data). This surfaces that
    // state honestly instead of just failing silently.
    private final Runnable healthChecker = new Runnable() {
        @Override public void run() {
            if (running) {
                boolean connected = Mobile.isConnected();
                if (!connected && "Подключено".equals(status)) {
                    status = "Подключение…";
                    lastError = "Транспорт отключился, переподключение…";
                } else if (connected && "Подключение…".equals(status) && active) {
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
    public static long getConnectedAtMillis() { return connectedAtMillis; }

    private static String formatSpeed(long bytesPerSecond) {
        if (bytesPerSecond < 1024) return bytesPerSecond + " Б/с";
        if (bytesPerSecond < 1024 * 1024) return String.format(Locale.US, "%.0f КБ/с", bytesPerSecond / 1024.0);
        return String.format(Locale.US, "%.1f МБ/с", bytesPerSecond / (1024.0 * 1024.0));
    }

    private void startSpeedUpdates() {
        bytesSent.set(0);
        bytesReceived.set(0);
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

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && ACTION_STOP.equals(intent.getAction())) {
            stopTunnel();
            return START_NOT_STICKY;
        }
        if (running) return START_STICKY;

        // A foreground service started via startForegroundService() must call
        // startForeground() right away - any early stopSelf() before that
        // (e.g. on a validation error below) would otherwise crash the app
        // with ForegroundServiceDidNotStartInTimeException.
        createNotificationChannel();
        startForeground(NOTIFICATION_ID, notification("Подключение…"));

        String transportTypeExtra = intent == null ? null : intent.getStringExtra(EXTRA_TRANSPORT_TYPE);
        final String transportType = transportTypeExtra == null || transportTypeExtra.isEmpty()
                ? "yandex" : transportTypeExtra;
        boolean needsUrl = !"oneme".equals(transportType);

        String url = intent == null ? null : intent.getStringExtra(EXTRA_DOCUMENT_URL);
        if (needsUrl && (url == null || !url.startsWith("https://"))) {
            lastError = "Некорректная ссылка на документ";
            status = "Ошибка";
            running = false;
            stopSelf();
            return START_NOT_STICKY;
        }
        if (url == null) url = "";
        String dnsServer = intent.getStringExtra(EXTRA_DNS_SERVER);
        String encryptionSecretExtra = intent.getStringExtra(EXTRA_ENCRYPTION_SECRET);
        final String encryptionSecret = encryptionSecretExtra == null ? "" : encryptionSecretExtra;
        if (!encryptionSecret.isEmpty() && encryptionSecret.length() < 16) {
            lastError = "Ключ шифрования должен быть не короче 16 символов, либо оставьте поле пустым";
            status = "Ошибка";
            running = false;
            stopSelf();
            return START_NOT_STICKY;
        }
        String codecExtra = intent.getStringExtra(EXTRA_CODEC);
        final String codec = codecExtra == null || codecExtra.isEmpty() ? "batched" : codecExtra;
        String maxTokenExtra = intent.getStringExtra(EXTRA_MAX_TOKEN);
        final String maxToken = maxTokenExtra == null ? "" : maxTokenExtra;
        String maxUidExtra = intent.getStringExtra(EXTRA_MAX_UID);
        final String maxUid = maxUidExtra == null ? "" : maxUidExtra;
        if (dnsServer == null || dnsServer.trim().isEmpty()) dnsServer = "1.1.1.1";
        int mtu = Math.max(576, Math.min(1500, intent.getIntExtra(EXTRA_MTU, 1400)));

        active = true;
        running = true;
        status = "Подключение…";
        lastError = "";
        int session = generation.incrementAndGet();
        String selectedDns = dnsServer;
        int selectedMtu = mtu;
        String finalUrl = url;
        workers.execute(() -> startTunnel(transportType, finalUrl, encryptionSecret, codec, maxToken, maxUid, selectedDns, selectedMtu, session));
        return START_STICKY;
    }

    private void startTunnel(String transportType, String url, String encryptionSecret, String codec, String maxToken, String maxUid, String dnsServer, int mtu, int session) {
        if (!isCurrent(session)) return;
        String error = Mobile.start(transportType, url, encryptionSecret, codec, maxToken, maxUid);
        if (error != null && !error.isEmpty()) {
            fail(session, error);
            return;
        }

        for (int attempt = 0; isCurrent(session) && !Mobile.isConnected() && attempt < 120; attempt++) {
            try { Thread.sleep(250); }
            catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
                return;
            }
        }
        if (!isCurrent(session)) return;
        if (!Mobile.isConnected()) {
            fail(session, "Yandex-транспорт не подключился за 30 секунд");
            return;
        }

        try {
            // Builder.addDnsServer() only accepts a numeric IP - it throws
            // IllegalArgumentException on a hostname like "dns.google", even
            // though queryLocalDns() below resolves hostnames just fine. This
            // is running on a background worker thread, so a blocking lookup
            // here is fine.
            String dnsServerIp = InetAddress.getByName(dnsServer).getHostAddress();
            Builder builder = new Builder()
                    .setSession("OpenFlux")
                    .setMtu(mtu)
                    .addAddress("10.10.10.2", 24)
                    .addRoute("0.0.0.0", 0)
                    .addDnsServer(dnsServerIp);
            applyAppFilter(builder);
            ParcelFileDescriptor established = builder.establish();
            if (established == null) throw new IOException("Android не создал TUN-интерфейс");
            synchronized (outputLock) {
                if (!isCurrent(session)) {
                    established.close();
                    return;
                }
                tunnel = established;
                tunnelInput = new FileInputStream(established.getFileDescriptor());
                tunnelOutput = new FileOutputStream(established.getFileDescriptor());
            }
        } catch (IOException | IllegalArgumentException exception) {
            fail(session, exception.getMessage());
            return;
        }

        if (!isCurrent(session)) return;
        status = "Подключено";
        if (connectedAtMillis == 0L) connectedAtMillis = System.currentTimeMillis();
        startSpeedUpdates();
        FileInputStream input = tunnelInput;
        FileOutputStream output = tunnelOutput;
        workers.execute(() -> readOutgoingPackets(session, input, dnsServer));
        workers.execute(() -> writeIncomingPackets(session, output));
    }

    // applyAppFilter routes traffic per the user's "Приложения" settings tab:
    // whitelist mode tunnels only the selected apps, blacklist mode tunnels
    // everything except the selected apps, and "off" tunnels everything.
    // Android forbids calling both addAllowedApplication and
    // addDisallowedApplication on the same Builder, so the two modes are
    // mutually exclusive branches below. Our own package must never enter
    // the tunnel: Go opens the Yandex connection inside this process, so
    // routing our own traffic through TUN would loop it back on itself.
    private void applyAppFilter(Builder builder) {
        SharedPreferences prefs = getSharedPreferences(AppFilter.PREFS_NAME, MODE_PRIVATE);
        String mode = prefs.getString(AppFilter.KEY_MODE, AppFilter.MODE_OFF);
        Set<String> packages = prefs.getStringSet(AppFilter.KEY_PACKAGES, Collections.emptySet());

        if (AppFilter.MODE_WHITELIST.equals(mode) && !packages.isEmpty()) {
            for (String packageName : packages) {
                if (packageName.equals(getPackageName())) continue;
                try {
                    builder.addAllowedApplication(packageName);
                } catch (PackageManager.NameNotFoundException ignored) {
                    // App was uninstalled since it was selected; skip it.
                }
            }
            return;
        }

        try {
            builder.addDisallowedApplication(getPackageName());
        } catch (PackageManager.NameNotFoundException neverThrown) {
            // We are this package; it always exists.
            throw new AssertionError(neverThrown);
        }
        if (AppFilter.MODE_BLACKLIST.equals(mode)) {
            for (String packageName : packages) {
                if (packageName.equals(getPackageName())) continue;
                try {
                    builder.addDisallowedApplication(packageName);
                } catch (PackageManager.NameNotFoundException ignored) {
                    // App was uninstalled since it was selected; skip it.
                }
            }
        }
    }

    private void readOutgoingPackets(int session, FileInputStream input, String dnsServer) {
        byte[] buffer = new byte[32767];
        try {
            while (isCurrent(session)) {
                int length = input.read(buffer);
                if (length <= 0) continue;
                byte[] packet = Arrays.copyOf(buffer, length);
                if (isIpv4UdpDns(packet)) {
                    workers.execute(() -> forwardDns(session, outputFor(session), packet, dnsServer));
                } else if (isIpv4Tcp(packet)) {
                    String error = Mobile.send(packet);
                    if (error != null && !error.isEmpty() && isCurrent(session)) {
                        lastError = "Отправка пакета: " + error;
                    } else {
                        bytesSent.addAndGet(packet.length);
                    }
                }
            }
        } catch (IOException exception) {
            if (isCurrent(session)) fail(session, "Чтение TUN: " + exception.getMessage());
        }
    }

    private void writeIncomingPackets(int session, FileOutputStream output) {
        try {
            while (isCurrent(session)) {
                byte[] packet = Mobile.read();
                if (packet == null || packet.length == 0) {
                    Thread.sleep(2);
                    continue;
                }
                inject(session, output, packet);
                bytesReceived.addAndGet(packet.length);
            }
        } catch (IOException exception) {
            if (isCurrent(session)) fail(session, "Запись TUN: " + exception.getMessage());
        } catch (InterruptedException interrupted) {
            Thread.currentThread().interrupt();
        }
    }

    // forwardDns answers the captured query by relaying it to dnsServer over
    // plain UDP directly from this device.
    private void forwardDns(int session, FileOutputStream output, byte[] request, String dnsServer) {
        int ipHeader = (request[0] & 0x0f) * 4;
        int dnsOffset = ipHeader + 8;
        int udpLength = unsignedShort(request, ipHeader + 4);
        if (dnsOffset > request.length || udpLength < 8 || ipHeader + udpLength > request.length) return;

        byte[] query = Arrays.copyOfRange(request, dnsOffset, ipHeader + udpLength);
        try {
            byte[] answer = queryLocalDns(query, dnsServer);
            if (answer == null || answer.length == 0) {
                if (isCurrent(session)) lastError = "DNS: сервер не ответил";
                return;
            }
            inject(session, output, buildDnsResponse(request, answer));
        } catch (IOException exception) {
            if (isCurrent(session)) lastError = "DNS: " + exception.getMessage();
        }
    }

    // queryLocalDns relays the raw DNS message to dnsServer over plain UDP.
    private byte[] queryLocalDns(byte[] query, String dnsServer) throws IOException {
        try (DatagramSocket socket = new DatagramSocket()) {
            socket.setSoTimeout(5000);
            InetAddress address = InetAddress.getByName(dnsServer);
            socket.send(new DatagramPacket(query, query.length, address, 53));
            byte[] buffer = new byte[4096];
            DatagramPacket response = new DatagramPacket(buffer, buffer.length);
            socket.receive(response);
            return Arrays.copyOf(buffer, response.getLength());
        } catch (SocketTimeoutException timeout) {
            return null;
        }
    }

    private void inject(int session, FileOutputStream output, byte[] packet) throws IOException {
        if (!isCurrent(session) || output == null || packet == null) return;
        synchronized (outputLock) {
            if (isCurrent(session)) output.write(packet);
        }
    }

    private FileOutputStream outputFor(int session) {
        return isCurrent(session) ? tunnelOutput : null;
    }

    private boolean isCurrent(int session) {
        return active && generation.get() == session;
    }

    private static boolean isIpv4Tcp(byte[] packet) {
        return packet.length >= 20 && (packet[0] >>> 4) == 4 && (packet[9] & 0xff) == 6;
    }

    private static boolean isIpv4UdpDns(byte[] packet) {
        if (packet.length < 28 || (packet[0] >>> 4) != 4 || (packet[9] & 0xff) != 17) return false;
        int header = (packet[0] & 0x0f) * 4;
        return header >= 20 && packet.length >= header + 8 && unsignedShort(packet, header + 2) == 53;
    }

    private static byte[] buildDnsResponse(byte[] request, byte[] dns) {
        int requestHeader = (request[0] & 0x0f) * 4;
        byte[] response = new byte[20 + 8 + dns.length];
        response[0] = 0x45;
        response[1] = request[1];
        putShort(response, 2, response.length);
        response[4] = request[4];
        response[5] = request[5];
        response[8] = 64;
        response[9] = 17;
        System.arraycopy(request, 16, response, 12, 4);
        System.arraycopy(request, 12, response, 16, 4);
        putShort(response, 10, checksum(response, 0, 20));

        putShort(response, 20, 53);
        putShort(response, 22, unsignedShort(request, requestHeader));
        putShort(response, 24, 8 + dns.length);
        // A zero UDP checksum is valid for IPv4.
        putShort(response, 26, 0);
        System.arraycopy(dns, 0, response, 28, dns.length);
        return response;
    }

    private static int checksum(byte[] bytes, int offset, int length) {
        long sum = 0;
        for (int i = offset; i < offset + length; i += 2) {
            int high = bytes[i] & 0xff;
            int low = i + 1 < offset + length ? bytes[i + 1] & 0xff : 0;
            sum += (high << 8) | low;
            while ((sum & 0xffff0000L) != 0) sum = (sum & 0xffffL) + (sum >>> 16);
        }
        return (int) (~sum) & 0xffff;
    }

    private static int unsignedShort(byte[] bytes, int offset) {
        return ((bytes[offset] & 0xff) << 8) | (bytes[offset + 1] & 0xff);
    }

    private static void putShort(byte[] bytes, int offset, int value) {
        bytes[offset] = (byte) (value >>> 8);
        bytes[offset + 1] = (byte) value;
    }

    private synchronized void fail(int session, String message) {
        if (!isCurrent(session)) return;
        lastError = message == null ? "Неизвестная ошибка" : message;
        status = "Ошибка";
        connectedAtMillis = 0L;
        generation.incrementAndGet();
        active = false;
        stopSpeedUpdates();
        closeTunnel();
        Mobile.stop();
        running = false;
        stopForeground(STOP_FOREGROUND_REMOVE);
        stopSelf();
    }

    private synchronized void stopTunnel() {
        status = "Останавливается…";
        generation.incrementAndGet();
        active = false;
        stopSpeedUpdates();
        closeTunnel();
        Mobile.stop();
        running = false;
        status = "Остановлено";
        lastError = "";
        connectedAtMillis = 0L;
        stopForeground(STOP_FOREGROUND_REMOVE);
        stopSelf();
    }

    @Override public void onDestroy() {
        generation.incrementAndGet();
        active = false;
        stopSpeedUpdates();
        closeTunnel();
        Mobile.stop();
        running = false;
        if (!"Ошибка".equals(status)) status = "Остановлено";
        connectedAtMillis = 0L;
        workers.shutdownNow();
        super.onDestroy();
    }

    private void closeTunnel() {
        synchronized (outputLock) {
            if (tunnel != null) {
                try { tunnel.close(); } catch (IOException ignored) { }
                tunnel = null;
            }
            tunnelInput = null;
            tunnelOutput = null;
        }
    }

    private void createNotificationChannel() {
        NotificationManager manager = getSystemService(NotificationManager.class);
        manager.createNotificationChannel(new NotificationChannel(
                CHANNEL_ID, "OpenFlux Tunnel", NotificationManager.IMPORTANCE_LOW));
    }

    private Notification notification(String text) {
        Intent open = new Intent(this, MainActivity.class);
        PendingIntent content = PendingIntent.getActivity(
                this, 0, open, PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        Intent stop = new Intent(this, OpenFluxTunnelService.class).setAction(ACTION_STOP);
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
