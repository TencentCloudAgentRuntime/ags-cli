# API sync live verification

These tests build the current CLI and call the configured real AGS endpoint.
They are skipped unless explicitly enabled:

```sh
AGR_TEST_API_SYNC_LIVE=1 go test ./tests/apicontract -count=1 -v
GOFLAGS=-tags=preview AGR_TEST_API_SYNC_LIVE=1 go test ./tests/apicontract -count=1 -v
```

Credentials, region, endpoint and domain use the existing `tests/testutil`
configuration. Keep credentials out of command arguments.

- Pre-cache checks require an existing successful task in the first 100 list
  results of `agr pre-cache-image-task list`. They compare its exact `ReadyTime`
  through list JSON, the text table and task lookup, including the timezone
  offset. An accepted `image_volume` lookup may return
  `Pending`; that verifies query support, not successful volume pre-warming.
  `ReadyTime` is optional even for `Success`; when supplied, it must remain a
  valid completion timestamp.
- List pagination checks require at least two existing image pre-cache resources.
  They follow `NextToken` with unchanged options and verify an `image` substring
  filter. They also query `pre-cache-image-id`, `image-registry-type` and
  `source-type` separately and check that returned fields match each selector;
  an ID filter must return exactly one task. Together with the ReadyTime checks,
  the suite exercises all six documented filter names.
- Registry checks browse `MARKETPLACE` without `RecordId`, verify the five new
  version fields, and exercise positive and negative metadata filters. A
  marketplace version with nonempty metadata must exist.
- Event center checks exercise `Raw` and `Aggregate` queries through `api call`.
- Set `AGR_TEST_WAA_SOURCE_TOOL_ID` to an existing ACTIVE WAA tool to test both
  start modes. The test forks a temporary tool, creates instances with a
  five-minute lifetime, waits for RUNNING, deletes them and verifies cleanup.
  Without that fixture the WAA case is explicitly skipped.

GPU allocation requires an existing purchased GPU resource group; decoding and
transport tests do not establish successful GPU provisioning. The live checks
above do not purchase resources or test GPU allocation.

## Sandbox keepalive and log format (preview)

The preview-only `sandbox.keepalive` and `sandbox.log-format` scenarios run as
part of this suite through the registered patch scenarios:

```sh
GOFLAGS=-tags=preview AGR_TEST_API_SYNC_LIVE=1 go test ./tests/apicontract -count=1 -v -ginkgo.focus='Sandbox keepalive and log format'
```

Both specs derive their assertion plan from `api/ags/v20250920/e2e-coverage.yaml`
and require a `pass` result with a passing cleanup and at least one real CLI
call; a skipped or empty plan is not acceptance. The executed assertion set must
equal the bound set exactly, so every assertion a scenario registers has to be
bound to at least one manifest entry: an unbound one fails the spec even when
the backend behaved correctly. `TestRegistryAssertionsAreBoundInManifest` in
`internal/patchscenarios` enforces that offline for both scenarios.

- `sandbox.keepalive` creates one disposable code-interpreter tool and several
  `30m` instances in `ap-guangzhou`, checks tool defaults, per-member instance
  inheritance, explicit `false`, `{}` overrides, mode-only and timeout-only
  overrides, the `300s`/`1800s` boundaries, `tool fork` copy/override, and reads
  the stored values back through `tool get/list`, `instance get/list` and
  `api call DescribeSandboxInstances`. Service-side rejections are probed with
  `api call` against a disposable instance; unknown top-level members are
  rejected locally and are not counted as backend evidence.
