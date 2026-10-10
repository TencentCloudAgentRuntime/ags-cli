# Canonical API promotion (issues #135 and #142)

Canonical tccli snapshot: SHA-256
`cb28d08735985809531e3cd9ad18a4515d1682ee6e32da86f838e829f53431f7`.
The checked-in `api.json` is byte-for-byte the public snapshot. Registry request
and response shapes are now populated; the empty-shape warning in the original
#135 baseline no longer describes this snapshot.

## Contract decisions

- All 19 Registry/Skill Package and 12 Session/Event Actions move into stable
  mapping/help. Their command paths and JSON transports are retained.
- Session get exposes `--num-recent-events` and `--after-timestamp` in both
  channels. AppendEvent uses canonical EventInfo instead of the obsolete
  EventInput projection. Nullable response members and JSON-string Event payload
  members retain their canonical types; no client-side stringification is added.
- DescribeQuotaOverview is explicitly raw-only via
  `agr api call DescribeQuotaOverview --request '{}'`; no new quota resource group.
- Volume/VolumeTemplate and storage-mount extensions remain preview-only. The
  remaining 36 API overlay operations rebase cleanly onto the canonical snapshot.
- AGS/common are pinned to v1.3.188; this AGS release requires common v1.3.188.
  The repository still compares the full request and response shape before SDK
  decoding and uses signed dynamic JSON for unsupported contracts. SDK runtime
  decoding and signed local transport tests verify compatibility; version
  availability alone is not used as evidence of wire compatibility.
- Pre-cache get accepts a task ID alone. The old digest positional argument and
  image triple remain available; digest is now optional at the CLI argument layer.
  The CLI rejects missing, incomplete, empty, or mixed ID/triple selectors locally for both flags and JSON requests. Create/get JSON responses
  preserve task IDs and all newly published fields, including large byte counts.

## Local verification

`TestPrecachePromotedContract` drives the real CLI over signed local HTTPS with
ID flags, inline JSON, files, stdin and the legacy triple. It checks output fields
and integer precision through the selected SDK or dynamic transport.
`TestPromotedRequestCoverage` derives fields from canonical metadata and checks
runtime flags, help and actual schema output. Both stable and preview run it.
Session lifecycle and EventCount scenarios run against both candidate channels
with deterministic fixtures and resource cleanup assertions. These are local
integration tests, not live-service evidence.

JSON request and field-flag input failures retain their original error causes
and diagnostic context: request-input/parse stage, input field, and file or stdin
location where observed. Regression tests cover missing files, stdin read errors,
malformed JSON through inline/file/stdin inputs, and empty stdin while preserving
usage error codes and exit status.

## Historical live verification (2026-09-29, ap-chongqing)

The stable candidate tested before the diagnostics merge passed Registry CUSTOM/manual MCP/A2A lifecycle
(45 CLI calls), Skill package/inline lifecycle (13 calls), and Session lifecycle
(58 calls) against the real service. Remote MCP/A2A changed/failed sync passed
on rerun (25 calls); the first run returned InternalError from A2A preview.
All scenario and diagnostic reproduction resources were deleted and cleanup
was confirmed.

The separate EventCount check remains failing: after appending one or two events,
DescribeSession returns EventCount=0 while DescribeSessions and DescribeEvents
return the correct counts. A repeat after five seconds gave the same result.
This remains a known backend issue tracked by #135; the assertion is retained
and its failure is not presented as a pass. A passing result or explicit contract
decision is still needed to complete that acceptance criterion.

These runs used the uncommitted candidate, not a strict committed-head report.
Pre-cache image task creation was not tested against the real service; its new
request/response contract was verified through signed local HTTPS tests.

After promotion, Registry/Session bindings are removed from overlay-only coverage;
remaining Volume bindings are unchanged. Live acceptance remains available via
`AGR_REGISTRY_E2E_BINARY` with `TestRegistryLive` and `AGR_SESSION_E2E_BINARY` with
`TestSessionLive`. The latter runs both lifecycle and EventCount with cleanup.
Both require an explicitly selected candidate binary and authorized environment.
