# AGR CLI

[English](README.md)

AGR CLI 是腾讯云 Agent Runtime（AGR）的命令行工具，用于管理 Deployment、实例、工具、API Key 与数据面操作；安装后的命令名为 `agr`。

## 安装

### Homebrew（macOS / Linux）

```bash
brew install TencentCloudAgentRuntime/tap/agr-cli
```

### 一键安装（macOS / Linux）

默认使用 official 下载源：

```bash
curl -fsSL https://dl.tencentags.com/agr-cli/latest/install.sh | sh
```

安装器会优先使用可写的 `/usr/local/bin`；在无 root 权限的环境中，会自动回退到
`$HOME/.local/bin`，且不会调用 `sudo`。如果所选目录不在 `PATH` 中，安装器会输出
相应的添加命令。

如需明确指定其他可写目录：

```bash
curl -fsSL https://dl.tencentags.com/agr-cli/latest/install.sh | INSTALL_DIR="$HOME/bin" sh
```

安装指定版本：

```bash
curl -fsSL https://dl.tencentags.com/agr-cli/latest/install.sh | VERSION=v0.7.0 sh
```

使用 GitHub Releases 作为备用下载源：

```bash
curl -fsSL https://github.com/TencentCloudAgentRuntime/ags-cli/releases/latest/download/install.sh | AGR_DOWNLOAD_MIRROR=github sh
```

### 手动下载二进制文件

