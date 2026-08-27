# 更新日志

本项目的所有重要更改都将记录在此文件中。

## [0.6.6] - 2026-08-27

### 破坏性变更
- 本版本没有破坏性变更。

### 新功能
- 基于官方 AGS API 元数据新增 `agr deployment create`、`get`、`list`、
  `update` 和 `delete` 命令，提供易读的文本输出、适合脚本使用的完整 JSON
  响应、列表过滤，以及删除时可选的等待能力。
- 新增 `agr deployment proxy`，用于在本地调试 HTTP、SSE 和 WebSocket；
  支持本地到远端端口映射、按需刷新 Token，以及在进程内发现、复用和轮换
  亲和会话。

### Bug 修复
- 本版本没有 Bug 修复。

### 文档
- 在 README 和 README-zh 中补充 Deployment 创建、更新、过滤、删除和本地
  代理的实用说明。

## [0.6.5] - 2026-08-21

### 破坏性变更
- 本版本没有破坏性变更。

### 新功能
- 本版本没有新功能。

### Bug 修复
- 修正机器可读命令 Schema：将更新操作标记为变更操作、将工作流命令标记为
  需要认证、允许镜像预缓存命令使用自定义镜像仓库，并让 JSON Schema 暴露与
  命令帮助一致的示例。
- `agr instance debug` 现在复用实例创建的生命周期策略；遇到已知终态时返回
  结构化等待错误，不再持续轮询到超时，并在清理临时资源后提供可执行的重试提示。
- 使 `agr instance list --filters` 的状态帮助与当前持久化状态保持一致，并说明
  派生状态 `UNHEALTHY` 的独立过滤要求和分页行为。

### 文档
- 本版本没有文档变更。

## [0.6.4] - 2026-08-06

### 破坏性变更
- 本版本没有破坏性变更。

### 新功能
- 新增 `agr identity` 和 `agr credential` 命令组，提供 17 个工作流命令，
  用于管理工作负载身份、访问令牌、凭证提供方、托管密钥和 OAuth2 授权。
- 为实例和工具生命周期命令（包括删除）新增面向具体操作的 `--wait`；等待过程
  按后端状态判断最终结果，明确报告失败或被其他状态抢占的操作，并在遇到陌生状态
  超时时附带诊断信息。
- 新增 `agr update` 和带 24 小时缓存的非阻塞后台更新检查；交互式文本命令可在
  不延迟正常执行、不污染 JSON 输出的前提下提示新版本。
- 在交互式文本终端中创建实例和工具时显示进度动画，同时保持 JSON、重定向和
  非交互输出干净。
- 新增统一错误分类，为可操作错误提供内联修复命令和
  `agr explain <CODE>` 指引。
- 刷新 AGS API 元数据，新增 `instance pause --memory` 和
  `instance resume --timeout`，并扩充存储类型、工具类型和自定义镜像仓库帮助。

### Bug 修复
- 当 `/usr/local/bin` 不可写时安装到 `$HOME/.local/bin`，同时遵循
  `INSTALL_DIR`，避免在非 root 环境中隐式调用 `sudo`。
- `instance pause` 现在报告接口实际返回的状态，不再始终声称实例已暂停。

### 文档
- 在 README 和 README-zh 中说明非 root 安装回退行为及对应的 `PATH` 配置。

## [0.6.3] - 2026-07-15

### 破坏性变更
- 本版本没有破坏性变更。

### 新功能
- 为 `agr instance delete` 和 `agr tool delete` 增加删除保护：交互式终端现在会先列出目标资源并请求确认，新增 `--dry-run` 预览删除内容，以及 `--yes`/`-y` 跳过确认；现有非交互流程仍无需确认。
- 支持通过 `brew install TencentCloudAgentRuntime/tap/agr-cli` 使用官方 Homebrew Tap，正式版本发布后会自动触发 Formula 校验和更新。
- 强化 CI：增加 GitHub Actions workflow lint、聚合 `CI Gate`，并在 Linux 和 macOS 上运行单元测试。

### Bug 修复
- 构建完全静态链接的 Linux release 二进制，避免在 Tencent Linux、CentOS 7/8 等较旧发行版上出现 `GLIBC_2.34 not found`。
- 移除 release 流水线中重复的 COS 直传步骤，统一由 GitHub release webhook 同步 COS，避免 GitHub Release 已成功但后续重复上传失败导致流水线整体报错。
- 在 live 生命周期测试中等待实例真正进入 `PAUSED` 后再执行恢复，消除 pause/resume 竞态。

### 文档
- 在 README 和 README-zh 中补充官方 Homebrew Tap 安装命令。
- 在中英文贡献指南中说明 `CI Gate` 和 `gitleaks` 是必需状态检查。

## [0.6.2] - 2026-06-12

### 破坏性变更
- 本版本没有破坏性变更。

### 新功能
- 为移动实例的 ADB tunnel 新增健康检测和自动恢复能力。不可达的 tunnel 现在会进入 degraded 状态，`mobile list` 会显示真实 tunnel 状态，`mobile list --prune` 可以清理不可达的 tunnel 记录。
- 新增 AGR CLI official 下载源 `https://dl.tencentags.com/agr-cli`，一键安装脚本默认使用该下载源，并可通过 `AGR_DOWNLOAD_MIRROR=github` 回退到 GitHub Releases。
- Release 流水线会将版本产物发布到 official 下载源，包括版本目录下的产物，以及 `latest/install.sh` 和 `latest/VERSION` 指针。

### Bug 修复
- 修复网络中断、沙箱 sleep/wake 或远端 adbd 重启后 ADB tunnel 可能挂起的问题；现在会关闭陈旧的本地 TCP 连接，让 ADB 以新的协议握手重新连接。
- 从上游 tccli API 定义同步 `api/ags/v20250920/api.json`。

### 文档
- 更新 README 和 README-zh 的安装说明，默认使用 official 下载源。

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
