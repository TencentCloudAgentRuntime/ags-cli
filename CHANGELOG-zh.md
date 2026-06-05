# 更新日志

本项目的所有重要更改都将记录在此文件中。

## [0.6.1] - 2026-06-05

### 破坏性变更
- 本版本没有破坏性变更。

### 新功能
- 本次发布无新功能。

### Bug 修复
- 修复 `agr instance debug` JSON flags（`--mount-options`、`--metadata`、`--custom-configuration`）现在正确支持 `@file` 和 `-`（标准输入）。
- 修复 `agr instance debug` 文本输出中 `ToolID` 字段显示错误（显示输入的 flag 值而非新建的 debug tool ID）。
- 修复 `agr instance debug --tool-name` 使用了错误的 API 字段名，现在正确使用 `Filters` 查询。
- 修复 `agr instance debug` 传入无效 JSON 时显示 `internal error`，现在显示 `INVALID_JSON_FLAG` 并附带明确提示。
- 修复 `agr instance login` 传入不存在的实例 ID 时显示 `internal error`，现在正确显示 `INSTANCE_NOT_FOUND`。
- 修复 README 手动安装命令中文件名版本号错误（`agr-0.5.0-*` 应为 `agr-0.6.1-*`）。

### 文档
- 更新 README 中 `agr instance debug` 示例，使用 `--tool-id`/`--tool-name` flag 并移除不存在的 `--debug-tool-name` flag。

## [0.6.0] - 2026-06-02

### 破坏性变更
- 本版本没有破坏性变更。

### 新功能
- 通过 `--token`、`TENCENTCLOUD_TOKEN` 和 `[auth].token` 支持腾讯云 STS 临时凭证认证。
- `agr instance debug --tool-id <id>` — 基于现有沙箱工具创建临时 Debug Tool，自动挂载 envd 镜像并等待就绪后启动实例。
- `agr tool fork <source-tool-id> --tool-name <new-name>` — 通过复制现有工具的可创建配置来新建工具，支持 flag 覆盖。
- `agr instance list --all` — 自动翻页获取当前 region 的所有实例，输出中包含 region 信息。
- 复杂 flag（`--filters`、`--tags`、`--network-configuration`、`--storage-mounts` 等）在 help 中新增 Format、Values 和 Examples 说明。
- 命令 help 新增分组 `Example - ...:` 示例区块，`agr schema` 也暴露命令示例及复杂 flag 的格式/示例/取值元数据。

### Bug 修复
- 服务端错误现在在 CLI 输出中同时显示 `RequestId`、`Code` 和 `Message`，便于联系腾讯云支持时快速定位问题。
- `agr instance login` 现在明确报告 PTY/envd 数据面会话失败，不再将其折叠为通用内部错误；用户输入 `exit` 时命令会透传远端 shell 的退出码，不再渲染错误信息（与 `ssh` 行为一致）。
- `agr version` 现在能从 Go module 伪版本中提取 commit hash 和构建时间（适用于 `go install @latest` 安装的二进制），并对 `go install <module>@<tag>` 构建的二进制显示 `n/a (go install)` 而非 `unknown`。
- `agr instance file upload`/`download` 收到 flag 格式路径参数时，错误提示改为具体的 positional 用法说明。
- `agr tool fork` 和 `agr instance debug` 在复制工具时过滤掉 `qcs` 前缀的内部标签。
- `agr schema instance.debug` 的元数据（`Mutation`、`CreatesResource`、`RequiresAuth`）现在正确返回 `true`。

### 文档
- 更新安装文档，推荐使用预编译 release 二进制，并说明 `go install` 构建的版本元数据行为。

## [0.5.1] - 2026-05-29

### 破坏性变更
- 本版本没有破坏性变更。

### 新增
- 新增一键安装脚本（`install.sh`），并将其包含在 release artifacts 中。

### 修复
- 本版本没有 BugFix。

### 文档
- 新增 Linux、macOS 和 Windows 的分平台安装说明。

## [0.5.0] - 2026-05-22

### 破坏性变更
- 0.5.0 相比 0.4.0 并不向后兼容。命令组织、flag 集合、request
  输入方式以及机器可读输出契约都在这一版里发生了明显变化。
- 现有 shell 脚本、封装层、CI 调用和任何依赖 `agr` 的自动化，
  升级前都需要重新检查。
- 如果你消费 JSON 或 NDJSON 输出，也应当基于新的
  `agr.v1` / `agr.events.v1` 契约重新验证解析逻辑。

### 新增
- CLI 被重新整理为更清晰的资源导向命令树，同时补齐了更一致的
  help 文案和机器可读命令元数据。
- 新增一组更适合日常使用的辅助命令：
  `agr schema`、`agr doctor`、`agr explain`、
  `agr config {show,set,path}`。
- `agr instance code run` 与 `agr instance exec` 新增临时沙箱工作流，
  支持 `--create-temp-instance`、清理策略控制，以及 JSON 输出中的
  `Data.ExecutionContext` 生命周期描述。
- 新增原始控制面 escape hatch `agr api call <Action> --request ...`，
  方便高级调试和未映射能力访问。
- 控制面 endpoint（`--cloud-endpoint`）与数据面 domain（`--domain`）
  的职责现在更加清晰分离。
- CLI 的 JSON 输出与流式行为整体更加一致，失败信息和执行上下文也更丰富。

### 修复
- 本版本没有 BugFix。

### 文档
- 更新 README 和 changelog，补充 `agr` 命令表面、`0.5.0` 迁移说明
  以及更新后的 quick start 指引。
