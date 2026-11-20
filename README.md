# AGR CLI

[中文文档](README-zh.md)

AGR CLI manages Tencent Cloud Agent Runtime deployments, instances, tools, API keys, and data-plane operations from the `agr` command.

## Installation

### Homebrew (macOS / Linux)

```bash
brew install TencentCloudAgentRuntime/tap/agr-cli
```

### One-line install (macOS / Linux)

```bash
curl -fsSL https://dl.tencentags.com/agr-cli/latest/install.sh | sh
```

The installer uses `/usr/local/bin` when it is writable. In non-root
environments it falls back to `$HOME/.local/bin` without invoking `sudo`. If
the selected directory is not in `PATH`, the installer prints the command
needed to add it.

To choose another writable directory explicitly:

```bash
curl -fsSL https://dl.tencentags.com/agr-cli/latest/install.sh | INSTALL_DIR="$HOME/bin" sh
```

To install a specific version:

```bash
curl -fsSL https://dl.tencentags.com/agr-cli/latest/install.sh | VERSION=v0.7.2 sh
```

To use GitHub Releases as a fallback source:

```bash
curl -fsSL https://github.com/TencentCloudAgentRuntime/ags-cli/releases/latest/download/install.sh | AGR_DOWNLOAD_MIRROR=github sh
```

### Manual binary download

