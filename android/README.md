# OpenFlux for Android

The Android app uses the system `VpnService` API and provides connection,
logging and settings tabs. Its document URL and shared encryption secret are
entered by the user; no server address, document URL or secret is embedded in
the source code or APK.

## Runtime behavior

- Android 8 (API 26) or newer, `arm64-v8a` only;
- IPv4/TCP traffic is forwarded through OpenFlux and the exit node;
- DNS uses DNS-over-HTTPS with selectable Cloudflare, Google or Quad9 service;
- the foreground service keeps the tunnel alive while the screen is off;
- the URL and secret are encrypted using an Android Keystore-backed key;
- settings remain after an in-place update signed by the same certificate;
- clearing app data or uninstalling the app removes the saved settings.

The current app does not tunnel arbitrary UDP or IPv6. It is experimental and
has not received an independent security audit.

## Build

Install Java 17, Android SDK/API 35, Build Tools 35.0.0,
NDK 27.0.12077973, Gradle 8.14.3, `gomobile` and `gobind`. Set
`ANDROID_SDK_ROOT` or `ANDROID_HOME`, then run from the repository root:

```bash
./build_android_app.sh
```

The script generates `android/app/libs/openflux.aar` and writes the debug APK to
`dist/OpenFlux-android-arm64-debug.apk`. Both files are ignored by Git.

Debug builds are suitable for direct testing only. Keep a production signing
key outside this repository and back it up securely before distributing a
release. Android accepts an in-place update only when the package name and
signing certificate match the installed application.