- `sandbox.log-format` requires an existing CLS topic and a role usable for it,
  supplied as the non-sensitive fixtures `AGR_TEST_CLS_TOPIC_ID` and
  `AGR_TEST_ROLE_ARN`. It creates disposable tools to check omitted, empty,
  `raw`/`RAW` and `auto`/`AuTo` values through `tool create`, `tool get/list`
  and `tool fork`, plus the two structured rejections (no topic with `auto`, an
  unknown format). Missing or unusable fixtures fail the scenario instead of
  skipping it. Every tool in this scenario uses a network mode with egress
  (`PUBLIC`), because the service rejects `LogConfiguration.CLSConfig` on
  `SANDBOX` with `InvalidParameterValue.CLSConfig`: the log sidecar cannot push
  to CLS from the sandbox network. Tools that carry the topic are also
  `custom` with a custom configuration, because the service rejects a
  `CLSConfig` on the built-in types with `InvalidParameter` "only supported for
  custom sandbox type for now"; the custom tool uses a reviewed system image so
  it never pulls a user image, and it starts the sandbox agent of that image
  (`/usr/bin/envd`) with no args: `Command` is a required member the service
  answers `MissingParameter` without, and a start command that leaves the agent
  down makes the tool stop at `FAILED` with an internal dependency error.
  The keepalive scenario stays on `SANDBOX` with
  `code-interpreter`. The rejection probes keep the same tool type and egress
  network so neither a type nor a network precondition can stand in for the
  `ContentFormat` validation under test. The suite never creates CLS or CAM
  resources, never deletes the
  fixture topic or role, and does not read either value into metadata.

Both scenarios wait (read-only, with a deadline) for a created or forked tool to
report `ACTIVE` before reading the copied configuration back: create and fork are
asynchronous, so an immediate readback observes a tool that has not published its
members yet. A failure of that wait reports the observed status together with the
service's `StatusReason` and the preconditions the scenario chose (tool type,
network mode, log `ContentFormat`, image and command), because the report itself
carries no raw response: a tool that stops at `FAILED` is then triaged from the
run log instead of costing another live round.

Cleanup deletes every instance and tool created by the run and then queries them
independently until they are gone or stopped; `Cleanup=pass` reflects that
verification, not the exit code of `delete`. The scenarios prove configuration
persistence, normalization and cleanup only. They do not prove data-plane
keepalive renewal, the server-side log extraction engine, or actual log
delivery, and those claims stay unverified until an API owner accepts the
semantics described in `api/ags/v20250920/CONTRACT-NOTES.md`.

## Dedicated operational event acceptance

The `Operational event command` group exercises actual CLI subprocesses
through `agr event list`, separately from the raw-call sync check:

```sh
AGR_TEST_API_SYNC_LIVE=1 go test ./tests/apicontract -count=1 -v -ginkgo.focus='Operational event command'
GOFLAGS=-tags=preview AGR_TEST_API_SYNC_LIVE=1 go test ./tests/apicontract -count=1 -v -ginkgo.focus='Operational event command'
```

Its six scenarios check nonempty Raw/Aggregate results and wire types, matching
and nonmatching ObjectId filters, fixed-window pagination and Asc/Desc sorting
in both modes, inclusive start/exclusive end, text/default queries, and local
validation plus service-enforced retention/future-time errors. Optional fields
are checked when supplied; missing response fields are not fabricated.

The group explicitly sets `AGR_NON_INTERACTIVE=1` on each CLI runner. The shared
test runner removes ambient `AGR_*` variables, including the pipeline's setting;
passing it to the subprocess suppresses automatic update notices while keeping
the empty-stderr assertions intact. A credential-free warm-cache regression
reproduces the notice and verifies this setup without calling any cloud API.

These queries create no resources. They use existing events from a fixed six-day
window ending two minutes before the run to leave indexing headroom. Pagination
requires at least two results with distinct timestamps among the first 100 rows
in each mode, avoiding assumptions about tie ordering. Missing fixtures fail the
scenario; empty results or skipped suites do not satisfy dedicated acceptance.
The default one-hour query may be empty; nonempty acceptance uses the explicit
fixed window. Record the tested commit, build channel, environment alias,
timestamps, pass/fail/skip counts and any fixture limits with the live result.
