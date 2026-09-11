# Changelog

All notable changes to this fork are documented here.

## Unreleased

### Added

- synced with upstream p1neappleXpress/OpenFlux through commit `3249724`:
  fixed swapped `maxToken`/`maxUid` flag descriptions, `yandex` transport now
  returns errors instead of panicking on unexpected document config (with new
  tests), and an experimental Yandex.Docs Volga transport (`vyandex.go`) is
  now in the tree.
- synced with upstream through commit `9ef5ab3`: exponential reconnect
  backoff and bounded WebSocket dial for the `yandex` transport; SOCKS5
  `Bind`/`Close` lifecycle plus a bounds-check fix for malformed domain
  requests; the MAX transport no longer kills the whole process
  (`os.Exit(1)`) when its connection drops — fatal when embedded as an
  Android library; panic recovery (`utils.SafeGo`) around background
  goroutines in the transport, tunnel and SOCKS5 layers; a `--local-ip` flag
  to scope the exit node's RST-drop iptables rule to a dedicated egress IP
  instead of dropping RSTs host-wide; aggressive GC on the exit node for
  small VPS instances.

### Notes

- the new `vyandex` transport is intentionally not wired into the
  `--transport` CLI switch: it has no mandatory encryption wrapper yet, so
  exposing it would contradict this fork's encrypted-by-default security
  model for Yandex Docs transports.
- upstream re-added a full iOS app (`ios-app/`, `export_ios*.go`,
  `tunnel/packettunnel.go`, `build_ios*.sh`) and several Network Extension
  hardening commits on top of it; none of that was carried over. This fork
  does not support iOS (see [FORK.md](FORK.md)).

## 0.3.0 - 2026-09-10

### Added

- Android APKs for `arm64-v8a`, `armeabi-v7a`, `x86_64`, `x86` and universal
  devices;
- automated GitHub Release publishing for tags matching `v*`;
- SHA-256 checksums for every release APK.

## 0.2.0 - 2026-09-10

### Added

- native Android `VpnService` client with connection, logs and settings tabs;
- Android Keystore-backed protection for the saved document URL and secret;
- mandatory AES-256-GCM encryption for the Yandex document transport;
- encrypted ping frames and an animated latency graph;
- DNS-over-HTTPS support for the Android VPN;
- a hardened sample systemd service for the Linux exit node;
- CI checks for Go and Android debug builds.

### Changed

- secret values can be loaded from root-only files instead of command-line
  arguments;
- Android application version is now 0.2.0 (version code 2).

### Removed

- the incomplete iOS prototype and generated IDE/build artifacts.
