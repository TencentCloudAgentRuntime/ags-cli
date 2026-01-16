# ags

AGS CLI - 腾讯云智能体沙箱命令行工具

## 概要

```
ags [命令] [选项]
ags [选项]              # 进入 REPL 模式
```

## 描述

AGS CLI 提供了一种便捷的方式来管理沙箱工具、实例，并在隔离环境中执行代码。支持 E2B API 和腾讯云 API 两种后端。

不带参数调用时，AGS 会进入带自动补全的交互式 REPL 模式。

## 命令

| 命令 | 别名 | 描述 |
|------|------|------|
| [tool](ags-tool-zh.md) | `t` | 工具（沙箱模板）管理 |
| [instance](ags-instance-zh.md) | `i` | 沙箱实例管理 |
| [run](ags-run-zh.md) | `r` | 在沙箱中执行代码 |
| [exec](ags-exec-zh.md) | `x` | 在沙箱中执行 Shell 命令 |
| [file](ags-file-zh.md) | `f`, `fs` | 沙箱文件操作 |
| [apikey](ags-apikey-zh.md) | `ak`, `key` | API 密钥管理（仅云端后端） |
| `completion` | - | 生成 Shell 补全脚本 |
| `help` | - | 获取命令帮助 |

## 全局选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `--backend` | string | `e2b` | API 后端：`e2b` 或 `cloud` |
| `--config` | string | `~/.ags/config.toml` | 配置文件路径 |
| `-o, --output` | string | `text` | 输出格式：`text` 或 `json` |
| `--e2b-api-key` | string | - | E2B API 密钥 |
| `--e2b-domain` | string | - | E2B 域名 |
| `--e2b-region` | string | - | E2B 地域 |
| `--cloud-secret-id` | string | - | 腾讯云 SecretID |
| `--cloud-secret-key` | string | - | 腾讯云 SecretKey |
| `--cloud-region` | string | - | 腾讯云地域 |
| `--cloud-internal` | bool | `false` | 使用内网端点 |

## 配置

### 配置文件

创建 `~/.ags/config.toml`：

```toml
backend = "e2b"
output = "text"

[e2b]
api_key = "your-e2b-api-key"
domain = "tencentags.com"
region = "ap-guangzhou"

[cloud]
secret_id = "your-secret-id"
secret_key = "your-secret-key"
region = "ap-guangzhou"
internal = false
```

### 环境变量

```bash
# E2B 后端
export AGS_E2B_API_KEY="your-api-key"
export AGS_E2B_DOMAIN="tencentags.com"
export AGS_E2B_REGION="ap-guangzhou"

# 云端后端
export AGS_CLOUD_SECRET_ID="your-secret-id"
export AGS_CLOUD_SECRET_KEY="your-secret-key"
export AGS_CLOUD_REGION="ap-guangzhou"
```

## 示例

```bash
# 进入 REPL 模式
ags

# 列出工具
ags tool list

# 执行 Python 代码
ags run -c "print('Hello')"

# 执行 Shell 命令
ags exec "ls -la"

# 使用云端后端
ags --backend cloud tool list
```

## 另请参阅

- [ags-tool](ags-tool-zh.md) - 工具管理
- [ags-instance](ags-instance-zh.md) - 实例管理
- [ags-run](ags-run-zh.md) - 代码执行
- [ags-exec](ags-exec-zh.md) - Shell 命令执行
- [ags-file](ags-file-zh.md) - 文件操作
- [ags-apikey](ags-apikey-zh.md) - API 密钥管理
