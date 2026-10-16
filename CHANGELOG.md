# Changelog

All notable changes to this project will be documented in this file.

## [0.8.0-preview.1] - 2026-10-16

### Breaking Changes
- Move Session Space commands from `agr session-space ...` to
  `agr session space ...`, and schema IDs from `session-space.*` to
  `session.space.*`. The old paths and IDs have no aliases; update preview
  scripts to use the new paths.
- `agr volume-template create` now requires `--volume-name-pattern` (or
  `VolumeNamePattern` in `--request`), matching the server contract. Include
  `${reuse_key}` in the volume name and storage path patterns.
- Some failures of existing commands now return a different `Failure.Code`,
  `Failure.Kind`, or exit code. Update scripts that branch on these fields:
  - `ClientError.NetworkError` now has `Kind=network` instead of `error`, with a
    connectivity-oriented hint and an `agr explain` entry; the exit code stays 1.
  - `agr instance mobile connect` startup failures that returned
    `INTERNAL_ERROR` (exit 1) now keep the background tunnel's classification,
    such as `PORT_IN_USE` (exit 2), `TUNNEL_AUTH_FAILED` (exit 4),
    `TUNNEL_READY_TIMEOUT`, `TUNNEL_EXITED`, or the cloud API error code.
  - `agr instance login` without interactive mode returns `TTY_REQUIRED`
    instead of `CLI_ERROR`; the exit code stays 2.
  - `agr pre-cache-image-task get` without a complete selector returns
    `MISSING_REQUIRED_INPUT` instead of `INVALID_USAGE` or
    `MISSING_REQUIRED_FLAG`; the exit code stays 2.
- An unknown command with `--help -o json` now fails with `INVALID_USAGE` and
  exit code 2, consistent with text mode, instead of printing root help with
  exit code 0. Use `agr --help -o json` or `agr schema -o json` to list
  commands.

### Features
- Add human-readable text output for Session Space, Session, and Event
  commands, including details, tables, and pagination. `agr session get` now
  exposes `--num-recent-events` and `--after-timestamp`; its current API
  response has no event list, so use `agr session event list` to read history.
- Add Beta sandbox snapshot commands (`agr instance snapshot create`, `list`,
  and `delete`). `agr instance create` can now clone an existing instance with
  `--source-instance-id` or restore an ACTIVE disk-only snapshot into a new
  instance with `--snapshot-id`.
- Add `--pre-cache-image-id` to `agr pre-cache-image-task get`, as an
  alternative to the image digest, image, and registry type selector. Pre-cache
  task JSON output preserves `PreCacheImageId` from creation and the ID,
  source type, cached size, creation time, and last-used time from retrieval
  when returned by the service.
- `--debug` and `AGR_DEBUG=1` now also save full redacted debug logs to
  `~/.agr/logs/` and print the log path on exit; `--debug-log <path>` enables
  debug and appends to a chosen file. Failures keep their original cause, with
  known credential values redacted.
- Failures now include optional `Failure.Details` context, such as the stage,
  operation, path, endpoint, HTTP status, and request ID, for configuration,
  file, cloud API, proxy, and mobile tunnel errors. `agr schema -o json` now
  lists the global flags in `GlobalFlags`.

### Bug Fixes
- Pass the effective cloud STS token and explicit `--cloud-endpoint` to the
  background tunnel process in `agr instance mobile connect`, fixing lost
  `--token` credentials and ignored endpoint overrides.
- Fix token collisions in `agr credential secret get` and
  `agr credential oauth2 acquire`: use `--workload-identity-token` for the
  business WorkloadIdentityToken. The global `--token` flag now consistently
  supplies the cloud STS token, alongside `TENCENTCLOUD_TOKEN` and `auth.token`.
- Report a failure when `agr instance file download` cannot finish writing to
  stdout or to the local file, instead of reporting success.
- Report configuration file parse errors with the file path, line, and column.
- Report `RequiresAuth: true` and the authentication failure codes for
  `deployment get`, `deployment list`, `volume list`, and
  `volume-template list`, and Session/Space/Event read commands in
  `agr schema` and `agr explain`.
- Use the JSON Schema type `integer` instead of `int64` for Registry list
  pagination fields in `agr schema`.
- Limit `deployment proxy` and `instance proxy` failure diagnostics to the
  upstream origin unless `--verbose` is set, so request paths that carry
  credentials are not logged by default.

