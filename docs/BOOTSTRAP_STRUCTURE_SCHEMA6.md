# Structure-only bootstrap observations, schema 6

Base: `3774ace1f58feb2c303df7db7220826c946d545e`. This change is local/CI
preparation only. It does not authorize a deployment, provider request, service
start, or phone test. The last real user3 result remains unidentified complete
200 Docs HTML, not proven CAPTCHA and not proven parser obsolescence.

## Frozen production behavior

The source proof in `scripts/ack-diag-verify.py` compares the entire transport,
tunnel, network, mobile, main, logger, dependencies, correlator and process-argv
validator byte-for-byte with the base. Only bootstrap observations, schema
dispatch/validation, tests, documentation and the workflow change. Startup.go
changes only its schema version constant.

| Baseline | Behavior retained |
|---|---|
| Request | Same GET construction, headers, cookie jar, 30-second client timeout |
| Redirect | Same manual redirects, ErrUseLastResponse, maximum ten iterations |
| Read | Same io.ReadAll; error is observed but still ignored by production decisions |
| Status | Same config search on every final non-3xx response, including non-2xx |
| Parser | Same case-sensitive, double-quoted id="client-config" regexp; dot does not cross newlines; same JSON UseNumber and field extraction |
| Authorization | Same officeActionData.action_url/access_token use; optional ttl; same subsequent requests |
| Retry | No new retry, backoff, rejection, alternative config selection or authorization fallback |

Alternative inline JSON is **never returned to authorization**. Its values
remain in temporary scanner memory. Production can still fail client-config
matching even when the structural diagnostic sees the required field shape.

## Closed response contract

Schema is exactly 6 for startup, bootstrap response and snapshots. Schema 5's
fields, error classes, strict journal handling and startup state machine remain
available as an explicit historical contract; schema 6 invocations reject mixed
versions. No generic extension map exists.

New response fields:

* `final_path_class`
* `script_total_count`, `script_inline_count`, `script_external_count`,
  `script_json_count`, `script_module_count`
* `form_count`, `iframe_count`, `noscript_count`, `stylesheet_count`
* `root_container_class`
* `initial_store_present`, `initial_store_structure_class`
* `other_inline_bootstrap_present`, `other_inline_bootstrap_structure_class`
* `expected_docs_app_markers`: exactly four booleans: `legacy_client_config`,
  `public_initial_store`, `editor_root`, `generic_root`
* `browser_compatibility_marker`: YES / NO / UNKNOWN
* `static_resource_family_classes`: fixed family counters listed below
* `required_auth_field_shape_present`, `optional_ttl_field_present`,
  `candidate_container_count`
* `structure_scan_limit_reason`, `structural_classification_conflict`,
  `page_structure_class`

Existing `structure_scan_complete` is retained, now covering both the existing
classifiers and the new scan. Neither absence nor final page inference is usable
when it is false. Positive flags observed before a limit remain partial evidence.

### Enums

`final_path_class`:
DOCS_ROOT, DOCS_EDITOR_LIKE, DOCS_VIEWER_LIKE, DOCS_PUBLIC_SHARE_LIKE,
DOCS_ERROR_LIKE, DOCS_UNKNOWN_PATH_CLASS, YANDEX_AUTH_PATH_CLASS,
YANDEX_CHALLENGE_PATH_CLASS, OTHER_YANDEX_PATH_CLASS, UNKNOWN_PATH_CLASS.
Only fixed route prefixes are checked. Dynamic path components are ignored,
never decoded into IDs, printed or hashed. A challenge path is not a challenge
classification. Host suffix matching requires a label boundary.

`root_container_class`:
NONE, KNOWN_PUBLIC_DOCS_ROOT, KNOWN_EDITOR_ROOT, GENERIC_APP_ROOT,
MULTIPLE_ROOTS, UNKNOWN.
Preapproved literal IDs `root`/`app` are generic; `editor`/`document-editor`
are editor-like hints. A generic root plus validated landing initial-store shape
is KNOWN_PUBLIC_DOCS_ROOT. These are structural hints, not page authenticity.

`initial_store_structure_class`:
ABSENT, PUBLIC_LANDING_KNOWN_SHAPE,
EDITOR_COMPATIBLE_REQUIRED_FIELDS_PRESENT, EDITOR_REQUIRED_FIELDS_ABSENT,
MALFORMED, UNKNOWN_SHAPE.

