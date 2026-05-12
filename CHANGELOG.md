# Changelog

All notable changes to this project will be documented in this file.

## [0.5.0] - 2026-05-12

### Breaking Changes
- **Exit codes now reflect remote execution failures**: `ags run` previously returned exit code 0 even when remote code execution failed; it now returns exit code 1. Multi-task mode returns exit code 1 for partial failures, exit code 2 when all tasks fail. `ags exec` also returns proper exit codes via `exitCodeError` instead of calling `os.Exit()` directly (which bypassed deferred cleanup).
- **stderr/stdout separation**: Remote stderr output, error tracebacks, and file operation diagnostics (e.g. `✓ Uploaded ...`) are now written to local **stderr** instead of stdout. Scripts that parsed stdout for these messages will need updating. This means `ags run ... | jq .` now works correctly without non-JSON noise on stdout.
- **Invalid `--backend` values are now rejected**: Previously, an unrecognized `--backend` value (e.g. `--backend foo`) silently fell back to E2B. It now returns an error: `invalid backend: foo (must be 'e2b' or 'cloud')`.
- **Invalid `--output` values are now rejected early**: `--output yaml` or any value other than `text`/`json` is rejected before any command runs, via `ValidateBasics()` in `PersistentPreRunE`.
- **Non-TTY stdin no longer enters REPL**: Piping input to `ags` (e.g. `echo test | ags`) now prints help and exits instead of entering REPL mode (which previously caused a `go-prompt` panic).
- **`SilenceUsage` / `SilenceErrors` enabled on root command**: Runtime errors no longer print the full usage/help text — only the error message is shown to stderr.

### Added
- `E2B_API_KEY` environment variable is now recognized alongside `AGS_E2B_API_KEY` via `viper.BindEnv` for compatibility with E2B tooling
- Config file permission warning: a stderr warning is printed when `~/.ags/config.toml` contains credentials and is readable by group or others (recommends `chmod 600`)
- Input validation before network calls: `run` validates `--language`, `--repeat`, `--max-parallel`, `--instance`/`--tool` mutual exclusivity; `mobile` validates port range [0–65535] and `--all`/device mutual exclusivity; `browser` validates `--timeout > 0`; `instance create` validates `--timeout > 0`
- New `cmd/sandbox_helper.go`: `GetOrCreateSandboxForDataPlane()` provides a unified path for all data-plane commands (`run`, `exec`, `file`) to create temporary instances through the control-plane client, consistent with `ags instance create`
- Unified token-caching connection layer: `ConnectWithToken()` and `ConnectSandboxWithCache()` ensure tokens are cached and reused for data-plane operations
- Richer E2B error formatting: `e2bHTTPError()` parses JSON error payloads with code/message fields instead of dumping raw HTTP bodies
- Tests: `cmd/run_test.go` (5 cases for `validateRunFlags`), `internal/client/interface_test.go` (compile-time interface conformance), `internal/config/config_test.go` (4 cases for `ValidateBasics`, 5 cases for `configFileContainsCredentials`)

### Changed
- Data-plane commands (`run`, `exec`, `file`) now create temporary instances through the configured control-plane backend instead of directly calling the cloud SDK, fixing an issue where `ags run -c "print('hello')"` on E2B backend accidentally hit the Cloud API with a signature failure
- `file list --depth` flag is now actually passed to the SDK's `filesystem.ListConfig{Depth: ...}` (was declared but silently ignored)
- `instance list --limit/--offset` on E2B backend now applies client-side truncation since the E2B API does not support server-side pagination
- Help text: root command example changed from `ags tool list` (fails on default E2B backend) to `ags --backend cloud tool list`
- Documentation fixes: `docs/ags-file.md` / `docs/ags-file-zh.md` flag description corrected; `docs/ags.md` / `docs/ags-zh.md` examples updated; `docs/ags-config.md` / `docs/ags-config-zh.md` now document `sandbox.default_user` and `E2B_API_KEY`
- Destructive commands (`apikey delete`, `instance delete/stop`, `tool delete`) now document in `--help` that they execute immediately without confirmation

