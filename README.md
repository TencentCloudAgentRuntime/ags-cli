# AGR CLI

[中文文档](README-zh.md)

AGR CLI manages Tencent Cloud Agent Runtime instances, tools, API keys, and data-plane operations from the `agr` command.

## Installation

### One-line install (macOS / Linux)

```bash
curl -fsSL https://github.com/TencentCloudAgentRuntime/ags-cli/releases/latest/download/install.sh | sh
```

### Manual binary download

Download the latest release from [GitHub Releases](https://github.com/TencentCloudAgentRuntime/ags-cli/releases) and install manually.

**macOS (Apple Silicon)**

```bash
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v0.5.0/agr-0.5.0-darwin-arm64.tar.gz
tar xzf agr-0.5.0-darwin-arm64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-0.5.0-darwin-arm64.tar.gz
```

**macOS (Intel)**

```bash
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v0.5.0/agr-0.5.0-darwin-amd64.tar.gz
tar xzf agr-0.5.0-darwin-amd64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-0.5.0-darwin-amd64.tar.gz
```

**Linux (x86_64)**

```bash
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v0.5.0/agr-0.5.0-linux-amd64.tar.gz
tar xzf agr-0.5.0-linux-amd64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-0.5.0-linux-amd64.tar.gz
```

**Linux (ARM64)**

```bash
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v0.5.0/agr-0.5.0-linux-arm64.tar.gz
tar xzf agr-0.5.0-linux-arm64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-0.5.0-linux-arm64.tar.gz
```

**Windows (x86_64) — PowerShell**

```powershell
Invoke-WebRequest -Uri https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v0.5.0/agr-0.5.0-windows-amd64.zip -OutFile agr-0.5.0-windows-amd64.zip
Expand-Archive agr-0.5.0-windows-amd64.zip -DestinationPath .
Move-Item agr.exe "$env:USERPROFILE\bin\agr.exe"
Remove-Item agr-0.5.0-windows-amd64.zip
```

> Make sure `$env:USERPROFILE\bin` is in your `PATH`.

### Verify checksums

```bash
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v0.5.0/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
```

### From source

```bash
git clone https://github.com/TencentCloudAgentRuntime/ags-cli.git
cd ags-cli
make build
sudo cp agr /usr/local/bin/agr
```

Or install to `$GOPATH/bin` with version metadata:

```bash
make go-install
```

### Using `go install`

```bash
go install github.com/TencentCloudAgentRuntime/ags-cli/cmd/agr@latest
```

### Verify installation

```bash
agr version
```

The installed command name is `agr`.

> **Note:** Binaries installed via `go install @tag` show
> `commit: n/a (go install)` and `built: n/a (go install)` in `agr version`
> output — Go does not stamp VCS metadata for module-cache builds. Use
> `make build` or download a pre-built release binary for the full commit
> hash and build timestamp.

## Prerequisites

1. A [Tencent Cloud](https://cloud.tencent.com/) account
2. AGR (Agent Runtime) service enabled
3. API credentials (SecretID / SecretKey) — obtain long-term credentials from [CAM Console](https://console.cloud.tencent.com/cam/capi), or use temporary STS credentials with a session token.

## Initialize CLI credentials

```bash
export TENCENTCLOUD_SECRET_ID="your-secret-id"
export TENCENTCLOUD_SECRET_KEY="your-secret-key"

agr init \
  --secret-id "$TENCENTCLOUD_SECRET_ID" \
  --secret-key "$TENCENTCLOUD_SECRET_KEY"
```

`agr init` only writes local CLI configuration under `~/.agr/config.toml`; it does not create remote resources or modify the current project directory.

For temporary STS credentials, provide the full TmpSecretId, TmpSecretKey,
and token triplet. Prefer environment variables in CI/CD so the short-lived
token is not written to disk:

```bash
export TENCENTCLOUD_SECRET_ID="tmp-secret-id"
export TENCENTCLOUD_SECRET_KEY="tmp-secret-key"
export TENCENTCLOUD_TOKEN="tmp-session-token"
```

You can also pass `--token` on any command or persist it with
`agr config set token=<token>` / `agr init --token <token>`. Expired
tokens are reported as authentication failures; refresh the token outside
the CLI and retry.

## Quick Start

```bash
export TENCENTCLOUD_SECRET_ID="your-secret-id"
export TENCENTCLOUD_SECRET_KEY="your-secret-key"

agr init \
  --secret-id "$TENCENTCLOUD_SECRET_ID" \
  --secret-key "$TENCENTCLOUD_SECRET_KEY"

tool_name="quickstart-code-$(date +%s)-$$"
tool_id=$(agr tool create \
  --tool-name "$tool_name" \
  --tool-type code-interpreter \
  --network-configuration '{"NetworkMode":"SANDBOX"}' \
  -o json --jq '.Data.ToolId')

instance_id=$(agr instance create --tool-id "$tool_id" -o json --jq '.Data.InstanceId')
agr instance code run "$instance_id" -c "print('Hello, World!')"
agr instance delete "$instance_id" --ignore-not-found
agr tool delete "$tool_id" || true
```

The example creates a unique tool name first because tool names must be unique within the current AppId.

## Temporary sandbox workflow

`agr instance code run` and `agr instance exec` accept
`--create-temp-instance` to spin up a sandbox just for this single
execution, and clean it up automatically. The referenced tool must
already exist; create one first with `agr tool create`, then pass
`--tool-name` or `--tool-id`:

```bash
# Create a temporary instance, run a snippet, delete it always (cleanup=always is the default).
agr instance code run \
  --create-temp-instance \
  --tool-id "$tool_id" \
  -c "print('hello')"

# Same workflow, but keep the temporary instance for debugging.
agr instance exec \
  --create-temp-instance \
  --tool-id "$tool_id" \
  --cleanup never \
  -- python -V
```

`--cleanup` accepts `always` (default), `success`, or `never`. To keep
the temporary instance after the run, use `--cleanup never`. There is
no `--keep-temp-instance`.

The JSON output of these commands includes
`Data.ExecutionContext.SandboxInstanceId`,
`Data.ExecutionContext.TemporarySandboxInstance` and
`Data.ExecutionContext.Cleanup` so scripts can inspect the workflow.

## Debug instance creation

Use `agr instance debug <tool-id>` to create a debug instance for an existing
tool. The command creates a temporary debug tool that keeps the source tool
configuration, changes the startup command to `/envd`, mounts
`ccr.ccs.tencentyun.com/ags-image/envd:v0.5.14` from `/usr/bin/envd` to
`/envd`, waits for that tool to be ready, then starts an instance from it.
The source tool must have `RoleArn` configured because image storage mounts
require it. `--timeout` controls the created instance lifetime and defaults
to `1h`; readiness waits use the command's internal workflow timeout.

```bash
debug_instance_id=$(agr instance debug "$tool_id" \
  --debug-tool-name "my-tool-debug" \
  --timeout 30m \
  -o json --jq '.Data.InstanceId')
```

## Cloud endpoint vs data-plane domain

| Flag                         | Default                  | Controls                         |
|------------------------------|--------------------------|----------------------------------|
| `--cloud-endpoint`           | `ags.tencentcloudapi.com`| Control-plane API endpoint       |
| `--domain`                   | `tencentags.com`         | Data-plane domain (browser, exec)|

`--cloud-endpoint` affects every control-plane call (regular resource
commands and `agr api call`). `--domain` only affects data-plane access. Both can
also be set via `cloud_endpoint` / `domain` in `~/.agr/config.toml` or
`AGR_CLOUD_ENDPOINT` / `AGR_DOMAIN` environment variables.

## Low-level API access

For undocumented fields or debugging, use the raw API channel:

```bash
agr api call DescribeSandboxInstanceList --request '{"Limit":1}' -o json
agr api call StartSandboxInstance --request @start.json
agr api call StopSandboxInstance --request - < stop.json
```

## Command Overview

```text
agr                              Print help
agr init                         Initialize local CLI config and credentials
agr version                      Version info
agr status                       Current configuration status
agr schema [command]             Machine-readable command schema
agr doctor                       Diagnose configuration and connectivity
agr explain <CODE>               Explain errors and fixes

agr instance create              Create a new instance
agr instance list                List instances
agr instance get <id>            Get instance details
agr instance update <id>         Update timeout/metadata
agr instance pause <id>          Pause an instance
agr instance resume <id>         Resume an instance
agr instance delete <id>         Delete instance(s)
agr instance debug <tool-id>     Create a debug instance from a tool

agr instance code run <id>       Execute code in an existing instance
agr instance exec <id> -- CMD    Execute shell command in an existing instance
agr instance file upload <id>    Upload file to an existing instance
agr instance file download <id>  Download file from an existing instance
agr instance login <id>          PTY terminal session
agr instance browser vnc <id>    Show VNC URL
agr instance proxy <id> PORT     Forward instance port to localhost
agr instance mobile ...          Mobile ADB operations

agr tool list/create/fork/get/update/delete
agr apikey create/list/delete
agr pre-cache-image-task create|get
agr completion bash|zsh|fish|powershell
```

## Machine-readable output and `--jq`

Commands that support `-o json` return one `agr.v1` envelope on stdout:

```json
{
  "SchemaVersion": "agr.v1",
  "Command": "instance.create",
  "Status": "succeeded",
  "Data": { "InstanceId": "sandbox-xxx", "ToolName": "my-tool" },
  "Failure": null,
  "Warnings": [],
  "Meta": { "DurationMs": 123 }
}
```

Examples:

```bash
agr instance create --tool-id "$tool_id" -o json --jq '.Data.InstanceId'
agr instance list -o json --jq '.Data.Items[].InstanceId'
agr status -o json --jq '.Data.Region'
agr schema -o json --jq '.Data.ExitCodes'
```

`--jq` must be used with `-o json`.

## Streaming

Only `instance code run` and `instance exec` support machine-readable streaming:

```bash
agr instance code run "$id" -c "print(1)" --stream -o ndjson
agr instance exec "$id" --stream -o ndjson -- tail -f app.log
```

Each stdout line is one `agr.events.v1` JSON event.

## Exit Codes

| Exit | Kind | Description |
|---:|---|---|
| 0 | success | OK |
| 1 | error | Non-usage, non-auth CLI or API failure; inspect `Failure.Kind` for details |
| 2 | usage | Invalid args, flags, input, or unsupported output mode |
| 4 | auth | Missing credentials, authentication failure, or permission failure |
| 255 | remote_execution_failed | Remote code execution failure |

`instance exec` and `instance mobile adb` may also pass through downstream process exit codes in the range `0-255`.

See `agr schema -o json --jq '.Data.ExitCodes'` for the full list.

## Global Flags

```text
--config          Config file path (default: ~/.agr/config.toml)
-o, --output      Output format: text, json, or ndjson (`ndjson` only when explicitly passed to supported streaming commands)
--jq              jq expression (only with -o json)
--region          Tencent Cloud region (default: ap-guangzhou)
--cloud-endpoint  Control-plane API endpoint (default: ags.tencentcloudapi.com)
--domain          Data-plane domain (default: tencentags.com)
--secret-id       Tencent Cloud SecretID
--secret-key      Tencent Cloud SecretKey
--token           Tencent Cloud STS session token
--non-interactive Disable interactive behavior
--no-color        Disable ANSI color
--debug           Write debug diagnostics to stderr
```

Environment variables: `TENCENTCLOUD_SECRET_ID`, `TENCENTCLOUD_SECRET_KEY`, `TENCENTCLOUD_TOKEN`, `AGR_OUTPUT`, `AGR_REGION`, `AGR_CLOUD_ENDPOINT`, `AGR_DOMAIN`, `AGR_NON_INTERACTIVE`, `AGR_DEBUG`, `NO_COLOR`.

`AGR_OUTPUT` is intended for default `text` or `json` output. For streaming, pass `-o ndjson` explicitly with `agr instance code run --stream` or `agr instance exec --stream`.

Configuration priority: `--flag` > environment variable > `~/.agr/config.toml` > default. Use `agr status` to inspect resolved values and their sources.

## Troubleshooting

```bash
agr status
agr doctor
agr explain AUTH_FAILED
agr schema instance.create -o json
```

## License

See [LICENSE](LICENSE-AGR%20CLI.txt).
