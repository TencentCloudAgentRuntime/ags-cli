# Storage references and security context (issue #156)

Canonical source: [tccli AGS v20250920](https://github.com/TencentCloud/tencentcloud-cli/blob/a5dfe0a8e660/tccli/services/ags/v20250920/api.json),
release 3.1.183.1. The checked-in snapshot matches upstream bytes, MD5
`02b4653b4230783fc00c76f7522af329`.

## Contract decisions

- Both channels use canonical `SecurityContext`, `Capabilities`, `VolumeRef`
  and `VolumeTemplateRef`. Keep the published required/nullability attributes,
  including required `VolumeTemplateName`.
- Remove the four absorbed API overlay operations and their 70 coverage bindings.
  The remaining 32 operations still provide preview Volume/VolumeTemplate
  management. Publishing mount references does not publish those resource Actions.
  Both channel request skeletons include the canonical mount references.
  The overlay-only plan now selects `volume.lifecycle` alone; it cannot establish
  mount acceptance. `TestVolumeMountLive` independently selects the complete
  `volume.mount-lifecycle` scenario and its registered assertions after promotion.
- Pin AGS/common to v1.3.194, the AGS release containing the new structs and its
  required common version. The existing SDK compatibility/dynamic transport
  selection remains in place.
- Reuse `--custom-configuration` for tool create/update and instance create,
  and `--storage-mounts` for tool create. These already accept JSON, files and
  stdin, so no mapping or new flag is needed. Instance update has no such fields.
- Fork and debug copy through the active create contract, including nested
  security settings and mount references. Resolved Volume IDs take precedence
  over readback names in copied requests. Explicit configuration overrides keep
  their existing replacement semantics; debug's custom configuration overrides
  the instance configuration.

## Local verification

`TestStorageSecuritySignedRequests` exercises the production CLI with fake
credentials and a trusted loopback HTTPS fixture. It checks inline/file/stdin
flags, full JSON requests, explicit `Privileged:false`, capability lists, Volume
ID/name references, template reuse keys, SDK response decoding and fork/debug
inheritance/overrides. `TestFillRequestSupportsStorageSecurityContract` independently
checks SDK JSON decoding and serialization. The Volume reference projection test
runs in both channels after promotion. Invalid nested values must fail locally
without an API call; affected help/schema must retain their existing flag types
and describe the new nested fields.

Run both normal and `-tags preview` full suites, `apipatch check-all`,
`apipatch rebase --upstream <downloaded-api.json>`, `apipatch coverage` and
`cobragen check`. Check the affected help/schema in both channel binaries.

Verified on 2026-10-20 against the local working tree:

- `go test -race -p 4 -count=1 ./...` and the same command with `-tags preview`
  passed in an isolated home without cloud credentials.
- Canonical byte comparison, module verification, patch validation/rebase/coverage
  and generated-output checks passed.
- Actual stable/preview binary help/schema, command isolation, and Markdown/man
  generation passed.

These are local contract/transport checks. Real service acceptance, privilege
behavior and mounted data are not established by the loopback fixture.

## Real-service acceptance after promotion

Build the stable and preview candidates from the exact committed source being
reviewed, with an isolated CLI home and explicitly supplied cloud credentials.
Record the source commit/tree, both binary SHA-256 values, region/environment
alias, executed assertions, timestamps and confirmed cleanup. Keep credentials,
private fixture identifiers and raw responses out of the public report.

The mount scenario needs a disposable, reachable CFS filesystem, a supported tool
type and its role where required. It creates run-specific paths/resources and
confirms cleanup, including instances stopped, tools/volumes/templates absent
and removal of the run-specific CFS directory. Supply the fixture via
`AGR_VOLUME_CFS_FILESYSTEM_ID`, `AGR_VOLUME_TOOL_TYPE` and, when needed,
`AGR_VOLUME_TOOL_ROLE_ARN`; never substitute invented identifiers.

```bash
# Credentials and fixture variables come from the reviewed test environment.
AGR_REGION=<region> \
AGR_VOLUME_MOUNT_E2E_BINARY=/absolute/path/to/agr-preview \
go test ./internal/patchscenarios -run '^TestVolumeMountLive$' -count=1 -v -timeout=30m
```

This explicit entry point runs all 12 registered mount assertions: Volume
name/ID and template readback, real data read/write, read-only enforcement,
template reuse/isolation, fork, invalid references, Delete/Retain reclaim
policies and cleanup. It uses the preview candidate because fixture management
commands remain preview-only. For stable acceptance, prepare references with the
preview candidate and separately exercise stable tool create/fork and instance
create/readback/exec against those references; record each candidate's results.

In both candidates, verify `SecurityContext` through tool create/update/readback,
fork inheritance/override and instance configuration, then inspect the running
container's capabilities to establish the runtime effect of explicit
`Privileged:false` and Add/Drop settings. Check debug inheritance/override through
its actual workflow when the selected tool supports it. Delete only resources
created by this run and confirm their final cleanup state even after failures.

The remaining preview overlay has a separate strict live report, following
[PATCH-VERIFICATION.md](../../PATCH-VERIFICATION.md). A passing overlay report is
not a substitute for the independent canonical mount/security acceptance above.
