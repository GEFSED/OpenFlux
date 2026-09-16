# Exit node differences from upstream

This document compares the code that affects the exit-node and client
(desktop CLI) binaries with the current state of `p1neappleXpress/OpenFlux`
(`main`, as of 2026-09-12). The goal is to understand exactly what breaks
compatibility between a client on one side and an exit node on the other, so
we can decide what to sync and what to leave as-is.

The Android app is not covered here: upstream has no Android client to
compare against.

> **Important note on branches.** Everything below describes the
> `experimental` branch, not `main`. After the core was resynced with
> upstream (`flx-kernel`), `main` carries none of its own framing, mandatory
> encryption, or country detection - it is nearly identical to upstream and
> wire-compatible with it out of the box. Framing/DNS-relay/ping-graph/
> country live only on `experimental` and require an exit node running that
> branch's own code. See [FORK.md](FORK.md) for what each branch is for.

## 1. `transport/encrypted.go` - critical, breaks compatibility entirely

In this fork, every packet is wrapped in a 1-byte "frame type" (data / ping
request / ping response / DNS request / DNS response) before encryption -
needed to carry ping and (recently) DNS resolution over the same encrypted
channel as regular data.

In the merged upstream PR `#38 upstream-pr-aes-gcm` (the same code as in
this fork, but an earlier version - without frames), `Send()`/`Receive()`
work with raw plaintext with no such leading byte.

Result: if a client from this fork sends a packet to an upstream exit node
(or vice versa), the receiving side decrypts a packet with an extra leading
byte and the IP header gets corrupted. This isn't limited to DNS relay - it
affects any data transfer, since both VPN mode and Proxy mode in the Android
app use the same code. The divergence started back when ping frames were
added, well before the recent DNS work.

## 2. New frame types `frameDNSRequest`/`frameDNSResponse`

Separate from item 1: DNS resolution through the tunnel itself was added
recently - the client sends a raw DNS query to the exit node, which reaches
out to an upstream DNS server over UDP from its own network and replies
through the same encrypted channel. Even if the frame protocol from item 1
were synced, these two new frame types would still need to be present on
both sides at once for DNS relay to work.

## 3. Encryption: mandatory vs optional

In upstream's `main.go`, encryption is optional: without
`--encryption-key-file`, traffic goes through completely unencrypted. In
this fork, encryption is always mandatory - the process refuses to start
without a key (a deliberate fork policy, see [FORK.md](FORK.md)).

## 4. `vyandex` (Volga) transport

Upstream wires it up behind `--transport vyandex`. In this fork, the file
`transport/yandex/vyandex.go` exists in the tree but is deliberately not
wired to a CLI flag - it lacks the mandatory encryption wrapper, and turning
it on would conflict with this fork's "always encrypted" model.

## 5. `tunnel/tunnel.go` - gVisor TCP buffers

This fork uses `TCPBufDefault = 1MB`, `TCPBufMax = 8MB`; upstream uses
`256KB`/`1MB`. A pure performance tweak, no protocol impact, no
incompatibility introduced.

## 6. `tunnel/tunnel.go` - DNS resolver for `DialTCP`

This fork adds an optional `SetDNSResolver` (uses
`EncryptedTransport.ResolveDNS` from item 2). If no resolver is set - as in
upstream's desktop CLI - behavior is unchanged, plain local resolution. This
part is backward compatible on its own; incompatibility only arises together
with items 1/2.

## 7. `socks5/socks5.go` - authentication and telemetry

Added: optional SOCKS5 username/password authentication (RFC 1929), byte
counters for the speed indicator in the notification, and correct RFC 1928
responses for IPv6 addresses or unsupported commands instead of silently
dropping the connection. All of this runs on the Android app's local SOCKS5
server (Proxy mode) - it has no effect at all on the protocol between the
client and the exit node.

## 8. `main.go` - exit-node country detection

This fork's exit node makes a single `ip-api.com` request on startup,
determines its own country, and publishes it to the client through the
existing ping frames (`EncryptedTransport.SetCountry`). Upstream has nothing
like this. It relies on the same frame protocol as item 1, so it is also
tied to compatibility.

## 9. `utils/debug.go` vs `utils/logging.go`

This fork adds `SetLogSink` (to mirror debug logs into the Android app).
Upstream renamed the file to `logging.go` and added `SetOutput` (output
redirection). Different, mutually incompatible small enhancements to the
same spot; neither has any critical impact.

## 10. iOS

iOS code reappeared upstream (`export_ios.go`, `export_ios_packet.go`,
`build_ios.sh`, `tunnel/packettunnel.go`). This fork deliberately does not
carry it over (see [FORK.md](FORK.md)) - iOS is not supported.

## What actually needs deciding

Items 1, 2, 3, 4 and 8 are all linked - they all use the same
`EncryptedTransport` frame protocol, and together they determine whether a
client from one side can work with an exit node from the other at all.
Items 5, 6, 7 and 9 can be left as-is with no compatibility consequences.
