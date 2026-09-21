# Android Volga Performance Lab baseline

Base: damnurmum/OpenFlux-Android `8566f727c8238436728758f139130cef433147b7`.
Latest release: v1.0.0 (tag at c52d208); main adds logo-only changes after it.
Preflight found no open PR or complete equivalent performance lab. Existing
batched+zstd work is already part of this baseline. No auth refresh changes from
#64/#79, UDP/IPv6, #80/#87 or proxy RACK changes are included.

## Packet path audit

| Stage | Copies / allocations | Synchronization / scheduling |
| --- | --- | --- |
| Android TUN read | 32767-byte reusable buffer; Arrays.copyOf for each packet; gomobile crosses Java/Go byte-array boundary | One blocking Java read thread. MTU is capped at 1500. No unsafe zero-copy introduced. |
| Mobile.send | Transport pointer under mutex; encrypted packets allocate nonce, header and ciphertext | AES-GCM is still outside the codec wrapper, exactly as this base CLI. |
| Outer BatchedTransport | Per-packet ownership copy, batch slice, framed buffer, zstd output and final frame | 4096-slot channel; one flusher; 8192-byte soft threshold, 64 packets, 5 ms linger. Threshold can overshoot by one packet, as on the base. |
| Volga relay Send | Another ownership copy into batchQueue | Baseline 2000 consumers, each with batch slice + timer, 20 frames / 2 ms / 4 MiB soft threshold. Concurrent consumers disperse light traffic across batches. |
| Volga HTTP | uint16 length prefixes, pooled blob, base64 scratch plus string copy, maps/interfaces/JSON and body copy | Per-request frontier mutex; atomic counters; blocking HTTP per worker; 2000/4000 idle connections configured. |
| Volga WS receive | JSON envelope/message parse; base64 decode; packet slices | One read loop; read deadline, reconnect delay. Auth behavior deliberately unchanged. |
| Outer receive / AES | zstd decode, copied packet records, decrypted packet | Nonce replay-window mutex; codec callback lock. |
| Mobile receive queue | Ownership copy; originally slice append/drop oldest | One mutex, depth 1024. Lab uses bounded ring storage with identical drop-oldest policy and a cancellable notification. |
| Java TUN write | gomobile returns a Java byte array | Baseline polls Mobile.read then Thread.sleep(2); up to approximately 500 wakeups/s. Output lock also serializes DNS injection. |
| DNS | copy query/response; socket per query | Cached executor has no thread bound, shared with lifecycle/read/write. DNS timeout is independent of Volga workers. |

The `relayClient.queue` field has only creation/close references; all packets use
`batchQueue`. Removing that unused channel preserves send behavior. The original
outer flusher can stay blocked on an empty channel after Stop, and relay Send
can race with Stop closing batchQueue. The lab cancellation changes are required
for reliable 100-cycle A/B testing; no auth-refresh behavior is ported.

## Original allocation measurement, before production edits

Windows amd64, Go 1.26.4, Intel i7-11800H; actual relay construction and workers,
fake credentials, **no network or Yandex calls**. This is the local part of Start,
not the memory of a real authenticated Android session. Values are bytes.

| Phase | HeapAlloc | HeapSys | HeapInuse | StackInuse | Goroutines | NumGC |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| before | 497880 | 3932160 | 1105920 | 262144 | 2 | 1 |
| two queues constructed | 48525080 | 54165504 | 49168384 | 294912 | 2 | 1 |
| 2000 workers started | 51612656 | 58884096 | 52551680 | 16613376 | 2002 | 3 |
| first base64 of 1400 bytes | 68394600 | 75661312 | 69345280 | 16613376 | 2002 | 3 |
| stopped, after GC | 18440120 | 84639744 | 19865600 | 7634944 | 2 | 4 |

The two channels cost 48,027,200 bytes measured, not just a capacity estimate.
The first encode adds 16,781,944 bytes, dominated by the 16 MiB pool allocation.
The pool can retain buffers until a GC and creates scratch space per concurrent
call; it is not a single shared 16 MiB allocation across workers.

## Pool selection

Compare 4/16/64 KiB initial capacities and 64/256/1024 KiB retention limits on
1400/8192/32768/65535/4 MiB payloads. 4 KiB initial capacity gives the smallest
cold allocation with essentially equal small-packet throughput. At 65535-byte
input, a 64 KiB retention cap requires ~184 KB/op versus ~90 KB/op with 256 KiB;
1024 KiB does not improve this case. Therefore use 4 KiB initial / 256 KiB max
retention, growing on demand and discarding larger scratch buffers.
This is a memory tradeoff, not a claim of faster real Volga throughput.
The old 16 MiB implementation is separately measured with its original unlimited
retention; combinations of a 16 MiB initial allocation and a smaller retention
limit are intentionally poor candidates, not a faithful original baseline.

Base64 output must match encoding/base64 byte for byte. Blob/JSON pools and the
4 MiB WebSocket buffers remain adjacent possible memory costs, not silently
retuned in this experiment. Candidate packet batching must respect the existing
uint16 Volga frame length; 64 KiB outer batches are excluded because encrypted,
incompressible frames can overflow it. No framing or AES changes are permitted.

Android PSS, Java/native heap, scheduler and JNI allocation cost require the
manual phone run. Desktop synthetic numbers cannot establish a production winner.
