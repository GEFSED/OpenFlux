# TUN classification retest — Perf Lab 3

RESULT = BlockedByAndroidTunWriteEINVAL (until build/test/artifact verification).
Performance A/B remains suspended. No memory tuning, Release or upstream PR.

## Real evidence and limits

Retest of `3b40f14a6228720260560c1ec5bb2432b9057771`:

* Batched: WS connected, 10 relay/inner/outer frames, 10 outer decode errors,
  zero AES receive or mobile callbacks. That profile used the wrong codec.
* Legacy: 3 encrypted receive packets, 2 too short, 1 authenticated success,
  zero decrypt failures; one 158-byte mobile callback/enqueue. Then Android
  `FileOutputStream.write` returned EINVAL and the service stopped.
* **CODEC_CONTROL_CONFIRMED=yes**: user checked the ordinary working profile
  and explicitly confirmed its codec label is **Legacy**.

The return carrier, Legacy decoding and AES authentication reached the mobile
callback at least once. This does not prove that the 158 bytes form valid IP,
nor that the Android descriptor/driver will accept a valid packet. **ROOT_CAUSE
= unknown; confidence in a specific cause = insufficient.** No real packet
contents, addresses or credentials were collected to make this report.

## Diagnostic behavior (Perf Lab only)

`TunPacketClassifier.classifyPacketForTun` is a pure, read-only helper. The
common `inject` point classifies both mobile return packets and locally built
DNS responses before any attempted TUN write. Production builds still call
`output.write(packet)` directly and propagate errors as before.

Obviously non-IPv4/short/impossible-header packets are counted and dropped;
subsequent packets continue. The helper checks version, minimum IPv4 header,
IHL bounds, Total Length >= IHL and Total Length <= supplied array. It does not
pretend to validate TCP state, checksums, MTU, routing, IPv4 options or the TUN
descriptor. Fragmentation and destination equality are metadata, not reasons
to silently discard otherwise structurally valid IPv4.

For a structurally valid IPv4 packet, an actual EINVAL increments both
`tun_write_einval` and **`tun_valid_ipv4_write_einval`**. It is never relabelled
as malformed. The first nine are dropped without destroying the session;
the **tenth cumulative valid-packet EINVAL** fails the session. Successful
writes in between do not reset this limit. Other errors retain normal failure
behavior; local DNS keeps its existing error handling except the same EINVAL
limit applies to its writes too.

