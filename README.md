<div align="center">
  <img src="design/logo/avatar.svg" width="112" alt="OpenFlux logo">
  <h1>OpenFlux Android</h1>
  <p>Encrypted document-transport VPN for Android, desktop clients and Linux exit nodes.</p>
  <p>
    <a href="https://github.com/damnurmum/OpenFlux-Android/releases/latest"><img src="https://img.shields.io/github/v/release/damnurmum/OpenFlux-Android?display_name=tag&amp;sort=semver&amp;style=flat-square&amp;color=7aa2f7" alt="Latest release"></a>
    <a href="https://github.com/damnurmum/OpenFlux-Android/actions/workflows/ci.yml"><img src="https://github.com/damnurmum/OpenFlux-Android/actions/workflows/ci.yml/badge.svg" alt="CI status"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/damnurmum/OpenFlux-Android?style=flat-square" alt="GPL-3.0 license"></a>
    <img src="https://img.shields.io/badge/Android-8.0%2B-3DDC84?style=flat-square&amp;logo=android&amp;logoColor=white" alt="Android 8 or newer">
  </p>
  <p>
    <img src="https://img.shields.io/badge/Go-1.26.4%2B-00ADD8?style=flat-square&amp;logo=go&amp;logoColor=white" alt="Go 1.26.4 or newer">
    <img src="https://img.shields.io/badge/Java-17-ED8B00?style=flat-square&amp;logo=openjdk&amp;logoColor=white" alt="Java 17">
    <img src="https://img.shields.io/badge/ABI-ARM64%20%7C%20ARMv7%20%7C%20x86__64%20%7C%20x86-455a64?style=flat-square" alt="Supported Android architectures">
    <img src="https://img.shields.io/badge/IPv4%20%2F%20TCP-experimental-f59e0b?style=flat-square" alt="Experimental IPv4 and TCP support">
  </p>
  <p><strong>English</strong> · <a href="README.ru.md">Русский</a></p>
</div>

![OpenFlux Android: connection, logs and settings](docs/images/openflux-android-tabs.png)

