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
  They follow `NextToken` with unchanged options and verify an exact Image filter.
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