The errno reader first uses the typed Android
[`ErrnoException.errno`](https://developer.android.com/reference/android/system/ErrnoException#errno)
in the cause chain. If a platform loses that cause, it recognizes only the
standalone EINVAL token in the exception message. Raw exception text is never
exported; unknown errno is -1. Error messages in the Perf Lab writer are numeric.

Counters reset only for a new session and survive Stop/fail/service teardown
in the current app process. The snapshot holds metadata, never packet arrays.
`tun_write_attempts` counts actual writes; pre-validation drops are counted
separately by `tun_invalid_packet_drops`. `packet_classification` counts every
packet offered to injection, including invalid ones; counters can overlap.
Protocol categories apply when the IPv4 protocol field is available.

`last_tun_failure_*` records the last actual IOException, and is not overwritten
by later success or validation drops. Before any failure its numeric fields
are -1 and boolean fields false. Validation drops separately retain only a
fixed reason enum, buffer length and version (`tun_last_invalid_*`). Separate
mobile/DNS classification counts prevent mistaking local DNS writes for carrier
return traffic. No source/destination address, port, payload, URL, key, nonce,
ciphertext, token or cookie is exported.

## Total Length smaller than the buffer: no trimming

[RFC 791](https://www.rfc-editor.org/rfc/rfc791.html#section-3.1) defines Total
Length as the IP datagram size including its header. That does not establish
that trailing bytes in this application are harmless padding. Audited source:
`TunnelLinkEndpoint.WritePackets` uses the complete `pkt.ToView().ToSlice()`;
the wrappers preserve lengths; mobile copies the complete plaintext, and
gomobile returns that byte array. There is no application padding contract.

Therefore Total Length < buffer is counted distinctly but **the original
array and full length are passed unchanged to TUN**. It passes the requested
structural bounds check, with the trailing-byte anomaly recorded. If that
write returns EINVAL it is counted as valid-IPv4 EINVAL, not assumed malformed.
A regression test verifies array identity, full 158-byte write and preserved
nonzero trailing bytes. No speculative repair or trimming was introduced.

## Can an authenticated non-IP control payload legitimately arrive?

**AUTHENTICATED_NON_IP_PAYLOAD_EXPECTED = no for the audited normal main-branch
packet exit path; unknown for the deployed exit binary, which was not accessed.**

Audited Android base `8566f727c8238436728758f139130cef433147b7`, Perf Lab source,
and upstream main at `d34dc8caa70ca059cd80d8f5753499361052dabc`:

* [Upstream endpoint](https://github.com/p1neappleXpress/OpenFlux/blob/d34dc8caa70ca059cd80d8f5753499361052dabc/tunnel/endpoint.go#L36)
  emits gVisor IP packet bytes; `AddHeader` is empty (no Ethernet/TUN-PI prefix).
  [Tunnel callback](https://github.com/p1neappleXpress/OpenFlux/blob/d34dc8caa70ca059cd80d8f5753499361052dabc/tunnel/tunnel.go#L102)
  passes those bytes directly to `trans.Send`. The normal network stack is IPv4.
* [AES Send/Receive](https://github.com/p1neappleXpress/OpenFlux/blob/d34dc8caa70ca059cd80d8f5753499361052dabc/transport/encrypted.go#L108)
  has no application frame-type byte or ping/DNS demultiplexer. It authenticates
  arbitrary bytes and invokes the callback; successful AES **does not validate IP**.
* [Volga keepalive](https://github.com/p1neappleXpress/OpenFlux/blob/d34dc8caa70ca059cd80d8f5753499361052dabc/transport/yandex/vyandex.go#L1033)
  sends a zero byte directly to `relay.Send`, below codec/AES. A synthetic test
  proves it fails the AES minimum length check and never authenticates. The two
  real too-short packets are consistent with this, but were not captured and
  cannot be positively identified as keepalives.
* WS JSON pings return from `handleMessage`; action/frontier items return from
  `handleBundleItem`. Neither generates an authenticated application control frame.
* Android's DNS response is constructed locally and injected separately, never
  passed through AES. Main's encrypted code lacks the old experimental DNS/ping/
  country multiplexing described (explicitly as experimental-only) in
  `docs/en/UPSTREAM_DIFF.md`. No detection/removal of an imagined prefix is added.
* A different/experimental peer, a benchmark role using `trans.Send` on arbitrary
  data, or any holder of the secret **can** produce authenticated non-IP bytes.
  That is possible cryptographically, but not a legal control message expected
  from the audited normal packet exit. The synthetic B case demonstrates it.

This audit does not identify the user's 158-byte packet or assert which exit
version generated it. The VPS and its configuration have not been touched.

## Local proof and tests

One shared synthetic corpus with SHA-256 identities is consumed by both Go and
Java. No real packet captures/addresses are used. All buffers are 158 bytes:

| Case | Plaintext | Java classification | Diagnostic write policy |
|---|---|---|---|
| A | IPv4 TCP, IHL 20, Total Length 158 | IPv4/TCP/exact | attempt write |
| B | non-IP bytes, version nibble 3 | unknown version | drop, continue |
| C | IPv4 TCP, Total Length 100, trailing bytes | IPv4/TCP/lt-buffer | write all 158 bytes unchanged |
| D | IPv4 header claims length 180 | IPv4/TCP/gt-buffer | drop, continue |

Tests construct authenticated frames using the frozen base exit wrapper. The
real Volga inner parser -> Legacy -> AES callback path preserves each shape.
Separate tests run the actual Baseline mobile constructor/callback/Read path
and verify four authenticated 158-byte deliveries. Java tests classify exactly
the same bytes (shared corpus hashes), including the invalid authenticated
payload. This boundary composition does not simulate Android kernel acceptance.

Java tests also cover TCP/UDP/ICMP, IPv6, unknown version, null/short buffers,
IHL below minimum/above buffer, options, zero/short/oversize Total Length,
MF/offset fragments versus DF, all requested counters, invalid-drop continuation,
valid-EINVAL continuation/threshold, other errors, snapshot retention/reset,
safe field allow-list, and no trimming. Go tests/race/vet/build run in root and
mobile modules. CI runs the Java suite and arm64 APK build.

## Single minimal user retest

Check the ordinary working OpenFlux profile and confirm its codec label.
This was confirmed **Legacy** in this conversation. If it is actually Batched,
STOP and revisit codec inference before interpreting this test.

1. Update/install **only Perf Lab** `io.openflux.app.perflab`,
   `1.0.0-perflab.3`, versionCode 12 (arm64).
2. Keep **Legacy + Baseline** and the same profile/exit.
3. Connect, open **one simple website**, wait **10–15 seconds**.
4. Copy diagnostics immediately, then stop.

No speedtest, profile A/B or memory optimization. Memory-heavy v1.0.0 Baseline
remains intentional. No signing credentials are requested. If Android rejects
an update due to an ephemeral CI debug-signature mismatch, reinstall only Perf
Lab after retaining its profile locally; leave the ordinary working app intact.

Only after CI/artifact verification may the build status become
**ReadyForTunClassificationRetest**. This is not a claim that Perf Lab is fixed.