## [0.5.0] - 2026-05-12

### Breaking Changes
- **Exit codes now reflect remote execution failures**: `ags run` previously returned exit code 0 even when remote code execution failed; it now returns exit code 1. Multi-task mode returns exit code 1 for partial failures, exit code 2 when all tasks fail. `ags exec` also returns proper exit codes via `exitCodeError` instead of calling `os.Exit()` directly (which bypassed deferred cleanup).
- **stderr/stdout separation**: Remote stderr output, error tracebacks, and file operation diagnostics (e.g. `✓ Uploaded ...`) are now written to local **stderr** instead of stdout. Scripts that parsed stdout for these messages will need updating. This means `ags run ... | jq .` now works correctly without non-JSON noise on stdout.
- **Invalid `--backend` values are now rejected**: Previously, an unrecognized `--backend` value (e.g. `--backend foo`) silently fell back to E2B. It now returns an error: `invalid backend: foo (must be 'e2b' or 'cloud')`.
- **Invalid `--output` values are now rejected early**: `--output yaml` or any value other than `text`/`json` is rejected before any command runs, via `ValidateBasics()` in `PersistentPreRunE`.
- **Non-TTY stdin no longer enters REPL**: Piping input to `ags` (e.g. `echo test | ags`) now prints help and exits instead of entering REPL mode (which previously caused a `go-prompt` panic).
- **`SilenceUsage` / `SilenceErrors` enabled on root command**: Runtime errors no longer print the full usage/help text — only the error message is shown to stderr.

### Added
- `E2B_API_KEY` environment variable is now recognized alongside `AGS_E2B_API_KEY` via `viper.BindEnv` for compatibility with E2B tooling
- Config file permission warning: a stderr warning is printed when `~/.ags/config.toml` contains credentials and is readable by group or others (recommends `chmod 600`)
- Input validation before network calls: `run` validates `--language`, `--repeat`, `--max-parallel`, `--instance`/`--tool` mutual exclusivity; `mobile` validates port range [0–65535] and `--all`/device mutual exclusivity; `browser` validates `--timeout > 0`; `instance create` validates `--timeout > 0`
- New `cmd/sandbox_helper.go`: `GetOrCreateSandboxForDataPlane()` provides a unified path for all data-plane commands (`run`, `exec`, `file`) to create temporary instances through the control-plane client, consistent with `ags instance create`
- Unified token-caching connection layer: `ConnectWithToken()` and `ConnectSandboxWithCache()` ensure tokens are cached and reused for data-plane operations
- Richer E2B error formatting: `e2bHTTPError()` parses JSON error payloads with code/message fields instead of dumping raw HTTP bodies
- Tests: `cmd/run_test.go` (5 cases for `validateRunFlags`), `internal/client/interface_test.go` (compile-time interface conformance), `internal/config/config_test.go` (4 cases for `ValidateBasics`, 5 cases for `configFileContainsCredentials`)

### Changed
- Data-plane commands (`run`, `exec`, `file`) now create temporary instances through the configured control-plane backend instead of directly calling the cloud SDK, fixing an issue where `ags run -c "print('hello')"` on E2B backend accidentally hit the Cloud API with a signature failure
- `file list --depth` flag is now actually passed to the SDK's `filesystem.ListConfig{Depth: ...}` (was declared but silently ignored)
- `instance list --limit/--offset` on E2B backend now applies client-side truncation since the E2B API does not support server-side pagination
- Help text: root command example changed from `ags tool list` (fails on default E2B backend) to `ags --backend cloud tool list`
- Documentation fixes: `docs/ags-file.md` / `docs/ags-file-zh.md` flag description corrected; `docs/ags.md` / `docs/ags-zh.md` examples updated; `docs/ags-config.md` / `docs/ags-config-zh.md` now document `sandbox.default_user` and `E2B_API_KEY`
- Destructive commands (`apikey delete`, `instance delete/stop`, `tool delete`) now document in `--help` that they execute immediately without confirmation

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