从 [GitHub Releases](https://github.com/TencentCloudAgentRuntime/ags-cli/releases) 下载最新版本并手动安装。

将 `VERSION` 设置为目标版本号（如 `0.7.0`）：

**macOS（Apple Silicon）**

```bash
VERSION=0.7.0
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-darwin-arm64.tar.gz
tar xzf agr-${VERSION}-darwin-arm64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-darwin-arm64.tar.gz
```

**macOS（Intel）**

```bash
VERSION=0.7.0
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-darwin-amd64.tar.gz
tar xzf agr-${VERSION}-darwin-amd64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-darwin-amd64.tar.gz
```

**Linux（x86_64）**

```bash
VERSION=0.7.0
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-linux-amd64.tar.gz
tar xzf agr-${VERSION}-linux-amd64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-linux-amd64.tar.gz
```

**Linux（ARM64）**

```bash
VERSION=0.7.0
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-linux-arm64.tar.gz
tar xzf agr-${VERSION}-linux-arm64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-linux-arm64.tar.gz
```

**Windows（x86_64）— PowerShell**

```powershell
$VERSION = "0.7.0"
Invoke-WebRequest -Uri "https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-windows-amd64.zip" -OutFile "agr-${VERSION}-windows-amd64.zip"
Expand-Archive "agr-${VERSION}-windows-amd64.zip" -DestinationPath .
Move-Item agr.exe "$env:USERPROFILE\bin\agr.exe"
Remove-Item "agr-${VERSION}-windows-amd64.zip"
```

> 请确保 `$env:USERPROFILE\bin` 在 `PATH` 中。

### 校验下载文件

```bash
VERSION=0.7.0
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
```

### 从源码构建

```bash
git clone https://github.com/TencentCloudAgentRuntime/ags-cli.git
cd ags-cli
make build
sudo cp agr /usr/local/bin/agr
```

或安装到 `$GOPATH/bin`（带版本元信息）：

```bash
make go-install
```

### 使用 `go install`

```bash
go install github.com/TencentCloudAgentRuntime/ags-cli/cmd/agr@latest
```

### 验证安装

```bash
agr version
```

安装后的命令名为 `agr`。

> **注意：** 通过 `go install @tag` 安装的二进制在 `agr version` 中会显示
> `commit: n/a (go install)` 和 `built: n/a (go install)`：Go 不会为模块缓存
> 构建注入 VCS 元数据。若需完整 commit 哈希和构建时间，请使用 `make build`
> 或下载预编译发布包。

## 前置条件

1. [腾讯云](https://cloud.tencent.com/) 账号
2. 已开通 AGR（Agent Runtime）服务
3. API 凭据（SecretID / SecretKey）— 在 [访问管理控制台](https://console.cloud.tencent.com/cam/capi) 获取

## 初始化 CLI 凭据

```bash
export TENCENTCLOUD_SECRET_ID="your-secret-id"
export TENCENTCLOUD_SECRET_KEY="your-secret-key"

agr init \
  --secret-id "$TENCENTCLOUD_SECRET_ID" \
  --secret-key "$TENCENTCLOUD_SECRET_KEY"
```

`agr init` 只写本机 CLI 配置，不创建远端资源。

## 快速开始

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

示例会先生成一个唯一的工具名，因为同一 AppId 下工具名称必须唯一。

## 临时沙箱工作流

`agr instance code run` 与 `agr instance exec` 接受
`--create-temp-instance`：仅为本次执行创建一个临时沙箱并自动清理。
这里引用的工具必须事先存在，请先用 `agr tool create` 创建，再传
`--tool-name` 或 `--tool-id`：

```bash
# 创建临时实例，运行片段，结束后总是删除（cleanup=always 是默认值）
agr instance code run \
  --create-temp-instance \
  --tool-id "$tool_id" \
  -c "print('hello')"

# 同样的工作流，但保留临时实例（用于排错）
agr instance exec \
  --create-temp-instance \
  --tool-id "$tool_id" \
  --cleanup never \
  -- python -V
```

`--cleanup` 接受 `always`（默认）、`success`、`never` 三种取值。
若需要在执行结束后保留临时实例，使用 `--cleanup never`；
**不存在 `--keep-temp-instance` 标志**。

`-o json` 时，响应包含 `Data.ExecutionContext.SandboxInstanceId`、
`Data.ExecutionContext.TemporarySandboxInstance` 与
`Data.ExecutionContext.Cleanup`，方便脚本检查工作流。

## 按 ID 点查实例

已知实例 ID 时，使用 `instance lookup` 查询当前地域内的 1～100 个实例：

```bash
agr instance lookup ins-xxxx ins-yyyy -o text
agr instance lookup ins-xxxx ins-yyyy -o json --jq '.Data.InstanceSet[] | {InstanceId, Status}'
```

点查返回持久化生命周期状态，不查询实时容器健康状态，也不分页或统计总数。
结果去重后按 ID 首次出现的顺序返回；不存在、已删除或无权限的实例会被省略，
空结果仍为成功。调用者需要 `DescribeSandboxInstances` 权限。
也可以通过 `--request` 传入 inline JSON、`@file` 或 `-`（stdin），
但不能与位置参数 ID 混用。

## 沙箱快照、恢复与实例克隆

Runtime Snapshot API 当前为 Beta，后续可能发生不兼容变更或被弃用。
从已启用磁盘快照能力、处于运行状态的实例创建磁盘快照，保存返回的
`Snapshot.SnapshotId`，再用该 ID 查询、恢复或删除：

```bash
agr instance snapshot create --instance-id ins-source --memory=false --snapshot-name checkpoint --client-token snapshot-001 -o text
agr instance snapshot list --snapshot-ids snapshot-xxxx -o text
agr instance create --snapshot-id snapshot-xxxx --client-token restore-001 -o text
agr instance snapshot delete snapshot-xxxx -o text
```

请显式传入 `--memory=false`。省略时 CLI 不发送 `Memory`；服务端默认包含内存，
除非源实例已配置为仅磁盘快照。当前暂不支持内存快照。
重试创建时保持相同的 `ClientToken` 和请求参数。

快照状态为 `ACTIVE` 后才能恢复。恢复会使用快照保存的工具配置创建新实例，
因此不能再传入 `--tool-id` 或 `--tool-name/-t`，不会恢复运行中的进程和内存状态。
`--snapshot-id` 与 `--source-instance-id`、`--tool-id`、`--tool-name/-t` 互斥，
`--request` 中的 `SnapshotId` 也不能与 `SourceInstanceId`、`ToolId`、`ToolName` 同时指定。

标准输入为 TTY 时，删除前会提示确认，默认取消。使用 `--yes`（`-y`）跳过确认；
`--non-interactive` 或非 TTY 标准输入也会直接执行，不提示确认。
删除成功表示请求已受理，清理可能异步完成。

列表返回 `NextToken` 供后续翻页。先使用 `--max-results 20`，再传入返回的
`--next-token`，并保持过滤条件、快照 ID（包括数组顺序）和 `Limit` 不变。
`MaxResults` 可以在翻页时调整；`NextToken` 为空表示没有后续页。
也支持 `--offset`/`--limit` 分页，以及 `--limit` 与 `--next-token` 组合。

`Offset` 必须非负；`Limit` 和 `MaxResults` 接受 0–100，两者均省略或为 0 时，
服务端使用默认页大小 20。正数 `MaxResults` 不能与正数 `Offset` 或 `Limit` 组合；
非空 `NextToken` 不能与正数 `Offset` 组合。

源实例必须已启用磁盘快照能力，未启用该能力的实例无法克隆。使用该源实例的 ID
克隆出独立的新实例：

```bash
agr instance create --source-instance-id ins-source --client-token clone-001 -o text
```

新实例继承源实例的工具，因此可以省略工具选择参数；若提供，则必须与源实例的工具匹配。
克隆会复制源实例的系统盘，之后两者的系统盘写入相互独立，不保留运行中的进程和内存状态。
向 `--source-instance-id` 传入实例 ID 进行克隆；向 `--snapshot-id` 传入
已保存的快照 ID 进行恢复。

## Debug Tool 创建

使用 `agr instance debug --tool-id` 或 `--tool-name` 基于现有工具创建一份
Debug Tool。新工具会保留源工具配置，把启动命令改为 `/envd`，并将
`ccr.ccs.tencentyun.com/ags-image/envd:v0.5.14` 镜像内的
`/usr/bin/envd` 挂载到 `/envd`。源工具必须配置 `RoleArn`，因为
镜像类型的存储挂载依赖该角色。

```bash
debug_instance_id=$(agr instance debug --tool-id "$tool_id" \
  --timeout 30m \
  -o json --jq '.Data.InstanceId')
```

## Deployment

Deployment 为 Sandbox Tool 提供稳定的远端访问入口，并管理入口背后的
Sandbox Instance。伸缩配置决定活跃容量，生命周期配置决定实例空闲后的处理方式。

以下示例假设 `tool_id` 已保存一个现有 Sandbox Tool 的 ID。

### 创建和查看 Deployment

Deployment 名称必须符合 DNS-1123 规范、保持唯一，且创建后不可修改。
复杂配置既可以直接传入 JSON，也可以用 `@file` 从文件读取，或用 `-` 从标准输入
读取。

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

默认情况下，`create`、`get`、`update` 输出便于阅读的详情，`list` 紧凑展示
伸缩、生命周期、亲和性和资源存续时间。脚本需要完整 API 响应时使用 `-o json`。

### 更新配置

更新伸缩或生命周期配置时，传入的对象会完整替换原配置，而不是局部修改。因此，
被更新的配置对象必须包含它的全部字段。

```bash
agr deployment update "$deployment_id" \
  --scaling-configuration '{"MinInstanceCount":1,"MaxInstanceCount":20,"MaxInstanceRequestConcurrency":100}' \
  --lifecycle-configuration '{"IdleTimeoutSeconds":600,"IdleAction":"STOP"}'
```

创建 Deployment 时可以省略配置对象中的部分字段，服务端会补齐默认值。可通过
`agr deployment create --help` 和 `agr deployment update --help` 查看字段说明。

### 删除 Deployment

删除命令默认在服务端接受请求后返回。添加 `--wait` 后，会复用 Tool 和 Instance
命令的等待机制持续轮询；远端 Deployment 消失时视为成功，异步删除进入
`DELETE_FAILED` 时会报告服务端返回的 `StatusReason`。

```bash
agr deployment delete "$deployment_id"
agr deployment delete "$deployment_id" --wait
```

### 将 Deployment 端口代理到本机

`deployment proxy` 只推荐用于本地调试。只写一个端口时，本地和远端使用相同
端口；`本地端口:远端端口` 可映射不同端口。

```bash
# http://127.0.0.1:8080 转发到远端 8080 端口
agr deployment proxy "$deployment_id" 8080

# http://127.0.0.1:3000 转发到远端 8080 端口
agr deployment proxy "$deployment_id" 3000:8080
```

代理支持 HTTP、SSE 和 WebSocket，不支持原始 TCP。默认只监听
`127.0.0.1`；通过 `--address` 监听非回环地址会把调试代理暴露到网络，并输出
风险提示。

## 控制面端点与数据面域名

| Flag | 默认值 | 作用对象 |
|---|---|---|
| `--cloud-endpoint` | `ags.tencentcloudapi.com` | 控制面 API 端点 |
| `--domain` | `tencentags.com` | 数据面域名（browser、exec 等数据通道）|

`--cloud-endpoint` 同时作用于常规资源命令与 `agr api call`；`--domain`
仅影响数据面访问。两者也可通过 `~/.agr/config.toml` 中的
`cloud_endpoint` / `domain` 字段，或 `AGR_CLOUD_ENDPOINT` /
`AGR_DOMAIN` 环境变量设置。两个开关相互独立。

## 会话空间、会话与历史

通过 `agr session` 完成整个会话流程：`agr session space` 管理业务空间，
`session create/get/list/update/delete` 管理具体会话，`agr session event`
追加和查询交互历史。

先选择已有空间，或按业务用途创建空间。保存服务端返回的 `SpaceId`，不要
根据空间名称构造 ID。空间查询使用当前选择的账号和地域，后续会话使用
已返回的空间 ID。

```bash
agr session space list -o text
agr session space create --name customer-support --description 'Customer support sessions' -o text
```

将下面的示例空间 ID 替换为返回的 ID。`UserId` 来自调用方业务系统，不是
腾讯云账号 ID。每轮操作使用相同的 SpaceId、UserId 和 SessionId。创建时
SessionId 可选，请保存返回的 ID 用于后续操作。公开契约没有规定使用相同
ID 重试创建的结果。

```bash
space_id=space-a1b2c3d4
user_id=user-123
session_id=session-123

agr session create --space-id "$space_id" --user-id "$user_id" --session-id "$session_id" --title 'Support request' -o text
agr session event append --space-id "$space_id" --user-id "$user_id" --session-id "$session_id" --event '{"EventId":"event-123","Author":"user","Content":{"Role":"user","Parts":[{"Text":"Hello"}]}}' -o text
agr session get --space-id "$space_id" --user-id "$user_id" --session-id "$session_id" -o text
agr session event list --space-id "$space_id" --user-id "$user_id" --session-id "$session_id" --offset 0 --limit 50 -o text
```

EventId 可选，为空时由服务端生成；公开契约不承诺重复 EventId 会去重。
`--event` 也支持 `@event.json` 和
`-`/stdin。会话快照不代表完整历史；通过 `event list` 增加 `--offset`
读取后续页，直到获取全部事件。使用
`session list --space-id ... --user-ids ...` 查找某个业务用户的会话，通过
`session update` 修改标题或元数据。

当前 `session get` 响应没有事件列表字段，即使传入 `--num-recent-events`
也不能从该响应获取事件；查询事件请使用 `session event list`。

删除空间前，先删除其中的会话：

```bash
agr session delete --space-id "$space_id" --user-id "$user_id" --session-id "$session_id" -o text
agr session space delete --space-id "$space_id" -o text
```

空间删除是单独的操作。服务端只允许删除没有会话、事件或用户状态的
非默认空间；空间是否可以删除，应以服务端返回结果为准。

此前 preview 的 `agr session-space` 已统一为 `agr session space`，旧命令及
schema ID 不保留兼容别名。通过 `agr session --help`、
`agr session space --help` 和 `agr session event --help` 分层发现命令。
自动化使用 `-o json`，接口结构可通过
`agr schema session.space.create -o json` 查看。

## 原始 API 调用

每个已映射 API 操作的稳定入口仍是对应的资源命令。当需要调试或访问
资源命令尚未覆盖的字段时，可以使用 `agr api call`：

```bash
agr api call DescribeSandboxInstanceList --request '{"Limit":1}'
agr api call StartSandboxInstance --request @start.json
agr api call StopSandboxInstance --request - < stop.json
```

## 命令一览

```text
agr                              打印帮助
agr init                         初始化本机 CLI 配置与凭据
agr version                      版本信息
agr status                       当前配置状态
agr schema [command]             机器可读命令 schema
agr doctor                       诊断配置与连通性
agr explain <CODE>               解释错误码与修复建议

agr instance create              创建实例
agr instance list                列出实例
agr instance lookup <id> [id...] 按 ID 点查实例持久化状态
agr instance snapshot create     创建沙箱快照（Beta）
agr instance snapshot list       查询沙箱快照（Beta）
agr instance snapshot delete     请求删除沙箱快照（Beta）
agr instance get <id>            实例详情
agr instance update <id>         更新 timeout / metadata
agr instance pause <id>          暂停实例
agr instance resume <id>         恢复实例
agr instance delete <id>         删除实例
agr instance debug --tool-id <id>  基于工具创建 Debug 实例

agr instance code run <id>       在实例中执行代码
agr instance exec <id> -- CMD    在实例中执行 shell 命令
agr instance file upload <id>    上传文件
agr instance file download <id>  下载文件
agr instance login <id>          PTY 终端会话
agr instance browser vnc <id>    显示 VNC URL
agr instance proxy <id> PORT     端口转发到 localhost
agr instance mobile ...          Mobile ADB 操作

agr deployment create            创建 Deployment
agr deployment list              列出 Deployment
agr deployment get <id>          查看 Deployment 详情
agr deployment update <id>       更新 Deployment 可变配置
agr deployment delete <id>       删除 Deployment
agr deployment proxy <id> PORT   本地调试七层代理

agr registry ...                 管理 Registry、记录和 Skill 包
agr session ...                  管理会话空间、会话与事件
agr session space ...            管理会话空间
agr session event ...            追加和查询会话事件

agr tool list/create/fork/get/update/delete
agr apikey create/list/delete
agr pre-cache-image-task create|get
agr completion bash|zsh|fish|powershell
```

## 机器可读输出

支持 `-o json` 的命令在 stdout 返回一份 `agr.v1` envelope：

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

示例：

```bash
agr instance create --tool-id "$tool_id" -o json --jq '.Data.InstanceId'
agr instance list -o json --jq '.Data.Items[].InstanceId'
agr status -o json --jq '.Data.Region'
agr schema -o json --jq '.Data.ExitCodes'
```

`--jq` 必须与 `-o json` 一起使用。JSON envelope 使用 `agr.v1`，
NDJSON 事件使用 `agr.events.v1`。

## 流式输出

只有 `instance code run` 与 `instance exec` 支持机器可读的流式输出：

```bash
agr instance code run "$id" -c "print(1)" --stream -o ndjson
agr instance exec "$id" --stream -o ndjson -- tail -f app.log
```

每行 stdout 是一个 `agr.events.v1` JSON 事件。

## 退出码

| Exit | Kind | 说明 |
|---:|---|---|
| 0 | success | 成功 |
| 1 | error | 通用错误，细分原因查看 `Failure.Kind` |
| 2 | usage | 参数、flag 或输入错误 |
| 4 | auth | 凭证、鉴权或权限错误 |
| 255 | remote_execution_failed | 远端代码执行失败 |

`instance exec` 与 `instance mobile adb` 也可能透传下游进程的退出码（`0-255` 范围）。

完整列表见 `agr schema -o json --jq '.Data.ExitCodes'`。

## 全局 flag

```text
--config          配置文件路径（默认：~/.agr/config.toml）
-o, --output      输出格式：text、json、ndjson（`ndjson` 仅在显式传给受支持的流式命令时可用）
--jq              jq 表达式（仅 `-o json` 时生效）
--region          腾讯云 region（默认：ap-guangzhou）
--cloud-endpoint  控制面 API 端点（默认：ags.tencentcloudapi.com）
--domain          数据面域名（默认：tencentags.com）
--secret-id       腾讯云 SecretID
--secret-key      腾讯云 SecretKey
--non-interactive 禁用交互提示
--no-color        关闭 ANSI 颜色
--debug           将调试信息写到 stderr，并在本地保存完整脱敏日志
--debug-log       将调试日志追加到指定文件（自动开启 --debug）
```

环境变量：`TENCENTCLOUD_SECRET_ID`、`TENCENTCLOUD_SECRET_KEY`、
`AGR_OUTPUT`、`AGR_REGION`、`AGR_CLOUD_ENDPOINT`、`AGR_DOMAIN`、
`AGR_NON_INTERACTIVE`、`AGR_DEBUG`、`NO_COLOR`。

`AGR_OUTPUT` 适合作为 `text` 或 `json` 的默认输出配置。若要获取流式事件，请在
`agr instance code run --stream` 或 `agr instance exec --stream` 上显式传
`-o ndjson`。

配置优先级：`--flag` > 环境变量 > `~/.agr/config.toml` > 默认值。使用 `agr status` 查看当前生效值及其来源。

## 故障排查

`--debug` 会将限长、脱敏后的错误链写到 stderr。JSON stdout 仍是单个 envelope，
NDJSON 流仍只输出一次终止事件；未知错误在普通模式下仍显示 `INTERNAL_ERROR`。
不生成堆栈转储或上传日志。

脱敏只替换凭据值本身，不删除周围文字，失败原因保持完整。替换范围：按精确值（原文及 URL
编码形式）替换当前生效的 SecretId/SecretKey/Token 以及 CLI 获取的 data-plane/deployment
访问 token；`Bearer`/`Basic` 后 16 个字符以上的凭据；URL 密码和签名 query 值。
不从文本中猜测其他请求头的值。

`--debug`（或 `AGR_DEBUG=1`）还会将完整脱敏诊断和 stderr 保存到
`~/.agr/logs/agr-<UTC 时间>-<随机后缀>.log`。可用 `--debug-log ./logs/agr.log`
开启 debug 并追加到指定文件。命令成功或失败时都会在 stderr
显示 `Debug log: <绝对路径>`。日志写入失败会给出警告，保留原命令结果和退出码。

复制的 stderr（包括远端程序输出）完整写入，不限制单行长度；只替换上述已知凭据值，
即使输出分段切开了凭据也能识别。远端程序自己的秘密 CLI 无从得知，不会脱敏，
与 `kubectl logs` 一致。远端程序原始 stderr 仍及时、原样显示在终端。

默认日志按进程新建，没有自动轮转或清理。后台 mobile tunnel 子进程跟随父进程最终生效的
debug 设置（显式 `--debug=false` 优先于继承的 `AGR_DEBUG=1`），开启时创建自己的默认日志文件，
不共用 `--debug-log` 指定的路径。若输出以可能是已知凭据开头的片段结束，
该片段写为 `[REDACTED]`。不再需要的旧日志需自行删除。

普通 text 错误和 JSON/NDJSON 的 Failure 字段（含嵌套 Details）也会脱敏；
普通错误字符串不截断，仅终端 debug 诊断限制为 8 KiB 加 UTF-8 安全截断标记。
URL 只替换密码和敏感 query 值；其他参数转义损坏时也不整段隐藏。
Details 中以凭据或请求头命名的字段（如 `Authorization`、`Cookie`、`SecretKey`）会被替换；
通用的 token/signature/sig 字段不仅凭名字隐藏。

mobile tunnel 获取 token 失败时保留云 API 分类和 RequestId；超时、取消保留各自分类。
握手 HTTP 401/403 返回 `TUNNEL_AUTH_FAILED`（退出码 4）。本地端口占用返回
`PORT_IN_USE`（退出码 2），可换端口或使用 `--port 0`；其他未分类 tunnel 操作
返回 `TUNNEL_ERROR` 并保留已观察到的原因。
其他 WebSocket 握手失败使用 `NETWORK_ERROR`（退出码 1），并按实际观察附带
`Failure.Details.Stage=websocket_handshake` 和 `HTTPStatus`，没有 HTTP 响应则省略状态码。
字段说明见 `agr schema -o json` 的 `Data.FailureDetails` 和 `agr explain NETWORK_ERROR`。
后台 `mobile connect` 保留子进程的结构化 Failure 和退出码，包括握手状态码和云 API RequestId。
没有有效子进程 Failure 时，按实际观察到的阶段返回 `TUNNEL_START_FAILED`、
`TUNNEL_EXITED`、`TUNNEL_PROTOCOL_ERROR` 或 `TUNNEL_READY_TIMEOUT`。
取消和 context deadline 保留各自分类；启动失败后会终止并回收子进程，清理等待有时间上限。

仅在成功创建日志文件时，失败结果才包含 `Failure.Details.LogPath`，文本输出也会显示该路径。
`--debug` 会转发给子进程，并在父进程 stderr 中单独显示 `tunnel log tail:`：
从文件末尾 64 KiB 中取最多 40 行，再限制为末尾 8 KiB 加截断标记。
摘录每行以 `  | ` 开头，并以 `end of tunnel log tail` 结束，避免把子进程自身的错误输出误认为父进程错误。
tunnel 日志记录在落盘前就会脱敏和限长，尾部日志显示前会再次脱敏。
日志文件创建失败不改变原始错误分类，也不会返回不存在的日志路径。

mobile WebSocket 握手诊断包含 `Endpoint`（不含用户信息、query 和 fragment）及
`TimeoutMs`（实际生效的握手预算，不是已耗时）。探测预算为 10 秒，运行期连接为
15 秒；更短的 context deadline 会缩短预算。DNS、连接和 TLS 失败仍统一记录为
`websocket_handshake`，不推测内部网络阶段；没有收到响应则省略 HTTP 状态。
请求 ID 仅依次读取 `X-TC-RequestId`、`X-Request-Id`，限制为不含空格的可打印 ASCII、
最多 256 字节，不保留其他响应头和响应正文。启动探测、恢复探测和运行期连接失败
均记录这些观察结果；启动失败还会跨后台就绪协议保留它们。token 获取失败保持独立分类。

其他边界按需补充 `Stage`、`Operation`，以及 `Program`、`Path`、`Field` 或
`InstanceId`。普通文本错误显示可用的边界信息；JSON/NDJSON 放在 `Failure.Details`。
文件错误保留底层 OS cause；配置解析或类型转换失败显示路径、可用的行列位置，
以及不含配置值的原因（如 TOML 语法错误，或字段名及其期望类型）。
不引用尚未成功加载的配置值，出错位置的输入字符显示为 `[REDACTED]`。连接准备失败使用 `remote_connect`；SDK 执行调用失败
使用 `remote_execute`，不据此推断远端程序是否已启动。PTY 在收到启动事件前使用
`remote_start`，之后使用 `remote_stream`。
云 SDK 保留 Code/Message/RequestId，并补目标、操作及已知调用预算；SDK 未提供的
HTTP 状态和响应头不填猜测值。没有收到 API 响应时，`ClientError.NetworkError`
归类为 `Kind=network`，但仍为 `Retryable=false`：SDK 无法说明请求是否已到达服务端，
重试前请先确认操作结果。代理 HTTP/WS 失败在脱敏、限长的 stderr 日志中记录
实际状态和白名单请求 ID，转发响应及流式协议保持不变。

远端程序正常返回非零退出码时，保留已有输出和退出码语义。诊断增强不代表业务操作可安全重试。

```bash
agr status
agr doctor
agr explain AUTH_FAILED
agr schema instance.create -o json
```

## License

详见 [LICENSE](LICENSE-AGR%20CLI.txt)。