### Docs
- Add snapshot, restore, and clone workflows, the Session workflow, Registry
  commands, and debug log troubleshooting to README and README-zh, and update
  the installation examples to v0.7.0.
- Correct `agr instance delete` help to describe its TTY confirmation and
  `--yes` behavior.
- Base API patch verification requirements in CONTRIBUTING and
  PATCH-VERIFICATION on the impact of each change.

## [0.7.0] - 2026-10-16

### Breaking Changes
- Some failures of existing commands now return a different `Failure.Code`,
  `Failure.Kind`, or exit code. Update scripts that branch on these fields:
  - `ClientError.NetworkError` now has `Kind=network` instead of `error`, with a
    connectivity-oriented hint and an `agr explain` entry; the exit code stays 1.
  - `agr instance mobile connect` startup failures that returned
    `INTERNAL_ERROR` (exit 1) now keep the background tunnel's classification,
    such as `PORT_IN_USE` (exit 2), `TUNNEL_AUTH_FAILED` (exit 4),
    `TUNNEL_READY_TIMEOUT`, `TUNNEL_EXITED`, or the cloud API error code.
  - `agr instance login` without interactive mode returns `TTY_REQUIRED`
    instead of `CLI_ERROR`; the exit code stays 2.
  - `agr pre-cache-image-task get` without a complete selector returns
    `MISSING_REQUIRED_INPUT` instead of `INVALID_USAGE` or
    `MISSING_REQUIRED_FLAG`; the exit code stays 2.
- An unknown command with `--help -o json` now fails with `INVALID_USAGE` and
  exit code 2, consistent with text mode, instead of printing root help with
  exit code 0. Use `agr --help -o json` or `agr schema -o json` to list
  commands.

### Features
- Add Registry lifecycle commands for registries, records, versions, approval
  decisions, remote preview/sync, audit logs, and Skill package transfer URLs.
- Add Session commands for session spaces (`agr session space`), sessions, and
  event append/list, with filtering, pagination, structured event payloads,
  and human-readable text output.
- Add Beta sandbox snapshot commands (`agr instance snapshot create`, `list`,
  and `delete`). `agr instance create` can now clone an existing instance with
  `--source-instance-id` or restore an ACTIVE disk-only snapshot into a new
  instance with `--snapshot-id`.
- Add `--pre-cache-image-id` to `agr pre-cache-image-task get`, as an
  alternative to the image digest, image, and registry type selector.
- `--debug` and `AGR_DEBUG=1` now also save full redacted debug logs to
  `~/.agr/logs/` and print the log path on exit; `--debug-log <path>` enables
  debug and appends to a chosen file. Failures keep their original cause, with
  known credential values redacted.
- Failures now include optional `Failure.Details` context, such as the stage,
  operation, path, endpoint, HTTP status, and request ID, for configuration,
  file, cloud API, proxy, and mobile tunnel errors. `agr schema -o json` now
  lists the global flags in `GlobalFlags`.

### Bug Fixes
- Fix token collisions in `agr credential secret get` and
  `agr credential oauth2 acquire`: use `--workload-identity-token` for the
  business WorkloadIdentityToken. The global `--token` flag now consistently
  supplies the cloud STS token, alongside `TENCENTCLOUD_TOKEN` and `auth.token`.
- Report a failure when `agr instance file download` cannot finish writing to
  stdout or to the local file, instead of reporting success.
- Report configuration file parse errors with the file path, line, and column.
- Report `RequiresAuth: true` and the authentication failure codes for
  `deployment get` and `deployment list` in `agr schema` and `agr explain`.
- Limit `deployment proxy` and `instance proxy` failure diagnostics to the
  upstream origin unless `--verbose` is set, so request paths that carry
  credentials are not logged by default.

### Docs
- Add snapshot, restore, and clone workflows, the Session workflow, Registry
  commands, and debug log troubleshooting to README and README-zh, and update
  the installation examples to v0.7.0.
- Correct `agr instance delete` help to describe its TTY confirmation and
  `--yes` behavior.
- Base API patch verification requirements in CONTRIBUTING and
  PATCH-VERIFICATION on the impact of each change.

## [0.7.0-preview.1] - 2026-09-18

### Breaking Changes
- This release has no Breaking Changes.

### Features
- Add Session Space and Session lifecycle commands, including event append/list,
  filtering, pagination, and structured event payloads.
- Add Registry lifecycle commands for registries, records, versions, approval
  decisions, remote preview/sync, audit logs, and Skill package transfer URLs.
