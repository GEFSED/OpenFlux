# Changelog

All notable changes to this fork are documented here.

## 0.5.1 - 2026-09-12

### Changed

- `main` is realigned to be wire-compatible with the current upstream
  `p1neappleXpress/OpenFlux` exit-node and client binaries again. See
  [docs/UPSTREAM_DIFF.md](UPSTREAM_DIFF.md) for the full comparison that led
  to this.

The previous `EncryptedTransport` added a 1-byte frame-type tag ahead of
every packet to multiplex ping and DNS-relay traffic into the same
encrypted channel. Upstream's transport (including the version merged from
this fork's own earlier PR) has no such tag, so a client on one side could
not talk to an exit node on the other without every packet's IP header
getting corrupted by the stray byte - not just the newer features, any data
at all. That divergence dates back to when ping support was first added, not
just this week's DNS-relay work.

To fix this, `main` drops everything that depended on the frame protocol:

- **ping graph and exit-node country display** (Home screen) - relied on
  `framePingRequest`/`framePingResponse`;
- **"resolve DNS on the server" option** - relied on the new
  `frameDNSRequest`/`frameDNSResponse` frames added this week. DNS
  resolution on Android is local-only again, exactly as it was before this
  week's tunnel-relay work, for both VPN and Proxy mode.

Everything else added since 0.4.0 is untouched and stays fully functional
with an unmodified upstream binary, since none of it touches the client<->
exit-node wire protocol: Proxy (SOCKS5) mode, local-network access and SOCKS5
authentication with a share link and QR code, the vertical Settings
navigation and About section, the GitHub update-check badge, live
upload/download speed and a Disconnect action in the notification, honest
connection-health tracking, the battery-optimization and Always-on VPN
shortcuts, the fuller active-parameters list, the `POST_NOTIFICATIONS`
request, and the animation-duplication fix.

The full previous feature set (frame protocol, ping graph, country display,
server-side DNS relay) is preserved on the `experimental` branch for anyone
running both ends with this fork's own exit-node binary.

- Android application version is now 0.5.1 (version code 6).

## 0.5.0 - 2026-09-12

### Added

- a second connection mode for Android: **Proxy (SOCKS5)**, alongside the
  existing system-wide VPN mode. It reuses the same encrypted document
  transport and the same userspace TCP/IP stack (gVisor) the desktop CLI's
  SOCKS5 client already used, wired through `gomobile` - no protocol or
  exit-node changes were needed for this mode by itself. Unlike VPN mode it
  needs no VPN permission prompt and runs as a plain foreground service.
- **DNS resolution via the exit node**: both VPN and Proxy mode can relay DNS
  queries through the encrypted tunnel to the exit node, which asks the
  configured upstream over plain UDP from its own network - the query never
  touches the client's local network. New symmetric `frameDNSRequest`/
  `frameDNSResponse` frames on the existing encrypted channel. A
  per-connection setting lets you choose server-side or local resolution
  instead (useful with an exit node that hasn't been updated yet, or when
  local resolution is simply faster).
- the DNS-server field now accepts a hostname as well as an IP (e.g.
  `dns.google`, not just `1.1.1.1`) for both resolution modes.
- **local-network access and authentication for Proxy mode**: an opt-in
  toggle exposes the SOCKS5 listener on `0.0.0.0` instead of loopback only,
  so another device on the same Wi-Fi/LAN can use it, plus optional SOCKS5
  username/password authentication (RFC 1929, constant-time credential
  check) with a one-tap credential generator. A `socks://` share link and QR
  code are shown for pairing another device.
- the SOCKS5 server now replies with a proper protocol-level error (RFC 1928
  address-not-supported / command-not-supported) instead of silently closing
  the connection on an IPv6 address or a non-CONNECT command, so a client
  fails fast instead of hanging. Full IPv6/UDP-ASSOCIATE delivery is not yet
  implemented - the exit node's raw-socket internet path is currently
  IPv4/TCP-only.
- Settings navigation was redone as a vertical, phone-Settings-style list
  (icon + label, tap to drill in, back via the top-left arrow or the system
  back button/gesture) instead of a horizontal tab strip, with a new
  **About** section linking to the upstream repository and this fork.
