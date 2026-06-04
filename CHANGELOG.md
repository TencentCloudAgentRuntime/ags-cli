# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

## [0.6.0] - 2026-06-02

### Breaking Changes
- This release has no Breaking Changes.

### Features
- Tencent Cloud STS session tokens are now supported via `--token`,
  `TENCENTCLOUD_TOKEN`, and `[auth].token` for temporary AK/SK
  credentials. (Upstream-Issue: 15)
- `agr instance debug <tool-id>` creates a debug tool from an existing
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
- `--jq` expressions now operate on the `Data` payload directly instead
  of the wrapping envelope, consistent with `gh --jq` and `aws --query`
  behavior (e.g. `.InstanceId` instead of `.Data.InstanceId`).
  (Upstream-Issue: 10)
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
