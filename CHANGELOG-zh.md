# 更新日志

本项目的所有重要更改都将记录在此文件中。

## [未发布]

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