- Add Volume and VolumeTemplate lifecycle commands with COS and CFS storage
  support, plus direct and template-derived mounts for sandbox tools that are
  preserved when a tool is forked.

### Bug Fixes
- This release has no Bug Fixes.

### Docs
- This release has no documentation changes.

## [0.6.7] - 2026-09-18

### Breaking Changes
- This release has no Breaking Changes.

### Features
- Add `--computer-configuration` to Tool create, update, and fork workflows,
  with WAA and OSWorld configuration support preserved through get, list, and
  fork results.
- Add token pagination to `agr instance list` through `--max-results`,
  `--next-token`, and `--need-total-count`, with the returned cursor normalized
  as `Pagination.NextCursor`.
- Add isolated stable and preview release channels from the same source branch.
  `agr version -o json` now reports the binary channel, while stable releases
  continue to own `latest` and Homebrew distribution.

### Bug Fixes
- Correct machine-readable command schemas to use JSON Schema-style `array`
  and `integer` property types and keep API-backed request fields synchronized
  with their CLI flags.
- Correct Instance metadata help and examples to use the service-supported
  `Name` and `Value` fields.

### Docs
- Document the API patch verification process and the stable/preview
  contribution and release workflow.

## [0.6.6] - 2026-08-27

### Breaking Changes
- This release has no Breaking Changes.

### Features
- Add generated `agr deployment create`, `get`, `list`, `update`, and `delete`
  commands backed by the official AGS API metadata, with human-readable text
  output, complete JSON responses for scripts, list filters, and opt-in waiting
  for deletion.
- Add `agr deployment proxy` for local HTTP, SSE, and WebSocket debugging, with
  local-to-remote port mapping, lazy token refresh, and in-memory affinity
  session discovery, reuse, and rotation.

### Bug Fixes
- This release has no Bug Fixes.

### Docs
- Add practical Deployment creation, update, filtering, deletion, and local
  proxy guidance to README and README-zh.

## [0.6.5] - 2026-08-21

### Breaking Changes
- This release has no Breaking Changes.

### Features
- This release has no new Features.

### Bug Fixes
- Correct machine-readable command schemas so update operations are marked as
  mutations, workflow commands require authentication, custom image registries
  are accepted by pre-cache commands, and JSON schemas expose the same examples
  as command help.
- Make `agr instance debug` follow the shared instance-create lifecycle policy,
  return structured wait failures instead of polling known terminal states until
  timeout, and provide a retry hint after temporary-resource cleanup.
- Align `agr instance list --filters` status help with current persisted states
  and document the special filtering and pagination behavior of the derived
  `UNHEALTHY` status.

### Docs
- This release has no documentation changes.

## [0.6.4] - 2026-08-06

### Breaking Changes
- This release has no Breaking Changes.

### Features
- Add 17 workflow commands for managing workload identities, access tokens,
  credential providers, managed secrets, and OAuth2 authorization through the
  new `agr identity` and `agr credential` command groups.
- Add operation-aware `--wait` to Instance and Tool lifecycle commands,
  including deletion. Waiting now follows backend-aligned final outcomes,
  reports failed or preempted operations, and includes timeout diagnostics for
  unfamiliar statuses.
- Add `agr update` and a non-blocking background update check with a 24-hour
  cache. Interactive text commands show update notices without delaying normal
  command execution or contaminating JSON output.
- Show progress spinners while creating instances and tools in interactive text
  terminals, while keeping JSON, redirected, and non-interactive output clean.
- Add unified error classification with inline fix commands and
  `agr explain <CODE>` guidance for actionable failures.
- Refresh AGS API metadata and expose `instance pause --memory` and
  `instance resume --timeout`, together with expanded storage, tool type, and
  custom image registry help.

### Bug Fixes
- Install to `$HOME/.local/bin` when `/usr/local/bin` is not writable, honoring
  `INSTALL_DIR` and avoiding implicit `sudo` in non-root environments.
- Report the actual status returned by `instance pause` instead of always
  claiming that the instance is paused.

### Docs
- Document non-root installer fallback behavior and the corresponding `PATH`
  setup in README and README-zh.

## [0.6.3] - 2026-07-15

### Breaking Changes
- This release has no Breaking Changes.

### Features
- Add deletion safeguards to `agr instance delete` and `agr tool delete`:
  interactive terminals now show the target resources and request confirmation,
  `--dry-run` previews deletions, and `--yes`/`-y` skips confirmation. Existing
  non-interactive workflows continue without prompts.
