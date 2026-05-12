# AGS CLI DX Audit Issues

Date: 2026-05-12
Auditor role: DX Owner
Workspace: `/Users/yaominxia/Documents/task/update-ags-cli/ags-cli`
Binary tested: `/tmp/ags-dx-audit` built from current `main`

## Verification Matrix

- `go test ./...`: pass
- `make test`: pass
- `go vet ./...` / `make lint`: pass
- `golangci-lint run ./...`: pass, 0 issues
- `gitleaks detect --no-git --source . --redact`: pass, no leaks
- `go build -o /tmp/ags-dx-audit .`: pass
- `ags docs markdown`: pass, generated 44 files
- `ags docs man`: pass, generated 44 man pages
- Shell completions for bash/zsh/fish/powershell: generate successfully

## DX-001: Running `ags` without a TTY panics

Severity: critical

Reproduction:

```bash
tmp=$(mktemp -d)
env -i HOME=$tmp PATH=$PATH /tmp/ags-dx-audit </dev/null
```

Observed:

- Prints REPL banner, then panics with `panic: device not configured`.
- Exit code is `2`.
- Stack trace points to `github.com/c-bata/go-prompt.NewStandardInputParser()` via `internal/repl.Start()`.

Expected:

- Non-interactive invocation should never panic.
- Either show help, return a concise "interactive mode requires a TTY" error, or support stdin-driven commands.

Impact:

- Breaks CI, scripts, smoke tests, package post-install checks, and any environment that invokes `ags` with stdin closed.
- A panic stack trace is a poor first-run experience.

Likely fix:

- In root execution, detect `term.IsTerminal(os.Stdin.Fd())` before starting REPL.
- Set `SilenceUsage/SilenceErrors` appropriately and return a normal error.

## DX-002: E2B API key passes validation but `run` / `exec` / `file` create sandboxes through cloud credentials

Severity: critical

Reproduction:

```bash
tmp=$(mktemp -d)
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit run -c 'print(1)'
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit exec echo hi
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit file list /tmp
```

Observed:

- `config.Validate()` accepts the E2B API key.
- Sandbox creation fails with Tencent Cloud SDK errors, for example `AuthFailure.SignatureFailure`.
- The user is told about Tencent Cloud signing even though they configured E2B.

Expected:

- E2B backend data-plane creation should use the configured E2B/API-key auth path, or the CLI should clearly state that these commands require cloud credentials.
- Docs and validation should match actual behavior.

Impact:

- Default config (`backend = "e2b"`) cannot deliver the README quick-start path for `ags run`, `ags exec`, and temporary-instance file operations.
- Users will churn on the wrong credential system.

Code evidence:

- `cmd/run.go:getCreateOptions()` always creates `common.NewCredential(cloudCfg.SecretID, cloudCfg.SecretKey)`.
- The E2B key is validated in `internal/config/config.go`, but not passed to `ags-go-sdk` create options here.

## DX-003: Errors print full usage for runtime failures

Severity: high

Reproduction:

```bash
tmp=$(mktemp -d)
env -i HOME=$tmp PATH=$PATH /tmp/ags-dx-audit instance list
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit proxy sandbox 3000:8080 --address 0.0.0.0
env -i HOME=$tmp PATH=$PATH /tmp/ags-dx-audit --output json tool create --name n --type invalid
```

Observed:

- Runtime/auth/network/validation errors are followed by a full command usage block.
- JSON mode still emits plain text error and usage.

Expected:

- Usage should appear for syntax/arity errors, not for operational failures.
- JSON output mode should emit structured error JSON consistently.

Impact:

- Scripts cannot reliably parse errors.
- Human users have to scroll past dozens of lines to find the actual problem.

Likely fix:

- Set `SilenceUsage: true` for command trees after parse succeeds.
- Route errors through a shared formatter that honors `--output json`.

## DX-004: Invalid global config can be bypassed by commands that do not call `config.Validate()`

Severity: high

Reproduction:

