# Legacy/AES wrapper-order retest — Perf Lab 4

ROOT_CAUSE = Perf Lab Legacy+AES wrapper-order mismatch
ROOT_CAUSE_CONFIDENCE = HIGH
FIRST_DIVERGENCE_LAYER = AES header validation caused by Legacy outside AES

Production source: `081d214300c1067f17f6c0d02f84a8491f1a7b98`.
Broken Perf Lab: `36aee5302dc702a251c8362adfec25388708a06d`.

## Proven boundary and correction

Production main.go and mobile/mobile.go construct raw -> AES -> Legacy.
Send is IP -> Legacy -> AES -> Volga; receive is Volga -> AES -> Legacy -> IP.
Perf Lab previously constructed raw -> Legacy -> AES, putting Legacy framing
outside the encrypted OFX/version header on the wire.

Server comparison found 828 CONTROL AES successes, Legacy successes and proxy
callbacks. Perf Lab had 235 Volga frames: 232 bad AES headers, 3 too short,
zero decrypt failures/successes and zero Legacy/proxy callbacks. This is a
framing error before decrypt, not evidence of a wrong secret or TUN failure.

The Baseline constructor and every other Perf Lab Legacy profile now use
production v0.6.0 order. The misleading baseline order comment is corrected.
Baseline retains its original v1.0.0 lifecycle/queue/worker settings; its Legacy
wire order is explicitly the exact v0.6.0 production order. No claim is made
that every runtime detail equals v0.6.0.

Batched behavior, ordinary non-lab Start, performance parameters, transport
lifecycle, AES framing/KDF, codec internals and VPS production are unchanged.
Older audit reports describe historical tests against a different v1.0.0 peer;
their symmetric Legacy/AES compatibility conclusion does not apply to this exit.

## Independent regression proof

`testsupport/productionv060peer` freezes production encrypted.go/compressor.go.
Only their package declaration is renamed. Tests reverse the rename and check
original Git blob hashes, including the exact source bytes. The constructor
matches production main.go/mobile.go and never calls the Perf Lab stack helper.

Mobile tests exercise actual Baseline startup/Send/Read and actual optimized
wrapper/finishStart/callback paths against that independent peer, for every
profile with Legacy, with and without AES. Both directions must pass. The raw
carrier asserts OFX/version/direction BEFORE forwarding the encrypted frame.
158-byte and 1400-byte synthetic packets cover raw Legacy and LZ4 compression.
A transparent post-AES test tap strictly decodes Legacy to ensure fallback
cannot masquerade as a successful decode.

The negative fixture reproduces raw -> Legacy -> AES. Production rejects it
before the post-AES tap or packet callback. Removing only its outer Legacy
framing lets independent production AES authenticate the very same frame,
distinguishing this bug from a key/decrypt error. Linux CI also temporarily
restores the two old Android constructors and requires the new compatibility
test to fail at the OFX assertion for all four profiles, then restores and
verifies the fixed sources. No fixture contains real URLs, keys or packets.

## Validation and phone acceptance

Only Linux CI runs diff checks, root/mobile tests, race tests, vet and builds,
the old-constructor regression proof, Android arm64 build and Java tests.
The Android job depends on Go validation. It verifies package
`io.openflux.app.perflab`, version `1.0.0-perflab.4`, versionCode 13,
arm64-only ABI, and publishes an Actions artifact with SHA256SUMS.
No Release, upstream PR, VPS deployment or Windows executable is involved.

After CI and artifact verification, RESULT = ReadyForWrapperOrderRetest.
This is not real-phone acceptance and does not resume performance A/B.

1. Update Perf Lab; retain user2.
2. Select Legacy + Baseline and connect.
3. Open one simple website, wait 15 seconds.
4. Copy diagnostics, then stop.

Expected: page loads, connected=true, ws_connected=true,
encrypted_receive_success > 0, mobile_callback_packets > 0, download_bytes > 0.
No Speedtest. Keep ordinary OpenFlux disconnected during this test.