- Add official Homebrew Tap support through
  `brew install TencentCloudAgentRuntime/tap/agr-cli`, with stable releases
  automatically triggering Formula validation and updates.
- Strengthen CI with GitHub Actions workflow linting, an aggregate `CI Gate`,
  and unit tests on both Linux and macOS.

### Bug Fixes
- Build fully static Linux release binaries to prevent `GLIBC_2.34 not found`
  failures on older distributions such as Tencent Linux and CentOS 7/8.
- Remove duplicate direct COS uploads from the release workflow and leave COS
  synchronization to the GitHub release webhook, preventing a successful
  GitHub Release from being reported as failed by a later duplicate upload.
- Wait for live lifecycle instances to reach `PAUSED` before resuming them,
  removing a pause/resume race from the lifecycle smoke tests.

### Docs
- Document the official Homebrew Tap installation command in README and
  README-zh.
- Document `CI Gate` and `gitleaks` as the required status checks in both
  contribution guides.

## [0.6.2] - 2026-06-12

### Breaking Changes
- This release has no Breaking Changes.

### Features
- Add ADB tunnel health detection with automatic recovery for mobile
  instances. Unreachable tunnels now enter a degraded state, `mobile list`
  reports the real tunnel status, and `mobile list --prune` can clean up
  unreachable tunnel entries.
- Add the official AGR CLI download source at `https://dl.tencentags.com/agr-cli`,
  with the one-line installer using this source by default and GitHub Releases
  available as a fallback via `AGR_DOWNLOAD_MIRROR=github`.
- Publish release artifacts to the official download source from the release
  workflow, including versioned artifacts and the `latest/install.sh` and
  `latest/VERSION` pointers.

### Bug Fixes
- Fix ADB tunnel hangs after network disruption, sandbox sleep/wake, or remote
  adbd restart by closing stale local TCP connections and forcing ADB to
  reconnect with a fresh protocol handshake.
- Sync `api/ags/v20250920/api.json` from the upstream tccli API definition.

### Docs
- Update README and README-zh installation instructions to use the official
  download source by default.

## [0.6.1] - 2026-06-05

### Breaking Changes
- This release has no Breaking Changes.

### Features
- This release has no new features.

### Bug Fixes
- Fix `agr instance debug` JSON flags (`--mount-options`, `--metadata`, `--custom-configuration`) now correctly support `@file` and `-` (stdin) as documented.
- Fix `agr instance debug` text output showing wrong `ToolID` (showed input flag value instead of the created debug tool ID).
- Fix `agr instance debug --tool-name` lookup using wrong API field; now uses `Filters` correctly.
- Fix `agr instance debug` invalid JSON input showing `internal error`; now shows `INVALID_JSON_FLAG` with a clear hint.
- Fix `agr instance login` with a non-existent instance ID showing `internal error` instead of `INSTANCE_NOT_FOUND`.
- Fix README manual install commands referencing wrong filename version (`agr-0.5.0-*` instead of `agr-0.6.1-*`).

### Docs
- Update `agr instance debug` examples in README to use `--tool-id`/`--tool-name` flags and remove non-existent `--debug-tool-name` flag.

## [0.6.0] - 2026-06-02

### Breaking Changes
- This release has no Breaking Changes.

### Features
- Tencent Cloud STS session tokens are now supported via `--token`,
  `TENCENTCLOUD_TOKEN`, and `[auth].token` for temporary AK/SK
  credentials. (Upstream-Issue: 15)
- `agr instance debug --tool-id <id>` creates a debug tool from an existing
  sandbox tool by switching the startup command to `/envd` and adding
  the envd image mount. (Upstream-Issue: 3)
- `agr tool fork <source-tool-id> --tool-name <new-tool-name>` creates a
  new tool by copying create-capable settings from an existing tool, with
  create-like flags available for explicit overrides. (Upstream-Issue: 5)
- `agr instance list --all` fetches all instance-list pages for the
  current configured region and includes the region in aggregated
  output. (Upstream-Issue: 4)
- Normal command help now includes Format, Values, and Examples for
  complex flags such as `--filters`, `--tool-ids`, `--instance-ids`,
  `--network-configuration`, `--tags`, and `--storage-mounts`.
  (Upstream-Issue: 7)
- Command help now renders grouped `Example - ...:` sections across
  public leaf commands, and `agr schema` exposes command examples plus
  complex flag format/examples/values metadata. (Upstream-Issue: 46)