```bash
tmp=$(mktemp -d)
env -i HOME=$tmp PATH=$PATH /tmp/ags-dx-audit --backend bad tool list
env -i HOME=$tmp PATH=$PATH /tmp/ags-dx-audit --output yaml tool list
```

Observed:

- `--backend bad` falls through to the E2B client and reports "tool operations are not supported by E2B backend".
- `--output yaml` is not rejected before command execution.

Expected:

- Invalid `--backend` and `--output` should fail immediately with a config validation error.

Impact:

- Misconfiguration produces misleading downstream errors.
- Users debug the wrong subsystem.

Code evidence:

- `internal/client/interface.go:NewControlPlaneClient` defaults unknown backend values to E2B.
- Tool commands create a client without first calling `config.Validate()`.

## DX-005: `mobile adb --help` cannot show help when `adb` is missing

Severity: high

Reproduction:

```bash
tmp=$(mktemp -d)
env -i HOME=$tmp PATH=/usr/bin:/bin /tmp/ags-dx-audit mobile adb --help
```

Observed:

- Returns `adb not found in PATH; install Android SDK Platform-Tools or set ADB_PATH`.
- Exit code is `1`.
- Help text is not displayed as help.

Expected:

- `--help` must work without external dependencies.

Impact:

- Users missing `adb` cannot read the command's built-in help to learn prerequisites or syntax.

Code evidence:

- `cmd/mobile.go` sets `DisableFlagParsing: true` on `mobile adb`, so Cobra does not handle `--help`.

## DX-006: Validation happens after network side effects for several local-only parameter errors

Severity: high

Reproduction:

```bash
tmp=$(mktemp -d)
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit run --repeat 0 -c x
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit run --repeat -1 -c x
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit run --max-parallel -1 --parallel -c x
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit run --language ruby -c x
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit exec --env BAD echo hi
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit instance create --timeout -1 --tool t
```

Observed:

- Commands attempt network/sandbox creation before rejecting invalid local inputs.
- Some invalid values are silently normalized, such as `--repeat 0` behaving like 1 and negative `--max-parallel` behaving as unlimited.

Expected:

- Validate local flags before credentials, network calls, or sandbox creation.
- Reject unsupported language, repeat values below 1, negative max parallel, malformed env vars, and negative timeouts locally.

Impact:

- Faster feedback is lost.
- Users see auth/network errors instead of the typo they can fix immediately.

## DX-007: `mobile tunnel --port -1` starts a listener before failing auth

Severity: medium

Reproduction:

```bash
tmp=$(mktemp -d)
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit mobile tunnel sandbox --port -1
```

Observed:

- Logs `ADB Tunnel listening on 127.0.0.1:<random>` even though `--port -1` is invalid.
- Then fails during upstream token probe and exits with code `2`.

Expected:

- Negative ports should be rejected before opening a listener.
- Auth/token readiness should be checked before claiming the tunnel is listening, or the status message should distinguish local bind from usable tunnel.

Impact:

- Misleading readiness logs.
- Potential short-lived local port side effect on invalid input.

## DX-008: `mobile disconnect --all sandbox` ignores the sandbox argument

Severity: medium

Reproduction:

```bash
tmp=$(mktemp -d)
env -i HOME=$tmp PATH=$PATH /tmp/ags-dx-audit mobile disconnect --all sandbox
```

Observed:

- Returns success with `no active connections`.
- The extra `sandbox` argument is ignored.

Expected:

- `--all` should be mutually exclusive with a sandbox ID.

Impact:

- A mistyped command can disconnect more than the user intended when active tunnels exist.

## DX-009: Config parse failures are warnings and the command continues with defaults

Severity: medium

Reproduction:

```bash
tmp=$(mktemp -d)
mkdir -p "$tmp/.ags"
printf 'not = [toml\n' > "$tmp/.ags/config.toml"
env -i HOME=$tmp PATH=$PATH /tmp/ags-dx-audit instance list
```

Observed:

- Prints `Warning: failed to load config...`
- Continues with defaults and then reports a missing E2B API key.

Expected:

- Malformed explicitly discovered config should be fatal for commands that depend on config.

Impact:

