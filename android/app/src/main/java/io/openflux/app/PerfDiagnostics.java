package io.openflux.app;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.os.Debug;
import android.os.Handler;
import android.os.Looper;
import android.os.Process;
import android.os.SystemClock;
import android.widget.ScrollView;
import android.widget.TextView;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import org.json.JSONObject;
import io.openflux.bridge.mobile.Mobile;

/** Local numeric diagnostics only. Never reads saved profiles, credentials or logs. */
final class PerfDiagnostics {
    static AlertDialog show(Activity activity) {
        TextView text = new TextView(activity);
        text.setPadding(24, 16, 24, 16);
        text.setTextIsSelectable(true);
        text.setText("Сбор локальных метрик…");
        ScrollView scroll = new ScrollView(activity);
        scroll.addView(text);
        AlertDialog dialog = new AlertDialog.Builder(activity)
                .setTitle("Диагностика возвратного пути")
                .setView(scroll).setPositiveButton("Закрыть", null)
                .setNeutralButton("Копировать диагностику", null).create();
        Handler ui = new Handler(Looper.getMainLooper());
        ScheduledExecutorService sampler = Executors.newSingleThreadScheduledExecutor();
        AtomicBoolean closed = new AtomicBoolean();
        String[] latest = {""}; // Accessed only on the main thread.
        dialog.setOnDismissListener(ignored -> { closed.set(true); sampler.shutdownNow(); });
        dialog.setOnShowListener(ignored -> dialog.getButton(AlertDialog.BUTTON_NEUTRAL).setOnClickListener(v -> {
            if (!latest[0].isEmpty()) {
                ClipboardManager clipboard = (ClipboardManager) activity.getSystemService(Context.CLIPBOARD_SERVICE);
                clipboard.setPrimaryClip(ClipData.newPlainText("OpenFlux Perf Lab diagnostics", latest[0]));
            }
        }));
        dialog.show();
        sampler.scheduleWithFixedDelay(new Runnable() {
            long lastTime, lastUp, lastDown, lastCPU;
            public void run() {
                if (closed.get()) return;
                try {
                    JSONObject data = new JSONObject(Mobile.performanceSnapshot());
                    JSONObject tun = OpenFluxTunnelService.tunDiagnosticsSnapshot();
                    java.util.Iterator<String> tunKeys = tun.keys();
                    while (tunKeys.hasNext()) {
                        String key = tunKeys.next();
                        data.put(key, tun.get(key));
                    }
                    long now = SystemClock.elapsedRealtime(), cpu = Process.getElapsedCpuTime();
                    long up = data.optLong("upload_bytes"), down = data.optLong("download_bytes");
                    // A reconnect resets packet counters; never report negative rates.
                    if (lastTime != 0 && now > lastTime && up >= lastUp && down >= lastDown) {
                        double seconds = (now - lastTime) / 1000.0;
                        data.put("upload_bytes_per_sec", (up - lastUp) / seconds);
                        data.put("download_bytes_per_sec", (down - lastDown) / seconds);
                        data.put("process_cpu_percent_one_core", 100.0 * (cpu - lastCPU) / (now - lastTime));
                    }
                    lastTime = now; lastUp = up; lastDown = down; lastCPU = cpu;
                    Debug.MemoryInfo memory = new Debug.MemoryInfo();
                    Debug.getMemoryInfo(memory);
                    data.put("android_pss_kib", memory.getTotalPss());
                    data.put("java_heap_used_bytes", Runtime.getRuntime().totalMemory() - Runtime.getRuntime().freeMemory());
                    data.put("native_heap_allocated_bytes", Debug.getNativeHeapAllocatedSize());
                    data.put("process_cpu_time_ms", cpu);
                    data.put("elapsed_realtime_ms", now);
                    data.put("apk_version", BuildConfig.VERSION_NAME);
                    data.put("apk_commit", BuildConfig.PERF_COMMIT);
                    data.put("application_id", BuildConfig.APPLICATION_ID);
                    data.put("measurement_scope", "packet VPN; SOCKS5 profiles not applied; rates sample every 2s while visible");
                    data.put("carrier_status", ReturnPathStatus.describe(data));
                    data.put("acceptance", "TUN classification retest only: Legacy + Baseline. A/B suspended; packet receipt is not proof of Internet access.");
                    String report = data.toString(2);
                    ui.post(() -> { if (!closed.get() && !activity.isDestroyed()) { latest[0] = report; text.setText(report); } });
                } catch (Exception ignored) {
                    ui.post(() -> { if (!closed.get()) text.setText("Метрики временно недоступны"); });
                }
            }
        }, 0, 2, TimeUnit.SECONDS);
        return dialog;
    }
}