> This repository is an experimental, independently maintained fork of
> [p1neappleXpress/OpenFlux](https://github.com/p1neappleXpress/OpenFlux).
> See [FORK.md](docs/FORK.md) for the differences from upstream. `main` is
> wire-compatible with current upstream exit-node/client binaries; a separate
> `experimental` branch carries additional features (ping graph, exit-node
> country, server-side DNS relay) that need this fork's own exit node - see
> [docs/UPSTREAM_DIFF.md](docs/UPSTREAM_DIFF.md).

OpenFlux is a research TCP tunnel with pluggable transports. This fork adds an
Android VPN client and optional end-to-end encryption for the Yandex Docs
transport.

**[Download the latest Android release](https://github.com/damnurmum/OpenFlux-Android/releases/latest)**

New to this? [docs/GUIDE.md](docs/GUIDE.md) is a beginner-friendly, step-by-step
walkthrough for deploying an exit node on a VPS and connecting from Android.

```text
Android VPN or SOCKS5 client -> encrypted document transport -> Linux exit node -> Internet
```

## Features

- Android 8+ client using the system `VpnService` API, with ARM, ARM64, x86 and
  x86_64 builds;
- a second Android connection mode, local SOCKS5 Proxy, for when a full
  system VPN isn't wanted - optionally exposed to the local network with
  SOCKS5 authentication, plus a `socks://` share link and QR code;
- Android 11-style UI with connection controls, logs and a phone-Settings-style
  vertical settings navigation;
- optional AES-256-GCM authenticated encryption with a key derived using
  scrypt, wire-compatible with upstream's exit-node and client binaries;
  leave the key empty to connect unencrypted to a plain upstream exit node;
- Android Keystore-backed storage for the document URL and shared secret;
- DNS-server field accepts any IP or hostname (not a fixed provider list),
  resolved locally on the device;
- per-app VPN routing (whitelist or blacklist which apps use the tunnel);
- a pinned notification with live upload/download speed and a disconnect
  action, for both connection modes;
- desktop SOCKS5 client and Linux exit-node modes;
- Yandex Docs and experimental MAX transport backends.

> **MAX transport warning:** the MAX backend sends packets via WebRTC
> DataChannel on your MAX account. Do not use a primary or important account;
> running it from an external VPS may lead to account restrictions that
> persist after OpenFlux stops. Treat MAX transport as experimental until its
> detection and blocking behavior is better understood.

## Important limitations

OpenFlux is experimental research software, not an audited replacement for
WireGuard or another mature VPN. The Android tunnel currently supports IPv4 and
TCP; arbitrary UDP and IPv6 are not tunneled (an IPv6 target or a UDP-only
request is rejected with a proper protocol error, not tunneled silently). The
document provider can still observe metadata such as connection times, traffic
sizes and encrypted payloads. Anyone with document edit access can disrupt the
connection.

Use the software only on systems and networks you own or are authorized to
test.

## Requirements

- Go 1.26.4 or newer for the desktop client and exit node;
- a Linux VPS/VDS with root access for the exit node;
- for Android builds: Java 17, Android SDK/API 35, Build Tools 35.0.0,
  NDK 27.0.12077973, Gradle 8.14.3 and `gomobile`;
- an editable document opened with the legacy Yandex Docs editor when using
  the Yandex transport.

## Prepare the private configuration

Create the following files locally and copy the same values to the exit node.
They are excluded by `.gitignore` and must never be committed:

```bash
printf '%s\n' 'https://your-own-document-url' > document-url
openssl rand -base64 32 > encryption-key
chmod 600 document-url encryption-key
```

The encryption key is optional: omit `--encryption-key-file` on both ends
(and leave the Android app's key field empty) to talk to a plain, unmodified
upstream exit node with no transport encryption. If you do set a key, it must
contain at least 16 characters, be a unique random value rather than a reused
password, and match on both ends. Rotate the document URL and the key if
either is exposed.

## Build the exit node and desktop client

The exit node and desktop client are the same binary; only the flags differ.
Prebuilt Linux `amd64`/`arm64` binaries are attached to every
[GitHub Release](https://github.com/damnurmum/OpenFlux-Android/releases/latest)
alongside the Android APKs. To build it yourself instead:

```bash
go build -o openflux .
```

By default the exit node runs in `--mode raw` (needs root on Linux; this is
what the rest of this section covers). Pass `--mode proxy` instead for a
root-free exit node that works on any OS - it dials outbound connections
with a plain `net.Dial` instead of a raw socket, at some throughput cost, and
none of the RST-drop steps below apply to it.

The exit node's TCP connections live in a userspace stack (gvisor), so the
kernel has no socket for them and sends an RST on every reply, tearing the
tunnel down. That RST must be suppressed - scoped, not host-wide. A blanket
`-j DROP` on all outbound RSTs makes every closed port on the box answer with
silence (scanners see `filtered` instead of `closed`) and stops the host from
resetting unrelated connections.

Recommended: give the box a second/alias IP dedicated to the tunnel and scope
the drop to it with `--local-ip`:

```bash
sudo iptables -A OUTPUT -p tcp --tcp-flags RST RST -s 203.0.113.10 -j DROP
sudo ./openflux --exit-node --transport yandex --local-ip 203.0.113.10 \
  --url-file ./document-url --encryption-key-file ./encryption-key
```

Even cleaner: run the exit node in its own network namespace or container so
the rule never touches the host's other services. `-m owner --uid-owner`
does not work here - the tunnel-breaking RSTs are generated by the kernel
with no owning socket, so the owner match never fires.

Host-wide fallback (only on a single-purpose box, understanding the trade-off
above):

```bash
sudo iptables -C OUTPUT -p tcp --tcp-flags RST RST -j DROP 2>/dev/null || \
  sudo iptables -I OUTPUT 1 -p tcp --tcp-flags RST RST -j DROP
sudo ./openflux --exit-node --transport yandex \
  --url-file ./document-url --encryption-key-file ./encryption-key
```

The sample [systemd unit](deploy/openflux.service) expects the binary and
private files in `/root/openflux`. Review its paths before installing it:

```bash
sudo install -d -m 700 /root/openflux
sudo install -m 755 ./openflux /root/openflux/openflux
sudo install -m 600 ./document-url ./encryption-key /root/openflux/
sudo install -m 644 deploy/openflux.service /etc/systemd/system/openflux.service
sudo systemctl daemon-reload
sudo systemctl enable --now openflux
sudo systemctl status openflux
```

Run the desktop client and configure the browser to use SOCKS5 at
`127.0.0.1:1080`:

```bash
./openflux --client --transport yandex --socks5 127.0.0.1:1080 \
  --url-file ./document-url --encryption-key-file ./encryption-key
```

Add `--debug` only when diagnosing a problem, and inspect logs before sharing
them.

## Build and install the Android app

Set `ANDROID_SDK_ROOT` (or `ANDROID_HOME`) and ensure `gomobile` and Gradle are
available, then run:

```bash
go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20260908204917-8b95e45f8d3e
go install golang.org/x/mobile/cmd/gobind@v0.0.0-20260908204917-8b95e45f8d3e
gomobile init
./build_android_app.sh
```

The build creates separate APKs for `arm64-v8a`, `armeabi-v7a`, `x86_64` and
`x86`, plus `OpenFlux-android-universal-debug.apk` for devices whose architecture
is unknown. Transfer the appropriate APK to an Android 8+ device, install it,
enter your own document URL and shared secret in **Settings**, then approve
Android's VPN prompt.

Configuration survives a normal in-place app update when the application ID
and signing certificate stay the same. Clearing app data or uninstalling the
app removes it. APKs signed with a different certificate cannot update the
existing installation. CI artifacts are debug builds; APKs attached to GitHub
Releases use the project's persistent release certificate. Moving from a debug
build to the release channel requires one uninstall and therefore clears saved
settings.

See [android/README.md](android/README.md) for Android-specific details.

## Command-line flags

| Flag | Default | Description |
| --- | --- | --- |
| `--client` | off | Run the SOCKS5 client |
| `--exit-node` | off | Run the exit node |
| `--mode` | `raw` | Exit-node internet path: `raw` (Linux, needs root, our default) or `proxy` (works everywhere, no root, falls back to automatically if raw mode can't get a raw socket) |
| `--local-ip` | empty | Exit node egress IP, for scoping the RST-drop rule (raw mode only) |
| `--socks5` | `:1080` | SOCKS5 listen address |
| `--transport` | `yandex` | Transport backend (`yandex`, `vyandex`, `oneme` or `cupsonline`) |
| `--url` | empty | Inline document URL; prefer `--url-file` |
| `--url-file` | empty | Read the document URL from a file |
| `--encryption-key-file` | empty | Optional: encrypt the transport with a shared secret from this file |
| `--maxToken` | empty | MAX transport token |
| `--maxUid` | empty | MAX transport user ID |
| `--debug` | off | Enable verbose logging |

`cupsonline` is an experimental transport riding the "shared cursor position"
feature of cups.online's live-coding interview rooms. As of this writing it
does not reliably deliver traffic end-to-end (the underlying cursor-broadcast
channel appears to throttle/coalesce rapid updates), so treat it as
unsupported until upstream fixes it.

## Development and security

Run checks before committing:

```bash
gofmt -w $(git ls-files '*.go')
go test ./...
go vet ./...
git diff --check
```

Contributions are described in [CONTRIBUTING.md](docs/CONTRIBUTING.md). Please
read [SECURITY.md](docs/SECURITY.md) before reporting a vulnerability. Changes
are listed in [CHANGELOG.md](docs/CHANGELOG.md).

## License

OpenFlux is licensed under the GNU General Public License v3.0 or later. See
[LICENSE](LICENSE), [COPYRIGHT](COPYRIGHT) and [NOTICE](NOTICE). This fork is not
endorsed by or affiliated with Yandex.
