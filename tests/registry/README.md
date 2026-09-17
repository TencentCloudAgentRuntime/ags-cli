# Registry preview verification

`go test ./tests/registry` builds stable and preview binaries, checks every
Registry request member against its flag and schema, and exercises serialization
through a signed local HTTPS server. It is not cloud-service evidence.

## Authorized live tests

These tests create and delete Registry resources. Supply credentials for an
explicitly authorized account and region. They use an isolated CLI home and
confirm deletion with a fresh cleanup context; do not point them at production
resources.

Build the candidate with `go build -tags=preview -o /tmp/agr-registry-preview ./cmd/agr`.
Set `TENCENTCLOUD_SECRET_ID`, `TENCENTCLOUD_SECRET_KEY`, optional
`TENCENTCLOUD_TOKEN`, and `AGR_REGION` using your credential provider. Then run:

```sh
AGR_REGISTRY_E2E_BINARY=/tmp/agr-registry-preview \
AGR_REGISTRY_E2E_SCENARIO=registry.lifecycle \
AGR_REGISTRY_FIXTURE_URL=https://YOUR-REVIEWED-FIXTURE \
go test ./internal/patchscenarios -run TestRegistryLive -count=1 -v -timeout=8m
```

The full scenario covers CUSTOM approval/rejection/cancellation, labels, filtered
lists and audit logs, manual MCP/A2A content, version deletion, Skill package
upload/download checksums and manual Markdown, and remote MCP/A2A preview/sync
(unchanged, changed, and failed fetch). All successful responses are checked
against the effective API schema. Optional fields absent from a real response
are not asserted to have been observed; their serialization is covered by the
local binary tests.

For diagnosis, the component scenarios are `registry.custom.lifecycle`,
`registry.skill.lifecycle`, and `registry.remote.lifecycle`. A component pass
is not a complete patch validation.

## Remote metadata fixture

`fixture/main.ts` is a standalone Deno service with no secrets or dependencies.
Deploy that directory to an explicitly authorized Deno Deploy test app. It
implements MCP metadata and an A2A 1.0 Agent Card only; it cannot execute tools.
`/case/<changeAt>/<failAt>/mcp` and `/case/<changeAt>/<failAt>/agent.json` path timestamps change metadata and return HTTP 503,
respectively, without mutable shared state. Each test gets an independent URL.
The default change and failure windows are each 1 minute. Set
`AGR_REGISTRY_FIXTURE_WINDOW` (30s to 3m) for slower environments; the scenario
timeout scales accordingly, up to 14 minutes. The 3-minute cap leaves 6 minutes
of the strict runner's 20-minute budget for builds, other lifecycles and cleanup. If a phase overruns its window, the run fails and
requires a larger window, never a passing result. Increase the outer `go test -timeout` too when using larger windows. Time windows reduce but do not eliminate
sensitivity to unusually slow cloud calls.
Run `deno test --no-config tests/registry/fixture` to check the fixture locally.

## Evidence boundary

`go run ./cmd/internal/apipatch coverage` checks that every contract delta is
bound to registered assertions. It does not execute the cloud tests. A local
working-tree test is not the strict committed-head report required by
[PATCH-VERIFICATION.md](../../PATCH-VERIFICATION.md). Generate that report only
after a clean candidate commit and PR head exist; never substitute a local run.