- Per-flag detailed help is available via `--<flag> --help`, exposed in
  both text and `-o json` output and in `agr schema` JSON.

### Bug Fixes
- Service-side TencentCloud errors now include `RequestId` alongside
  `Code` and `Message` in CLI error output, and text-mode error output
  renders `Error`, `Code`, and `RequestId` on dedicated, uniformly
  labeled lines (instead of the previous `Error: <message> (<code>)`
  format), so all three service-side identifiers needed for a
  TencentCloud support handoff are easy to read and copy-paste.
- `agr instance login` now reports PTY/envd data-plane session failures
  explicitly instead of collapsing them into a generic internal error,
  and no longer treats a remote shell exit as a CLI failure: when the
  user types `exit` the command now propagates the remote shell's exit
  code as the CLI process exit status without rendering an error
  envelope, mirroring `ssh` behavior. (Upstream-Issue: 13)
- `agr version` now extracts commit hash and build timestamp from Go
  module pseudo-versions when VCS build info is unavailable (e.g.
  binaries installed via `go install @latest`), and prints
  `n/a (go install)` instead of the bare literal `unknown` for `commit`
  and `built` when the binary was produced by
  `go install <module>@<tag>` (Go does not stamp VCS metadata for
  module-cache builds). Pseudo-version installs and ldflags-stamped
  release binaries are unchanged. (Upstream-Issue: 6)
- Live test binary builds now disable Go VCS stamping so race-test gate
  runs do not fail in worktrees where Git metadata cannot be inspected.
- CI gofmt and changelog-gate failures resolved by formatting
  `internal/cli/flaghelp.go` and changing the `## [Unreleased]` heading
  to `## Unreleased` so it does not trip the version-heading regex.
- `agr instance file upload` and `agr instance file download` now emit a
  command-specific hint when an unknown flag (such as `--source` or
  `--target`) is passed, pointing at the positional `<instance-id>
  <local-path|-> <remote-path>` form instead of the generic
  `agr --help` pointer. (Upstream-Issue: 20)

### Docs
- `make go-install` target installs the binary to `$GOPATH/bin` with
  full version metadata injected via ldflags; README and README-zh
  installation docs updated to recommend pre-built binaries.

## [0.5.1] - 2026-05-29

### Breaking Changes
- This release has no Breaking Changes.

### Features
- Add a one-line installer script (`install.sh`) and include it in release
  artifacts.

### Bug Fixes
- This release has no Bug Fixes.

### Docs
- Add per-platform installation instructions for Linux, macOS, and Windows.

## [0.5.0] - 2026-05-22

### Breaking Changes
- 0.5.0 is not backward-compatible with 0.4.0. Command layout, flag
  sets, request input handling, and machine-readable output all change
  significantly in this release.
- Existing shell scripts, wrappers, CI jobs, and any automation that
  calls `agr` should be reviewed before upgrading.
- If you consume JSON or NDJSON output, revalidate parsers against the
  new `agr.v1` / `agr.events.v1` contracts.

### Features
- The CLI is reorganized around a clearer resource-oriented command
  tree, with more consistent help text and machine-readable command
  metadata.
- New support commands are available for day-to-day use:
  `agr schema`, `agr doctor`, `agr explain`, and
  `agr config {show,set,path}`.
- `agr instance code run` and `agr instance exec` now support temporary
  sandbox workflows with `--create-temp-instance`, cleanup policy
  control, and `Data.ExecutionContext` in JSON output.
- `agr api call <Action> --request ...` provides a raw control-plane
  escape hatch for advanced debugging and unsupported operations.
- Control-plane endpoint selection (`--cloud-endpoint`) and data-plane
  domain selection (`--domain`) are now clearly separated.
- JSON output and streaming behavior are more consistent across the CLI,
  including richer failure metadata and execution context reporting.

### Bug Fixes
- This release has no Bug Fixes.

### Docs
- Refresh the README and changelog to document the `agr` command
  surface, the `0.5.0` migration, and the updated quick-start guidance.
- Use `agr --help` and `agr schema <command> -o json` as the primary
  references when updating existing automation to the new surface.

## [0.4.0] - 2026-04-28

### Added
- Surface a backend-agnostic `Secure` flag on the `Instance` type (Cloud: `Secure = AuthMode != "NONE"`; E2B: `Secure = envdAccessToken != ""`); `ags instance login` now skips access-token acquisition and omits the `X-Access-Token` header / webshell `access_token` query parameter when the instance is not secure, and `ags instance create` no longer fails to cache a token for such instances
- Add `--auth-mode` flag to `ags instance create` / `ags instance start` accepting `DEFAULT`, `TOKEN`, `NONE`, `PUBLIC`; cloud backend passes it through as `AuthMode`, while E2B backend translates it into the `secure` + `network.allowPublicTraffic` request fields