- the app-version badge (previously a static "BETA" pill) now shows the
  installed version and checks GitHub for a newer release at startup,
  turning green ("up to date") or amber ("update available").
- the pinned connection notification now shows live upload/download speed
  (updated every second) with a **Disconnect** action button, for both
  modes.
- connection status now tracks the transport honestly after the initial
  handshake: if the encrypted transport drops and silently retries mid
  session, the UI switches back to "Connecting..." instead of continuing to
  show a stale "Connected", and back again once it recovers.
- shortcuts to two OS-level reliability settings: a battery-optimization
  exemption prompt (foreground services survive OEM battery managers much
  better with it granted), and a link to Android's Always-on VPN /
  "Block connections without VPN" settings screen for VPN mode.
- the Home screen's active-parameters card now lists every relevant setting
  for the current mode (mode, DNS server and resolution location, MTU or
  local port, app filter, LAN/auth status), not just two of them.
- runtime `POST_NOTIFICATIONS` permission is now requested on Android 13+;
  without it the app worked fine but silently showed no notification at all.
- animated UI buttons no longer stack/duplicate their scale animation when
  tapped rapidly or repeatedly (a shared `bounce()` helper cancels and
  replaces any in-flight animation on the same view instead of layering a
  new one on top).

### Changed

- Android application version is now 0.5.0 (version code 5).
- the keyboard no longer resizes the whole window (shrinking the bottom
  navigation and the Save button up into view); it now pans instead, so
  those stay in place and are simply covered while a field is focused.
- the per-app VPN filter's hint now notes it only applies to VPN mode, since
  Proxy mode has no system-wide capture to filter.

## 0.4.0 - 2026-09-12

### Added

- Android Settings is now organized into tabs (Транспорт / Сеть /
  Приложения / Вид) instead of one long scrolling page.
- per-app VPN routing: a whitelist ("only these apps use the tunnel") or
  blacklist ("all apps except these") mode, with an in-app picker over
  installed apps. Backed by `VpnService.Builder.addAllowedApplication`/
  `addDisallowedApplication`.
- the exit node's country now shows up next to the ping once it arrives,
  piggybacked on the existing encrypted ping/pong frames (no new protocol
  message); the exit node determines it once at startup via `ip-api.com`
  and publishes it through `EncryptedTransport.SetCountry`.
- animations throughout the Android UI: crossfaded tab/page transitions,
  a staggered card entrance on Home, a pulsing status dot while
  connecting/disconnecting, an animated VPN-button color transition, and
  small pop/bounce feedback on tab and toggle interactions.
- haptic feedback: light ticks on button/toggle/tab interactions via
  `View.performHapticFeedback`, plus distinct vibration patterns for a
  successful connection and for errors via `Vibrator`/`VibrationEffect`
  (new `VIBRATE` permission).
- a beginner-friendly, step-by-step VPS deployment guide in English and
  Russian ([docs/GUIDE.md](GUIDE.md), [docs/GUIDE.ru.md](GUIDE.ru.md)),
  linked from both READMEs.
- CI now cross-compiles Linux `amd64`/`arm64` server/client binaries and
  uploads them as a build artifact; GitHub Releases now attach the same
  binaries (with checksums) alongside the Android APKs.
- synced with upstream p1neappleXpress/OpenFlux through commit `3249724`:
  fixed swapped `maxToken`/`maxUid` flag descriptions, `yandex` transport now
  returns errors instead of panicking on unexpected document config (with new
  tests), and an experimental Yandex.Docs Volga transport (`vyandex.go`) is
  now in the tree.
- synced with upstream through commit `9ef5ab3`: exponential reconnect
  backoff and bounded WebSocket dial for the `yandex` transport; SOCKS5
  `Bind`/`Close` lifecycle plus a bounds-check fix for malformed domain
  requests; the MAX transport no longer kills the whole process
  (`os.Exit(1)`) when its connection drops - fatal when embedded as an
  Android library; panic recovery (`utils.SafeGo`) around background
  goroutines in the transport, tunnel and SOCKS5 layers; a `--local-ip` flag
  to scope the exit node's RST-drop iptables rule to a dedicated egress IP
  instead of dropping RSTs host-wide; aggressive GC on the exit node for
  small VPS instances.

### Changed

- Android application version is now 0.4.0 (version code 4).

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
