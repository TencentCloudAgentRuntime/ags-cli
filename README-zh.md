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
curl -fsSL https://dl.tencentags.com/agr-cli/latest/install.sh | VERSION=v0.6.2 sh
```

使用 GitHub Releases 作为备用下载源：

```bash
curl -fsSL https://github.com/TencentCloudAgentRuntime/ags-cli/releases/latest/download/install.sh | AGR_DOWNLOAD_MIRROR=github sh
```

### 手动下载二进制文件

从 [GitHub Releases](https://github.com/TencentCloudAgentRuntime/ags-cli/releases) 下载最新版本并手动安装。

将 `VERSION` 设置为目标版本号（如 `0.6.1`）：

**macOS（Apple Silicon）**

```bash
VERSION=0.6.1
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-darwin-arm64.tar.gz
tar xzf agr-${VERSION}-darwin-arm64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-darwin-arm64.tar.gz
```

**macOS（Intel）**

```bash
VERSION=0.6.1
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-darwin-amd64.tar.gz
tar xzf agr-${VERSION}-darwin-amd64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-darwin-amd64.tar.gz
```

**Linux（x86_64）**

```bash
VERSION=0.6.1
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-linux-amd64.tar.gz
tar xzf agr-${VERSION}-linux-amd64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-linux-amd64.tar.gz
```

**Linux（ARM64）**

```bash
VERSION=0.6.1
curl -fLO https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-linux-arm64.tar.gz
tar xzf agr-${VERSION}-linux-arm64.tar.gz
sudo mv agr /usr/local/bin/agr
rm agr-${VERSION}-linux-arm64.tar.gz
```

**Windows（x86_64）— PowerShell**

```powershell
$VERSION = "0.6.1"
Invoke-WebRequest -Uri "https://github.com/TencentCloudAgentRuntime/ags-cli/releases/download/v${VERSION}/agr-${VERSION}-windows-amd64.zip" -OutFile "agr-${VERSION}-windows-amd64.zip"
Expand-Archive "agr-${VERSION}-windows-amd64.zip" -DestinationPath .
Move-Item agr.exe "$env:USERPROFILE\bin\agr.exe"
Remove-Item "agr-${VERSION}-windows-amd64.zip"
```

> 请确保 `$env:USERPROFILE\bin` 在 `PATH` 中。

### 校验下载文件

```bash
VERSION=0.6.1
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

Deployment 是由 Sandbox Tool 支撑、具备稳定数据面 authority 的远端托管资源。
嵌套配置可以直接传 JSON，也可以用 `@file` 或 `-` 从文件、stdin 读取：

```bash
deployment_id=$(agr deployment create \
  --deployment-name workspace-service \
  --tool-id "$tool_id" \
  --scaling-configuration '{"MinInstanceCount":0,"MaxInstanceCount":10,"MaxInstanceRequestConcurrency":100}' \
  --lifecycle-configuration '{"IdleTimeoutSeconds":300,"IdleAction":"PAUSE"}' \
  -o json --jq '.Data.Deployment.DeploymentId')

agr deployment get "$deployment_id"
agr deployment list
agr deployment update "$deployment_id" \
  --tags '[{"Key":"env","Value":"test"}]'
```

create、get、update 的 text 输出采用类似 Kubernetes describe 的详情视图；
list 固定展示 `ID NAME TOOL STATUS SCALING LIFECYCLE AFFINITY AGE`，其中
`AGE` 是从创建时间起算的时长。需要完整 API 响应时使用 `-o json`。

delete 默认等待异步删除完成，每秒查询一次，直到 Deployment 消失或进入
`DELETE_FAILED`。默认超时为 10 分钟；`--wait=false` 会在删除请求被接受后
立即返回，`--timeout 0` 表示不设等待期限：

```bash
agr deployment delete "$deployment_id"
agr deployment delete "$deployment_id" --wait=false
```

`agr deployment proxy` 支持 HTTP、SSE、WebSocket，是七层代理而不是原始
TCP 隧道，明确只推荐用于本地调试。默认监听 `127.0.0.1`；改为非 loopback
地址时会输出暴露风险提示。代理按需获取 Deployment credential，只在内存中
刷新；业务 header 会被保留，`X-Access-Token` 会被覆盖，非空 `Origin` 会
规范化为远端 Deployment authority。

```bash
agr deployment proxy "$deployment_id" 8080
agr deployment proxy "$deployment_id" 3000:8080
```

## 控制面端点与数据面域名

| Flag | 默认值 | 作用对象 |
|---|---|---|
| `--cloud-endpoint` | `ags.tencentcloudapi.com` | 控制面 API 端点 |
| `--domain` | `tencentags.com` | 数据面域名（browser、exec 等数据通道）|

`--cloud-endpoint` 同时作用于常规资源命令与 `agr api call`；`--domain`
仅影响数据面访问。两者也可通过 `~/.agr/config.toml` 中的
`cloud_endpoint` / `domain` 字段，或 `AGR_CLOUD_ENDPOINT` /
`AGR_DOMAIN` 环境变量设置。两个开关相互独立。

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
agr deployment delete <id>       删除并等待 Deployment
agr deployment proxy <id> PORT   本地调试七层代理

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
--debug           将调试信息写到 stderr
```

环境变量：`TENCENTCLOUD_SECRET_ID`、`TENCENTCLOUD_SECRET_KEY`、
`AGR_OUTPUT`、`AGR_REGION`、`AGR_CLOUD_ENDPOINT`、`AGR_DOMAIN`、
`AGR_NON_INTERACTIVE`、`AGR_DEBUG`、`NO_COLOR`。

`AGR_OUTPUT` 适合作为 `text` 或 `json` 的默认输出配置。若要获取流式事件，请在
`agr instance code run --stream` 或 `agr instance exec --stream` 上显式传
`-o ndjson`。

配置优先级：`--flag` > 环境变量 > `~/.agr/config.toml` > 默认值。使用 `agr status` 查看当前生效值及其来源。

## 故障排查

```bash
agr status
agr doctor
agr explain AUTH_FAILED
agr schema instance.create -o json
```

## License

详见 [LICENSE](LICENSE-AGR%20CLI.txt)。