- 迁移现有自动化时，建议优先参考 `agr --help` 和
  `agr schema <command> -o json`。

## [0.4.0] - 2026-04-28

### 新增
- `Instance` 类型新增后端无关的 `Secure` 标识（Cloud 后端：`Secure = AuthMode != "NONE"`；E2B 后端：`Secure = envdAccessToken != ""`）；当实例不安全（无需 token）时，`ags instance login` 会跳过访问令牌的获取，并省略 `X-Access-Token` 请求头与 webshell URL 中的 `access_token` 查询参数，`ags instance create` 也不再因缓存令牌失败而报警告
- 为 `ags instance create` / `ags instance start` 新增 `--auth-mode` 参数，取值 `DEFAULT`、`TOKEN`、`NONE`、`PUBLIC`；云端后端直接透传为 `AuthMode`，E2B 后端会自动转换为 `secure` + `network.allowPublicTraffic` 两个请求字段

### 变更
- 升级腾讯云 SDK（`tencentcloud-sdk-go/tencentcloud/ags` 与 `common`）至 v1.3.87，以获得沙箱实例新增的 `AuthMode` 字段

### 修复
- 修复 `mobile connect` 仅显示通用错误 "tunnel process exited without ready message" 而非实际错误信息的问题；daemon 子进程现在通过 stdout 发送错误详情，使父进程能向用户展示真实错误原因

## [0.3.1] - 2026-03-18

### 修复
- 将隧道子进程 stderr 重定向到 `~/.ags/tunnel-<id>.log`，防止后台重连日志污染用户终端
- 添加最大连续拨号失败次数限制，在沙箱已删除或 token 过期时停止无限重连
- 重连同一沙箱时先断开旧 ADB 地址，防止出现过期的离线设备
- `adb connect` 后等待 ADB 协议握手完成，避免首次执行命令时出现 "error: closed" 错误
- 移除 `mobile list` 中的 TCP 端口探测，防止抢占活跃 ADB 会话；改用基于 PID 的僵尸进程检测

## [0.3.0] - 2026-03-17

### 新增
- 新增 `mobile` 命令组（`ags mobile`），包含 `connect`、`disconnect`、`list`、`adb`、`tunnel` 子命令，通过加密 WebSocket 隧道安全访问远程 Android 沙箱的 ADB
- 为 `instance login` 命令添加 `--mode` 参数，支持 `pty`（默认）和 `webshell` 两种模式；PTY 模式在当前终端中直接开启原生终端会话，无需浏览器或 ttyd 二进制文件
- 新增移动端 ADB 命令的中英文文档

### 修复
- 修复 `instance create --tool-id` 未传递给 Cloud 后端 API 的问题；现在指定 ToolID 时优先使用 ToolID 而非 ToolName

## [0.2.1] - 2026-03-13

### 变更
- 扩展支持的工具类型，从 `code-interpreter` 和 `browser` 扩展为同时支持 `mobile`、`osworld`、`custom`、`swebench`

## [0.2.0] - 2026-03-09

### 新增
- 为 `exec`、`file` 和 `instance login` 命令添加 `--user` 参数，支持指定数据面操作的用户身份（默认值: "user"）
- 在 config.toml 中添加 `sandbox.default_user` 配置项，支持全局设置默认用户
- 新增顶层统一配置字段 `region`、`domain`、`internal`，替代后端特定的重复配置
- 新增全局 CLI 参数 `--region`、`--domain`、`--internal`
- 新增环境变量 `AGS_REGION`、`AGS_DOMAIN`、`AGS_INTERNAL`
- 新增独立配置参考文档（`docs/ags-config.md`）

### 变更
- 统一 region/domain/internal 配置：所有数据面和控制面操作现在从顶层配置字段读取，不再分别从 `[e2b]` 或 `[cloud]` 段获取
- 控制面客户端（`CloudControlPlane`、`E2BControlPlane`）现使用统一配置的 region 和 domain
- 在配置解析阶段将 `internal` 标志归一化到 `domain` 中：当 `internal=true` 时，domain 自动加上 `internal.` 前缀（如 `internal.tencentags.com`），确保 E2B 和 Cloud 后端的 endpoint 拼接一致

### 废弃
- 配置字段 `e2b.region`、`e2b.domain`、`cloud.region`、`cloud.internal` 已废弃，请使用顶层 `region`、`domain`、`internal`
- CLI 参数 `--e2b-region`、`--e2b-domain`、`--cloud-region`、`--cloud-internal` 已废弃，请使用 `--region`、`--domain`、`--internal`
- 环境变量 `AGS_E2B_REGION`、`AGS_E2B_DOMAIN`、`AGS_CLOUD_REGION`、`AGS_CLOUD_INTERNAL` 已废弃，请使用 `AGS_REGION`、`AGS_DOMAIN`、`AGS_INTERNAL`

## [0.1.2] - 2026-02-11

### 变更
- E2B 后端现支持通过 GET /sandboxes/{id} 获取 token，不再限制 token 仅在创建实例时可用
- 统一 Cloud 和 E2B 两种后端在 token 缓存缺失时的恢复逻辑

## [0.1.1] - 2026-01-20

### 变更
- 分离控制面和数据面，添加 token 缓存机制

## [0.1.0] - 2026-01-16

### 新增
- 初始发布
- 更新模块路径为 github.com/TencentCloudAgentRuntime/ags-cli
- 将所有 git.woa.com 引用替换为 github.com/TencentCloudAgentRuntime/ags-go-sdk v0.0.10