- The actionable root cause is demoted to a warning and followed by a misleading credential error.

## DX-010: Credential-bearing config file permissions are not checked

Severity: medium

Reproduction:

```bash
tmp=$(mktemp -d)
mkdir -p "$tmp/.ags"
printf 'backend = "e2b"\n[e2b]\napi_key="fake"\n' > "$tmp/.ags/config.toml"
chmod 0644 "$tmp/.ags/config.toml"
env -i HOME=$tmp PATH=$PATH /tmp/ags-dx-audit instance list
```

Observed:

- CLI reads the config with no warning.
- `~/.ags/config.toml` can be group/world-readable.

Expected:

- Warn or fail when config contains credentials and is readable beyond the owner.

Impact:

- Users can unknowingly expose API keys or cloud secrets.

Contrast:

- Token and tunnel stores write files with `0600`; config deserves the same DX guardrail.

## DX-011: README quick start recommends `ags tool list` under the default E2B setup

Severity: medium

Evidence:

- README config example defaults to `backend = "e2b"` with `AGS_E2B_API_KEY`.
- Quick Start then shows `ags tool list`.
- Runtime says tool operations are not supported by E2B and require cloud backend.

Expected:

- Quick Start should either use `ags instance list` / `ags run`, or explicitly switch to `ags --backend cloud tool list` with cloud credentials.

Impact:

- First documented command after setup fails for default users.

## DX-012: Generated docs include shell completion pages but omit the hidden `docs` command

Severity: low

Reproduction:

```bash
tmp=$(mktemp -d)
env -i HOME=$tmp PATH=$PATH /tmp/ags-dx-audit docs markdown -o "$tmp/md"
ls "$tmp/md"
```

Observed:

- Generated docs include `ags_completion*.md`.
- Generated docs omit `ags_docs*.md` because `docs` is hidden.

Expected:

- Decide whether developer-only commands are included consistently.
- If `docs` is intentionally hidden, generated public docs should probably also hide completion only if it is not part of user docs, or README should link the generated command docs source of truth.

Impact:

- Documentation generation is useful, but command visibility policy is inconsistent.

## DX-013: Cloud SDK and E2B REST errors leak low-level provider responses directly

Severity: medium

Reproduction:

```bash
tmp=$(mktemp -d)
env -i HOME=$tmp PATH=$PATH AGS_E2B_API_KEY=fake /tmp/ags-dx-audit instance create -t code-interpreter-v1
env -i HOME=$tmp PATH=$PATH AGS_CLOUD_SECRET_ID=sid AGS_CLOUD_SECRET_KEY=key /tmp/ags-dx-audit --backend cloud tool list
```

Observed:

- E2B path shows raw `401 Unauthorized - {"code":401,"message":"[AuthFailure] Authentication failed: Invalid API key"}`.
- Cloud path shows raw `[TencentCloudSDKError] Code=AuthFailure... RequestId=...`.

Expected:

- Preserve request IDs, but wrap provider errors in actionable AGS CLI guidance: which credential was used, which backend was selected, and how to fix it.

Impact:

- Users see provider internals before they understand their local configuration.

## DX-014: Some command help labels are misleading about whether `--instance` is required

Severity: low

Evidence:

- `ags file --help` says `--instance string Instance ID to use (required)`.
- The same help also says `--tool-name` is used for temporary instance if `--instance` is not specified.
- Runtime supports temporary instances for file operations.

Expected:

- Help should say "existing instance ID; if omitted a temporary instance is created".

Impact:

- Users may think file operations require a pre-created instance when they do not.

## DX-015: Destructive commands do not advertise confirmation or dry-run behavior

Severity: low

Evidence:

- `ags instance delete <instance-id> [instance-id...]`
- `ags tool delete <tool-id> [tool-id...]`
- `ags apikey delete <key-id>`

Observed:

- Help does not mention confirmation, force flags, dry-run, or whether deletes are recoverable.

Expected:

- Destructive operations should clearly document whether they prompt, are immediate, support batch partial failure, and whether they can be undone.

Impact:

- Users cannot assess blast radius from help output alone.