`other_inline_bootstrap_structure_class`:
ABSENT, REQUIRED_OPENFLUX_FIELDS_PRESENT,
POSSIBLE_BOOTSTRAP_WITHOUT_REQUIRED_FIELDS, MULTIPLE_CANDIDATES, MALFORMED,
UNKNOWN.

`page_structure_class`:
LEGACY_EDITOR_CLIENT_CONFIG, PUBLIC_LANDING_INITIAL_STORE,
EDITOR_LIKE_INLINE_BOOTSTRAP, EDITOR_LIKE_NO_REQUIRED_BOOTSTRAP,
BROWSER_COMPATIBILITY, AUTH_PAGE, CHALLENGE_PAGE, PERMISSION_ERROR_PAGE,
GENERIC_DOCS_SHELL, AMBIGUOUS_STRUCTURE, UNKNOWN_DOCS_HTML.

Static resource families (fixed counter names):
YANDEX_STATIC_PSF, YANDEX_STATIC_DOCS, YANDEX_STATIC_GENERIC,
OTHER_YANDEX_STATIC, THIRD_PARTY_STATIC, RELATIVE_STATIC,
UNKNOWN_STATIC_FAMILY.
Only external script elements contribute. Exact yastatic.net `/s3/psf/` maps
to PSF; `/s3/docs/` and `/s3/docviewer/` to DOCS; other yastatic.net paths to
GENERIC. Boundary-checked Yandex hosts map to OTHER_YANDEX_STATIC. Other HTTP(S)
hosts map to THIRD_PARTY_STATIC. No host maps to RELATIVE_STATIC. Malformed,
empty, credential-bearing or unsupported-scheme src maps to UNKNOWN.
No URL survives in the result: no scheme, host, path, query, fragment or digest.

## Counting and JSON search

The x/net/html tokenizer handles case normalization, quotes, attribute ordering,
raw script text and malformed-but-tokenizable markup. Each script start or
self-closing token counts once. A `src` attribute (even empty) makes it external;
otherwise inline. Thus inline + external = total. MIME type application/json
(case insensitive, parameters parsed) counts as JSON; type module counts as
module. These categories are disjoint and their sum cannot exceed total.
Link rel token stylesheet counts once. Forms, iframes and noscript start tokens
count once. Markup inside executable script text or comments is not traversed.

Permitted candidate containers are inline application/json, or inline scripts
with literal ID client-config or initial-store. No JavaScript is executed, no
assignment-expression extraction is attempted, and JSON-LD is not searched.
`candidate_container_count` counts containers whose buffered content reached
inspection, including malformed JSON; a scan limit can stop before that point.

JSON is decoded using bounded recursive tokens with UseNumber. Duplicate keys,
trailing values and invalid JSON are MALFORMED. Objects/arrays are searched
recursively for officeActionData with nonempty string/number action_url and
access_token. Optional TTL presence is reported only in such a matching object.
Only booleans leave memory, never keys, values, snippets or hashes.

The public landing shape requires top-level router/environment objects and
subscription/tariffs/onboarding keys. This is based on the previous public-root
audit, not a proven editor contract. Other possible-bootstrap hints use only
officeActionData, editorParams, initialState, bootstrap, environment, router.
Unknown keys never leave memory. Multiple non-legacy/non-initial candidates are
conservatively MULTIPLE_CANDIDATES plus conflict; no candidate is chosen.

## Compatibility marker evidence limitation

The implemented fixed structural signature is a main/div with literal ID
unsupported-browser containing an anchor with data-action=update-browser.
It needs both elements in the same subtree; title/text, the ID alone, unrelated
anchors and strings/comments containing markup do not match.

This signature is a **synthetic contract fixture, not verified current Yandex
DOM**. The prior public audit captured a compatibility notice but no DOM marker
provenance. A positive result proves this exact structure only. It must not be
reported as proof of Yandex's browser policy without independent public marker
validation. The experiment therefore retains an explicit coverage limitation;
unknown HTML/notice text remains UNKNOWN. No new provider request is made here.

YES means the fixed composite marker matched. NO means a complete scan instead
found a different unambiguous known structural class. Otherwise UNKNOWN.