### Changed
- Upgrade Tencent Cloud SDK (`tencentcloud-sdk-go/tencentcloud/ags` and `common`) to v1.3.87 to pick up the new `AuthMode` field on sandbox instances

### Fixed
- Fix `mobile connect` showing generic "tunnel process exited without ready message" instead of the actual error; daemon subprocess now sends error details via stdout so the parent process can display them to the user

## [0.3.1] - 2026-03-18

### Fixed
- Redirect tunnel subprocess stderr to `~/.ags/tunnel-<id>.log` instead of parent terminal, preventing background reconnection logs from polluting the user's shell
- Add max consecutive dial failure limit to stop infinite reconnection when sandbox is deleted or token expired
- Disconnect old ADB address before cleanup when reconnecting the same sandbox, preventing stale offline devices
- Wait for ADB protocol handshake to complete after `adb connect`, avoiding "error: closed" on the first user command
- Remove TCP port probe from `mobile list` to prevent preemption of active ADB sessions; use PID-based zombie detection instead

## [0.3.0] - 2026-03-17

### Added
- Add `mobile` command group (`ags mobile`) with `connect`, `disconnect`, `list`, `adb`, and `tunnel` subcommands for secure ADB access to remote Android sandboxes through encrypted WebSocket tunnels
- Add `--mode` flag to `instance login` command with `pty` (default) and `webshell` modes; PTY mode connects a native terminal session directly in the current console without requiring a browser or ttyd binary
- Add mobile ADB command documentation in both English and Chinese

### Fixed
- Fix `instance create --tool-id` not being passed to Cloud backend API; ToolID is now preferred over ToolName when specified

## [0.2.1] - 2026-03-13

### Changed
- Expand supported tool types from `code-interpreter` and `browser` to also include `mobile`, `osworld`, `custom`, and `swebench`

## [0.2.0] - 2026-03-09

### Added
- Add `--user` flag to `exec`, `file`, and `instance login` commands to specify the user identity for data plane operations (default: "user")
- Add `sandbox.default_user` configuration option in config.toml for setting the default user globally
- Add unified top-level `region`, `domain`, and `internal` configuration fields to replace backend-specific duplicates
- Add `--region`, `--domain`, and `--internal` global CLI flags
- Add `AGS_REGION`, `AGS_DOMAIN`, and `AGS_INTERNAL` environment variables
- Add dedicated configuration reference documentation (`docs/ags-config.md`)

### Changed
- Unify region/domain/internal configuration: all data plane and control plane operations now read from top-level config fields instead of backend-specific `[e2b]` or `[cloud]` sections
- Control plane clients (`CloudControlPlane`, `E2BControlPlane`) now use unified config for region and domain
- Normalize `internal` flag into `domain` at config resolution time: when `internal=true`, the domain is automatically prefixed with `internal.` (e.g., `internal.tencentags.com`), ensuring consistent endpoint construction for both E2B and Cloud backends

### Deprecated
- Config fields `e2b.region`, `e2b.domain`, `cloud.region`, `cloud.internal` are deprecated in favor of top-level `region`, `domain`, `internal`
- CLI flags `--e2b-region`, `--e2b-domain`, `--cloud-region`, `--cloud-internal` are deprecated in favor of `--region`, `--domain`, `--internal`
- Environment variables `AGS_E2B_REGION`, `AGS_E2B_DOMAIN`, `AGS_CLOUD_REGION`, `AGS_CLOUD_INTERNAL` are deprecated in favor of `AGS_REGION`, `AGS_DOMAIN`, `AGS_INTERNAL`

## [0.1.2] - 2026-02-11

### Changed
- E2B backend now supports token acquisition via GET /sandboxes/{id}, removing the limitation that tokens were only available at instance creation time
- Unified token recovery logic for both Cloud and E2B backends when token cache is missing

## [0.1.1] - 2026-01-20

### Changed
- Separate control plane and data plane with token caching

## [0.1.0] - 2026-01-16

### Added
- Initial release
- Update module path to github.com/TencentCloudAgentRuntime/ags-cli
- Replace all git.woa.com references with github.com/TencentCloudAgentRuntime/ags-go-sdk v0.0.10