Download the latest release from [GitHub Releases](https://github.com/TencentCloudAgentRuntime/ags-cli/releases) and install manually.

Set `VERSION` to the release you want (e.g. `0.7.2`):

**macOS (Apple Silicon)**

```bash
VERSION=0.7.2
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-darwin-arm64.tar.gz
tar xzf agr-${VERSION}-darwin-arm64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-darwin-arm64.tar.gz
```

**macOS (Intel)**

```bash
VERSION=0.7.2
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-darwin-amd64.tar.gz
tar xzf agr-${VERSION}-darwin-amd64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-darwin-amd64.tar.gz
```

**Linux (x86_64)**

```bash
VERSION=0.7.2
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-linux-amd64.tar.gz
tar xzf agr-${VERSION}-linux-amd64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-linux-amd64.tar.gz
```

**Linux (ARM64)**

```bash
VERSION=0.7.2
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-linux-arm64.tar.gz
tar xzf agr-${VERSION}-linux-arm64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-linux-arm64.tar.gz
```

**Windows (x86_64) — PowerShell**

```powershell
$VERSION = "0.7.2"
Invoke-WebRequest -Uri "https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-windows-amd64.zip" -OutFile "agr-${VERSION}-windows-amd64.zip"
Expand-Archive "agr-${VERSION}-windows-amd64.zip" -DestinationPath .
Move-Item agr.exe "$env:USERPROFILE\bin\agr.exe"
Remove-Item "agr-${VERSION}-windows-amd64.zip"
```

> Make sure `$env:USERPROFILE\bin` is in your `PATH`.

### Verify checksums

```bash
VERSION=0.7.2
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/checksums.txt
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

## Sandbox keepalive and log format (preview)

These flags exist only in the preview build (`-tags=preview`). The stable
channel neither sends nor documents them.

A tool carries the default keepalive configuration; an instance overrides it
per member. Unset members inherit from the instance value, then the tool's
current value, then the platform default (`REQUEST` / `10m`). The CLI applies no
default of its own: an omitted flag sends nothing, an explicit `{}` sends an
empty override, and an explicit `--auto-pause=false` sends `false` rather than
being dropped as a zero value.

```bash
# Tool defaults: renew while requests are served, for 600 seconds.
agr tool create -n my-tool -t code-interpreter \
  --network-configuration '{"NetworkMode":"SANDBOX"}' \
  --keepalive-configuration '{"KeepaliveMode":"REQUEST","KeepaliveTimeout":"600s"}' \
  --auto-pause --auto-resume -o text

# Instance override: only the timeout, plus an explicit false.
agr instance create --tool-id sdt-xxxx --timeout 30m \
  --keepalive-configuration '{"KeepaliveTimeout":"5m"}' --auto-pause=false -o json

# Change the tool defaults later; instances that did not override follow them.
agr tool update sdt-xxxx --keepalive-configuration '{"KeepaliveMode":"MIXED","KeepaliveTimeout":"900s"}'
agr instance update ins-xxxx --auto-resume=false -o json

# Read back the stored values (instance > tool > platform default).
agr tool get sdt-xxxx -o json
agr instance list --tool-id sdt-xxxx -o text
```

`KeepaliveTimeout` is the keepalive renewal window (300s to 1800s, integer
seconds with a unit, echoed with the spelling you sent). It is unrelated to
`--default-timeout` / `--timeout`, which bound how long a sandbox may live.
Mode and duration are validated by the service, which also rejects values
outside the enum or range. `--auto-pause` selects pause or stop at expiry.
`--auto-resume` allows data-plane access to resume a paused instance.

Omit `--keepalive-configuration` on `tool fork` to copy the source Tool's
configuration. Supplying JSON replaces the entire object. Include every member
you want to carry over, or pass `{}` for an empty object.

`tool create --log-configuration` accepts a `ContentFormat` member next to
`CLSConfig`:

```bash
agr tool create -n logs-tool -t custom \
  --network-configuration '{"NetworkMode":"PUBLIC"}' \
  --custom-configuration '{"Image":"<registry>/<image>:<tag>","ImageRegistryType":"personal","Command":["<entrypoint>"]}' \
  --role-arn '<role-arn>' \
  --log-configuration '{"CLSConfig":{"TopicId":"<cls-topic-id>"},"ContentFormat":"auto"}' -o text
```

CLS logging is currently accepted only on `custom` tools: the service rejects a
`CLSConfig` on the built-in types (`code-interpreter`, `browser`, ...) with
`InvalidParameter` and "LogConfiguration with CLSConfig is only supported for
custom sandbox type for now", so a custom configuration is required too. That
custom configuration must name its start command: the service answers
`MissingParameter` and "CustomConfiguration.Command is required" without it. A
custom configuration the service accepts can still end at `FAILED` with
`FailedOperation.DependencyUnavailable`; use the shape of the custom tools that
already run in your account - a `Command` that starts the sandbox agent, with
that agent's `/health` endpoint declared as the probe and as a port.

A CLS topic needs a network mode with outbound access: the service rejects
`CLSConfig` on `SANDBOX` because the log sidecar cannot reach CLS from that
network. Use `PUBLIC` (or another mode with egress) for tools that ship logs,
and keep `SANDBOX` for tools that only need the sandbox network.

`raw` keeps the original log line; `auto` extracts the first JSON object level
or a complete logfmt line and falls back to the whole entry when it cannot
extract it fully. Values are case-insensitive and stored lower case; omitting
the member or sending an empty string keeps `raw`, and `auto` requires a valid
`CLSConfig.TopicId`. Log body processing happens on the server; the CLI does not
parse logs. `tool update` does not accept `--log-configuration`; create a new
tool or fork one instead. `tool fork` copies the configuration and accepts the
same three override flags.

## Look up known instances

Use `instance lookup` for 1 to 100 known instance IDs in the current region:

```bash
agr instance lookup ins-xxxx ins-yyyy -o text
agr instance lookup ins-xxxx ins-yyyy -o json --jq '.Data.InstanceSet[] | {InstanceId, Status}'
```

Lookup returns persisted lifecycle states without live container health queries,
pagination, or a total count. The service determines result order and duplicate
handling; the CLI preserves the returned order. Missing, deleted, or inaccessible
instances are omitted, and an empty result is successful. Requests must include
at least one ID. The caller needs permission for `DescribeSandboxInstances`.
Use `--request` with inline JSON, `@file`, or `-` for stdin as an alternative to
positional IDs; the two input modes cannot be combined.

## Sandbox snapshots, restoration, and instance cloning

The Runtime Snapshot API is Beta and may change incompatibly or be discontinued.
Create a disk-only snapshot from a running instance with disk snapshot support
enabled, save its returned `Snapshot.SnapshotId`, and use that ID to inspect, restore, or delete it:

```bash
agr instance snapshot create --instance-id ins-source --memory=false --snapshot-name checkpoint --client-token snapshot-001 -o text
agr instance snapshot list --snapshot-ids snapshot-xxxx -o text
agr instance create --snapshot-id snapshot-xxxx --client-token restore-001 -o text
agr instance snapshot delete snapshot-xxxx -o text
```

Use `--memory=false` explicitly. When omitted, the CLI leaves `Memory` unset;
the service defaults to including memory unless the source instance is configured
for disk-only snapshots. Memory snapshots are currently unsupported. Retry creation
with the same `ClientToken` and identical parameters.

Restore only after the snapshot is `ACTIVE`. Restoration creates a new instance
using the saved tool configuration, so `--tool-id` and `--tool-name/-t` cannot be
supplied. Running processes and memory state are not restored. `--snapshot-id`
conflicts with `--source-instance-id`, `--tool-id`, and `--tool-name/-t`.
In `--request`, `SnapshotId` likewise cannot be combined with `SourceInstanceId`,
`ToolId`, or `ToolName`.

Deletion prompts for confirmation when stdin is a TTY and defaults to cancel.
Use `--yes` (`-y`) to skip confirmation; `--non-interactive` and non-TTY stdin
also proceed without prompting. A successful response acknowledges the request;
cleanup may finish asynchronously.

List results contain `NextToken` for further pages. Use `--max-results 20`, then
pass the returned `--next-token` while keeping filters, snapshot IDs (including
array order), and `Limit` unchanged. `MaxResults` may change between pages; an empty
`NextToken` means there are no more pages. `--offset`/`--limit` pagination is also
supported, as is `--limit` with `--next-token`.

`Offset` must be non-negative. `Limit` and `MaxResults` accept 0-100; if both are
omitted or 0, the service uses 20. A positive `MaxResults` cannot be combined with
a positive `Offset` or `Limit`. A nonempty `NextToken` cannot be combined with a
positive `Offset`.

Clone a source instance with disk snapshot support enabled into a separate new
instance. Instances without this capability cannot be cloned. Pass the source instance ID:

```bash
agr instance create --source-instance-id ins-source --client-token clone-001 -o text
```

The new instance inherits the source tool, so a tool selector is optional; if
supplied, it must match the source tool. The source system disk is copied to the
new instance; subsequent writes to either system disk are independent. Running
processes and memory state are not preserved. Pass an instance ID to
`--source-instance-id` to clone it, or a saved snapshot ID to `--snapshot-id`
to restore it.

## Debug instance creation

Use `agr instance debug` with `--tool-id` or `--tool-name` to create a debug
instance for an existing tool. The command creates a temporary debug tool that
keeps the source tool configuration, changes the startup command to `/envd`,
mounts `ccr.ccs.tencentyun.com/ags-image/envd:v0.5.14` from `/usr/bin/envd`
to `/envd`, waits for that tool to be ready, then starts an instance from it.
The source tool must have `RoleArn` configured because image storage mounts
require it. `--timeout` controls the created instance lifetime and defaults
to `1h`; readiness waits use the command's internal workflow timeout.

```bash
debug_instance_id=$(agr instance debug --tool-id "$tool_id" \
  --timeout 30m \
  -o json --jq '.Data.InstanceId')
```

## Deployments

A Deployment gives a Sandbox Tool a stable remote endpoint and manages the
Sandbox Instances behind it. Scaling settings control active capacity;
lifecycle settings control what happens when those instances become idle.

The examples below assume that `tool_id` contains the ID of an existing
Sandbox Tool.

### Create and inspect a Deployment

A Deployment name must follow DNS-1123 naming rules, must be unique, and cannot
be changed after creation. Complex configuration flags accept inline JSON,
`@file`, or `-` to read JSON from stdin.

```bash
deployment_id=$(agr deployment create \
  --deployment-name workspace-service \
  --tool-id "$tool_id" \
  --scaling-configuration '{"MinInstanceCount":0,"MaxInstanceCount":10,"MaxInstanceRequestConcurrency":100}' \
  --lifecycle-configuration '{"IdleTimeoutSeconds":300,"IdleAction":"PAUSE"}' \
  -o json --jq '.Data.Deployment.DeploymentId')

agr deployment get "$deployment_id"
agr deployment list
```

By default, `create`, `get`, and `update` print a human-readable details view.
`list` prints compact scaling, lifecycle, affinity, and age summaries. Use
`-o json` when a script needs the complete API response.

### Update configuration

Scaling and lifecycle updates replace the existing configuration object; they
are not partial patches. Include every member of an object when updating it.

```bash
agr deployment update "$deployment_id" \
  --scaling-configuration '{"MinInstanceCount":1,"MaxInstanceCount":20,"MaxInstanceRequestConcurrency":100}' \
  --lifecycle-configuration '{"IdleTimeoutSeconds":600,"IdleAction":"STOP"}'
```

When creating a Deployment, omitted configuration members are filled by the
service. Run `agr deployment create --help` or
`agr deployment update --help` for the accepted fields.

### Delete a Deployment

Delete returns after the service accepts the request. Add `--wait` to poll with
the same wait behavior as Tool and Instance commands. A waited delete succeeds
when the remote Deployment is gone and reports `StatusReason` if asynchronous
deletion reaches `DELETE_FAILED`.

```bash
agr deployment delete "$deployment_id"
agr deployment delete "$deployment_id" --wait
```

### Proxy a Deployment port locally

Use `deployment proxy` only for local debugging. A single port uses the same
local and remote port; `local:remote` maps different ports.

```bash
# http://127.0.0.1:8080 forwards to remote port 8080
agr deployment proxy "$deployment_id" 8080

# http://127.0.0.1:3000 forwards to remote port 8080
agr deployment proxy "$deployment_id" 3000:8080
```

The proxy supports HTTP, SSE, and WebSocket traffic, but not raw TCP. It binds
to `127.0.0.1` by default. Using `--address` with a non-loopback address exposes
the debugging proxy to the network and prints a warning.

When Deployment affinity is enabled, the proxy captures the affinity ID from
the upstream response, prints it, and automatically reuses it for later HTTP,
SSE, and WebSocket requests during the lifetime of the proxy process. To resume
a known affinity session after restarting the proxy, initialize it explicitly:

```bash
agr deployment proxy "$deployment_id" 3000:8080 --affinity-id "$affinity_id"
```

`--affinity-id` is rejected when the Deployment does not enable affinity. The
proxy keeps affinity IDs only in memory and never persists them to disk.

## Managed Harness (preview)

Preview builds provide `agr model-provider` and `agr harness` for creating an
agent, talking over ACP, publishing revisions and rolling back. The
[Harness walkthrough](HARNESS.md) covers prerequisites, conversation
recovery, real MCP tools and cleanup. Publishing affects new instances; existing
instances keep their snapshot.

## Cloud endpoint vs data-plane domain

| Flag                         | Default                  | Controls                         |
|------------------------------|--------------------------|----------------------------------|
| `--cloud-endpoint`           | `ags.tencentcloudapi.com`| Control-plane API endpoint       |
| `--domain`                   | `tencentags.com`         | Data-plane domain (browser, exec)|

`--cloud-endpoint` affects every control-plane call (regular resource
commands and `agr api call`). `--domain` only affects data-plane access. Both can
also be set via `cloud_endpoint` / `domain` in `~/.agr/config.toml` or
`AGR_CLOUD_ENDPOINT` / `AGR_DOMAIN` environment variables.

## Session spaces, conversations, and history

Use `agr session` for the complete conversation workflow. `agr session space`
manages business spaces, the direct `session create/get/list/update/delete`
commands manage conversations, and `agr session event` writes and reads history.

First choose an existing space, or create one for a business purpose. Save the
server-returned `SpaceId`; do not construct it from the space name. Space lookup
uses the selected account and region. Use the returned ID for subsequent
conversations.

```bash
agr session space list -o text
agr session space create --name customer-support --description 'Customer support sessions' -o text
```

Replace the example space ID below with that returned ID. `UserId` comes from
your application, not your Tencent Cloud account. Keep the same SpaceId, UserId,
and SessionId across turns. SessionId is optional on create; save the returned
ID for subsequent operations. The published contract does not specify the
result of retrying creation with the same ID.

```bash
space_id=space-a1b2c3d4
user_id=user-123
session_id=session-123

agr session create --space-id "$space_id" --user-id "$user_id" --session-id "$session_id" --title 'Support request' -o text
agr session event append --space-id "$space_id" --user-id "$user_id" --session-id "$session_id" --event '{"EventId":"event-123","Author":"user","Content":{"Role":"user","Parts":[{"Text":"Hello"}]}}' -o text
agr session get --space-id "$space_id" --user-id "$user_id" --session-id "$session_id" -o text
agr session event list --space-id "$space_id" --user-id "$user_id" --session-id "$session_id" --offset 0 --limit 50 -o text
```

EventId is optional; the service generates it when empty. The published
contract does not promise deduplication for repeated EventIds. `--event` also accepts
`@event.json` or `-` for stdin. A session snapshot is not the complete event
history: continue `event list` with increasing `--offset` until all pages are
read. Use `session list --space-id ... --user-ids ...` to find a user's
conversations, and `session update` to change their title or metadata.

The current `session get` response has no event-list field, even when
`--num-recent-events` is supplied. Read events with `session event list`.

Delete a conversation before removing its space:

```bash
agr session delete --space-id "$space_id" --user-id "$user_id" --session-id "$session_id" -o text
agr session space delete --space-id "$space_id" -o text
```

Space deletion is a separate operation. The service only allows deletion of
non-default spaces with no sessions, events, or user state; use the service
result to determine whether the space can be removed.

Space commands previously exposed in preview as `agr session-space` now use
`agr session space`; the old command and schema IDs have no compatibility alias.
Discover each layer with `agr session --help`, `agr session space --help`, or
`agr session event --help`. For automation, use `-o json` and inspect the interface
with `agr schema session.space.create -o json`.

## Operational events

Use `agr event list` to inspect operational events in the configured
account and region. These events describe cloud resources; conversation history
is available through `agr session event list`.

```bash
agr event list -o text
agr event list --mode Aggregate --limit 20 -o json
agr event list --filters '[{"Name":"ObjectId","Values":["<resource-id>"]}]' -o text
agr event list --filters @filters.json -o json
cat filters.json | agr event list --filters - -o json
```

Replace `<resource-id>` with the resource's returned `ObjectId`. Filters support
the case-sensitive names `Source`, `ObjectType`, `ObjectId`, `EventType`,
`EventLevel`, `Reason`, `EventId`, `ToolId`, `ToolType` and `Message`. There may be
at most 12 conditions with distinct names, each with 1–20 nonempty values of at
most 256 bytes. Repeated `--filters` uses the last value. A complete `--request`
also accepts inline JSON, `@file` or stdin, and cannot be mixed with field flags.

All seven query members are optional. The service defaults to `Raw`, `Desc`,
offset 0, limit 10, EndTime=now and StartTime=EndTime minus one hour. RFC3339
timestamps define an inclusive start and exclusive end, within the last seven
days. The service clock controls retention and future-time checks.

For repeatable pagination, fix the same window, filters, mode, order and page
size. For example, capture the effective window when it is returned:

```bash
page=$(agr event list --mode Raw --order Asc --limit 20 -o json)
START=$(printf '%s\n' "$page" | jq -er '.Data.StartTime')
END=$(printf '%s\n' "$page" | jq -er '.Data.EndTime')
agr event list --mode Raw --order Asc --start-time "$START" --end-time "$END" --offset 20 --limit 20 -o json
```

If the response omits either effective timestamp, choose explicit RFC3339 bounds
within the retention window and restart from offset 0. Offset ranges from 0 to
10000; limit ranges from 1 to 100. The command fetches one page at a time.
`Raw` pages deduplicated events ordered by `OccurredAt`; `Aggregate` pages
aggregation results ordered by `LastOccurredAt`. Lifecycle events remain
individual aggregate rows with Count=1.

Text output shows the resource, type, level, reason and applicable times/count.
JSON retains `Events`, `TotalCount`, `RequestId`, optional effective window/mode,
all event fields and unknown fields. Optional mode-specific fields may be absent;
timestamps retain their timezone offsets and integer values retain precision.
`Extensions` is a JSON-encoded string, not an object. This read-only list supports
text and JSON output; NDJSON remains reserved for streaming commands. Discover
the interface with `agr event --help`, `agr event list --help`, or
`agr schema event.list -o json`.

## Uploading a Registry Plugin

Use an existing Registry and a local Plugin file. Create a record with
`--descriptor-type PLUGIN` and a `FILE` source declaring the raw byte count
(1 byte to 52 MiB). An optional `SHA256` is a 64-character lowercase
hexadecimal digest.

```bash
plugin=./plugin.bin
registry_id=reg-xxxx
source=$(jq -cn --argjson bytes "$(wc -c < "$plugin")" '{Type:"FILE",SizeBytes:$bytes}')
result=$(agr registry record create --registry-id "$registry_id" --name my-plugin \
  --descriptor-type PLUGIN --file-source "$source" -o json)
record_id=$(printf '%s\n' "$result" | jq -er '.Data.RecordId')
version_id=$(printf '%s\n' "$result" | jq -er '.Data.Version.VersionId')
upload_url=$(printf '%s\n' "$result" | jq -er '.Data.UploadURL')
curl --fail --proto '=https' --upload-file "$plugin" \
  --header 'Content-Type: application/octet-stream' "$upload_url"
agr registry record get --registry-id "$registry_id" --record-id "$record_id" \
  --version-id "$version_id" -o json
```

Upload before `Data.ExpireTime`. The file is ready once
`Data.Version.ContentStatus` is `READY`. Check the same version again if it is
still being prepared. Approval and assigning the stable label are separate
operations.

To upload a new version, pass `--file-source` with `Type=FILE` to
`registry record update` and use the new upload URL and Version ID. To reuse a
READY Skill or Plugin file from the same record without uploading, update with
`--file-source '{"Type":"VERSION","SourceVersionId":"rv-xxxx"}'`.
`--metadata` takes a JSON array of `Name`/`Value` objects and
`--supported-harness-versions` takes a JSON array of exact
`HarnessEngine`/`HarnessEngineVersion` pairs. These values apply to the new
version and are empty when omitted, including when reusing a file. Use
`--request @file` to supply the complete API request from a JSON file.

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
agr instance lookup <id> [id...] Look up persisted instance states by ID
agr instance snapshot create     Create a sandbox snapshot (Beta)
agr instance snapshot list       List sandbox snapshots (Beta)
agr instance snapshot delete     Request snapshot deletion (Beta)
agr instance get <id>            Get instance details
agr instance update <id>         Update timeout/metadata
agr instance pause <id>          Pause an instance
agr instance resume <id>         Resume an instance
agr instance delete <id>         Delete instance(s)
agr instance debug --tool-id <id>  Create a debug instance from a tool

agr instance code run <id>       Execute code in an existing instance
agr instance exec <id> -- CMD    Execute shell command in an existing instance
agr instance file upload <id>    Upload file to an existing instance
agr instance file download <id>  Download file from an existing instance
agr instance login <id>          PTY terminal session
agr instance browser vnc <id>    Show VNC URL
agr instance proxy <id> PORT     Forward instance port to localhost
agr instance mobile ...          Mobile ADB operations

agr deployment create            Create a Deployment
agr deployment list              List Deployments
agr deployment get <id>          Describe a Deployment
agr deployment update <id>       Update mutable Deployment configuration
agr deployment delete <id>       Delete a Deployment
agr deployment proxy <id> PORT   L7 proxy for local debugging

agr registry ...                 Manage registries, records, and skill packages
agr session ...                  Manage session spaces, sessions, and events
agr session space ...            Manage session spaces
agr session event ...            Append and list session events

agr tool list/create/fork/get/update/delete
agr apikey create/list/delete
agr pre-cache-image-task create|list|get
agr event list
agr completion bash|zsh|fish|powershell
```

Omit `--pre-cache-type` on `agr pre-cache-image-task create` to use the service
default (`image`). For image volume pre-caching, create with
`--pre-cache-type image_volume -o json`, then query with the returned image
digest, image address and registry type:

```bash
agr pre-cache-image-task create --image "$image" --image-registry-type "$registry_type" --pre-cache-type image_volume -o json
agr pre-cache-image-task get "$image_digest" --image "$image" --image-registry-type "$registry_type" --pre-cache-type image_volume -o json
```

Use `ImageDigest`, `Image`, and `ImageRegistryType` from the create response for
the lookup. `pre-cache-image-task list` shows ordinary image tasks. To see the
optional account concurrency limit, query:

```bash
agr api call DescribeQuotaOverview --request '{}' -o json \
  --jq '.Data.Response.Response.AccountQuotaOverview.Quota.PreCacheImageConcurrency'
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
--debug           Write debug diagnostics to stderr and full redacted logs locally
--debug-log       Append debug logs to a specified file (enables --debug)
```

Environment variables: `TENCENTCLOUD_SECRET_ID`, `TENCENTCLOUD_SECRET_KEY`, `TENCENTCLOUD_TOKEN`, `AGR_OUTPUT`, `AGR_REGION`, `AGR_CLOUD_ENDPOINT`, `AGR_DOMAIN`, `AGR_NON_INTERACTIVE`, `AGR_DEBUG`, `NO_COLOR`.

`AGR_OUTPUT` is intended for default `text` or `json` output. For streaming, pass `-o ndjson` explicitly with `agr instance code run --stream` or `agr instance exec --stream`.

Configuration priority: `--flag` > environment variable > `~/.agr/config.toml` > default. Use `agr status` to inspect resolved values and their sources.

## Troubleshooting

Use `--debug` to write a bounded, redacted error chain to stderr. JSON stdout
remains one envelope; NDJSON streams retain their single terminal event. Unknown
errors still show `INTERNAL_ERROR` in normal output. No stack dump or upload is
performed.

Redaction replaces credential values only and never removes surrounding text,
so the failure reason stays intact. It replaces, by exact value (raw and
URL-encoded), the active SecretId/SecretKey/Token and the data-plane/deployment
access tokens the CLI obtains; the credential after `Bearer`/`Basic` (16 or more
characters); and URL passwords and signed URL query values. Other header values
are not guessed from text.

`--debug` (or `AGR_DEBUG=1`) also saves full redacted diagnostics and a copy of stderr to
`~/.agr/logs/agr-<UTC timestamp>-<unique suffix>.log`. Use
`--debug-log ./logs/agr.log` to enable debug and append to a specified file.
Successful and failed commands print `Debug log: <absolute path>` to stderr;
logging failures produce a warning without replacing the command's result or
exit code.

The copied stderr, including remote program output, is written in full with no
line limit; only the known credential values above are replaced, wherever
output chunks split them. Remote programs' own secrets are not known to the CLI
and are not redacted, as with `kubectl logs`. The original program stderr still
reaches the terminal immediately and unchanged.

Default log files are created per process and are not automatically rotated or
deleted. Background mobile tunnel processes follow the parent's effective debug
setting (an explicit `--debug=false` overrides an inherited `AGR_DEBUG=1`) and,
when enabled, create their own default log files; a `--debug-log` path is not
shared with them.
If output ends with a fragment that may start a known credential, that fragment
is written as `[REDACTED]`. Remove old files when they are no longer needed.

Redaction also applies to ordinary text errors and JSON/NDJSON `Failure` fields,
including nested `Details`, without truncating ordinary error strings. Only
terminal debug diagnostics are capped at 8 KiB plus a UTF-8-safe truncation marker.
URL redaction changes only passwords and sensitive query values, even when
unrelated URL escapes are malformed. `Details` fields named after credentials or
headers (such as `Authorization`, `Cookie`, `SecretKey`) are replaced; generic
`Details.token`/`signature`/`sig` fields are not hidden by name alone.

For mobile tunnel failures, token acquisition retains cloud API
classification and RequestId; timeouts and cancellations retain their own kinds.
Handshake HTTP 401/403 uses `TUNNEL_AUTH_FAILED` (exit 4). A local port already
in use returns `PORT_IN_USE` (exit 2); choose another port or `--port 0`. Other
unclassified tunnel operations use `TUNNEL_ERROR` and retain the observed reason.
Other WebSocket handshake failures use `NETWORK_ERROR` (exit 1), with optional
`Failure.Details.Stage=websocket_handshake` and `HTTPStatus` when observed. These
fields are described by `agr schema -o json` under `Data.FailureDetails` and by
`agr explain NETWORK_ERROR`. Background `mobile connect` preserves the child's
structured failure and exit code, including handshake status and cloud RequestId.
When no valid child failure is available, it reports `TUNNEL_START_FAILED`,
`TUNNEL_EXITED`, `TUNNEL_PROTOCOL_ERROR`, or `TUNNEL_READY_TIMEOUT` according to
the observed startup stage. Cancellation and context deadlines retain their own
classification; failed startup terminates and reaps the child with bounded waits.

If a tunnel log was created, failures include `Failure.Details.LogPath` (also
shown in text output). `--debug` is forwarded to the child and adds a separate
`tunnel log tail:` section to parent stderr: at most 40 lines from the last
64 KiB, capped to the last 8 KiB plus a truncation marker. Each tail line is
prefixed with `  | ` and the section ends with `end of tunnel log tail`, so the
child's own error output is not mistaken for the parent's. Tunnel log records
are redacted and bounded before being written to disk; the tail is redacted
again before display. A log creation failure does not change the underlying
failure classification and does not return a nonexistent log path.

Mobile WebSocket handshake diagnostics include `Endpoint` (without user info,
query or fragment) and `TimeoutMs` (the effective handshake budget, not elapsed
time). Probes use 10 seconds and runtime connections use 15 seconds; a shorter
context deadline reduces that budget. The stage remains `websocket_handshake`,
including DNS, connection and TLS failures: the CLI does not infer an internal
network phase. HTTP status is omitted when no response was received. Request IDs
are taken only from `X-TC-RequestId`, then `X-Request-Id`, with a 256-byte limit
and printable ASCII excluding spaces. Other response headers and bodies are not
retained. These observations apply to startup/recovery probes and runtime
connection failures; startup failures also preserve them across background
readiness. Token acquisition remains a separate error boundary.

Other boundaries add optional `Stage` and `Operation`, plus `Program`, `Path`,
`Field` or `InstanceId` where relevant. Text failures display available boundary
metadata; JSON/NDJSON retain it in `Failure.Details`. File errors retain their OS
cause. Configuration parse/decode failures report the file, the parser location
and a value-free reason when available, such as the TOML syntax error or the field
name with its expected type. Values from a config that has not loaded are never
quoted; an offending input character is shown as `[REDACTED]`.
Remote connection setup uses `remote_connect`; SDK execution failures use
`remote_execute`, which does not prove whether the remote program started.
PTY reports `remote_start` before its start event and `remote_stream` afterwards.
Cloud SDK errors preserve Code/Message/RequestId and add the target/action and any
known caller budget; unavailable HTTP status/headers are omitted. When no API
response arrives, `ClientError.NetworkError` is classified as `Kind=network` but
stays `Retryable=false`: the SDK does not say whether the request reached the
service, so check the operation's outcome before retrying. Proxy HTTP/WS
failures log observed response status and allowlisted IDs to redacted, bounded
stderr diagnostics while preserving the proxied response and stream protocol.

Remote programs returning nonzero exit codes keep their existing output and
exit-code semantics. Diagnostic availability does not make retries safe to replay.

```bash
agr status
agr doctor
agr explain AUTH_FAILED
agr schema instance.create -o json
```

## License

See [LICENSE](LICENSE-AGR%20CLI.txt).