## Limits and deterministic conflict policy

| Budget | Hard maximum |
|---|---:|
| HTML bytes | 1,048,576 |
| HTML tokens (each scan) | 65,536 |
| Script elements | 1,024 |
| Total inline JSON bytes inspected | 786,432 |
| JSON recursion depth | 64 |
| HTML token nesting | 256 |

The existing production ReadAll still reads the original body unchanged. These
are diagnostic scanning limits, not networking limits. Tokenizer buffers are
bounded by HTML limit + 1. A complete public-root-like 525KB JSON fixture fits;
oversize/deep/malformed inputs never cause unbounded diagnostic recursion.

Limit reasons: NONE, HTML_SIZE, TOKEN_COUNT, SCRIPT_COUNT, INLINE_JSON_SIZE,
DEPTH, OTHER. First detected reason wins. Duplicate HTML attributes, unclosed
candidate JSON, unsupported decoding and incomplete body reads use OTHER.
Malformed but fully read JSON can be classified MALFORMED with a complete HTML
scan; it does not prove required fields absent in a valid candidate.

Positive classes are computed independently: legacy script ID, landing shape,
required alternative JSON, editor root with absent required fields, compatibility
signature, and unchanged schema-5 auth/challenge/permission markers. More than
one positive class, duplicate initial-store containers, a landing+editor shape
in one initial-store, or multiple other candidates sets conflict. Conflict always
produces AMBIGUOUS_STRUCTURE. Otherwise incomplete scans produce UNKNOWN_DOCS_HTML;
one positive class selects it; a generic root alone gives GENERIC_DOCS_SHELL;
remaining pages give UNKNOWN_DOCS_HTML. No favorable class wins a conflict.

## Validator and privacy proof

Schema 6 checks exact keys, enums, bool types (not coerced integers), bounded
counts, candidate/script partitions, fixed family sums, marker/presence/shape
relations, derived class/conflict and scan completeness. Unknown raw URL/path,
script src, DOM/body/HTML/JSON, credential/token/cookie/key fields fail closed.
Errors contain static classifications, never rejected values.

Fresh cursor, `journalctl --all`, exact invocation/PID/exe scoping and exact
NUL-separated argv/stable process checks remain. Null MESSAGE means unavailable
journal data, not proof the app emitted binary data. Startup failure before a
snapshot yields DiagnosticStartupFailed with stage/class and validated response
metadata; it never becomes readiness PASS. Incomplete or conflicting terminal
structure evidence cannot pass the readiness observation.

Go emits exact fixtures consumed by Python through the deployment journal path.
Tests cover all requested page/count/limit cases, secret-bearing strings and JSON
keys, scheme/host/path/query/document IDs, tokens/cookies/script/JSON values,
malformed contracts and a real local Linux child process with mocked journal
transport. No real systemd mutation or provider request occurs in these tests.

## One future authorized probe: interpretation matrix

| Complete, unambiguous observation | Permitted inference |
|---|---|
| PUBLIC_LANDING_INITIAL_STORE, no required shape | Supports public/landing shell; does not prove parser obsolete |
| EDITOR_LIKE_INLINE_BOOTSTRAP, required shape, old parser misses | Strong format incompatibility evidence; still validate how the alternative fields are meant to be used before any auth fix |
| LEGACY_EDITOR_CLIENT_CONFIG, old regexp misses | DOM script exists; old formatting assumptions may mismatch; not proof contents are valid auth |
| EDITOR_LIKE_NO_REQUIRED_BOOTSTRAP | Editor-like hints only; never invent required auth values |
| BROWSER_COMPATIBILITY | Fixed composite signature observed; current provider applicability requires public marker provenance |
| AUTH_PAGE / CHALLENGE_PAGE / PERMISSION_ERROR_PAGE | Specific existing structural marker evidence, not status-based inference |
| GENERIC_DOCS_SHELL / UNKNOWN_DOCS_HTML | Observation classes, root cause unknown |
| AMBIGUOUS_STRUCTURE or incomplete scan | No page/root-cause conclusion; retain explicit conflict/limit reason |

No future probe is executed by this change. A user3 probe still requires separate
authorization, Restart=no, exactly one start, a fresh invocation boundary and
the previously specified configuration cleanup. Permanent SSH access remains.
