# 更新日志

本项目的所有重要更改都将记录在此文件中。

## [0.5.0] - 2026-05-12

### 破坏性变更
- **退出码现在反映远程执行失败**：`ags run` 之前在远程代码执行失败时仍返回退出码 0，现在返回退出码 1。多任务模式在部分失败时返回退出码 1，全部失败时返回退出码 2。`ags exec` 也改为通过 `exitCodeError` 返回正确的退出码，而非直接调用 `os.Exit()`（此前会跳过 defer 清理逻辑）。
- **stderr/stdout 分离**：远程 stderr 输出、错误堆栈追踪以及文件操作提示（如 `✓ Uploaded ...`）现在写入本地 **stderr** 而非 stdout。依赖 stdout 解析这些消息的脚本需要相应修改。这意味着 `ags run ... | jq .` 现在可以正确工作，stdout 上不再有非 JSON 内容。
- **无效的 `--backend` 值现在被拒绝**：之前未知的 `--backend` 值（如 `--backend foo`）会静默回退到 E2B 后端，现在将返回错误：`invalid backend: foo (must be 'e2b' or 'cloud')`。
- **无效的 `--output` 值现在被提前拒绝**：`--output yaml` 或任何非 `text`/`json` 的值在任何命令执行前即被拒绝（通过 `PersistentPreRunE` 中的 `ValidateBasics()` 实现）。
- **非 TTY stdin 不再进入 REPL**：向 `ags` 管道输入（如 `echo test | ags`）现在会打印帮助信息并退出，而不是进入 REPL 模式（此前会导致 `go-prompt` panic）。
- **根命令启用 `SilenceUsage` / `SilenceErrors`**：运行时错误不再打印完整的 usage/help 文本，仅向 stderr 输出错误信息。

### 新增
- `E2B_API_KEY` 环境变量现在与 `AGS_E2B_API_KEY` 一同被识别（通过 `viper.BindEnv` 实现），兼容 E2B 工具链
- 配置文件权限告警：当 `~/.ags/config.toml` 包含凭证且对 group 或 others 可读时，向 stderr 输出警告（建议执行 `chmod 600`）
- 在网络调用前进行输入校验：`run` 校验 `--language`、`--repeat`、`--max-parallel`、`--instance`/`--tool` 互斥性；`mobile` 校验端口范围 [0–65535] 和 `--all`/device 互斥性；`browser` 校验 `--timeout > 0`；`instance create` 校验 `--timeout > 0`
- 新增 `cmd/sandbox_helper.go` 中的 `GetOrCreateSandboxForDataPlane()`：为所有数据面命令（`run`、`exec`、`file`）提供统一的控制面实例创建路径，与 `ags instance create` 行为一致
- 统一 token 缓存连接层：`ConnectWithToken()` 和 `ConnectSandboxWithCache()` 确保 token 被缓存和复用
- 更丰富的 E2B 错误格式：`e2bHTTPError()` 解析 JSON 错误负载中的 code/message 字段，不再输出原始 HTTP body
- 测试：`cmd/run_test.go`（5 个 `validateRunFlags` 测试用例）、`internal/client/interface_test.go`（编译期接口一致性检查）、`internal/config/config_test.go`（4 个 `ValidateBasics` 测试用例、5 个 `configFileContainsCredentials` 测试用例）

### 变更
- 数据面命令（`run`、`exec`、`file`）现在通过配置的控制面后端创建临时实例，而非直接调用 cloud SDK；修复了 E2B 后端下 `ags run -c "print('hello')"` 意外请求 Cloud API 导致签名失败的问题
- `file list --depth` 参数现在实际传递给 SDK 的 `filesystem.ListConfig{Depth: ...}`（之前声明了该参数但未实际使用）
- E2B 后端的 `instance list --limit/--offset` 现在执行客户端截断（E2B API 不支持服务端分页）
- 帮助文本：根命令示例从 `ags tool list`（在默认 E2B 后端下会失败）改为 `ags --backend cloud tool list`
- 文档修复：`docs/ags-file.md` / `docs/ags-file-zh.md` 参数说明更正；`docs/ags.md` / `docs/ags-zh.md` 示例更新；`docs/ags-config.md` / `docs/ags-config-zh.md` 新增 `sandbox.default_user` 和 `E2B_API_KEY` 文档
- 破坏性操作命令（`apikey delete`、`instance delete/stop`、`tool delete`）的 `--help` 中现在注明会立即执行，不会提示确认

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
