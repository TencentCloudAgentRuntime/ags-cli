# ags-config

AGS CLI 配置参考

## 配置文件

默认配置文件位于 `~/.ags/config.toml`。可通过 `--config` 选项指定其他路径：

```bash
ags --config /path/to/config.toml
```

### 完整示例

```toml
# 后端类型："e2b" 或 "cloud"
backend = "e2b"

# 默认输出格式："text" 或 "json"
output = "text"

# API 访问地域（默认：ap-guangzhou）
# region = "ap-guangzhou"

# AGS 服务基础域名（默认：tencentags.com）
# domain = "tencentags.com"

# 使用内网端点（默认：false）
# 设为 true 时，domain 会自动加上 "internal." 前缀
# internal = false

# E2B API 配置
[e2b]
api_key = "your-e2b-api-key"

# 腾讯云 API 配置
[cloud]
secret_id = "your-secret-id"
secret_key = "your-secret-key"

# 沙箱配置
[sandbox]
default_user = "user"
```

## 配置字段

### 顶层字段

| 字段 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `backend` | string | `e2b` | API 后端：`e2b` 或 `cloud` |
| `output` | string | `text` | 输出格式：`text` 或 `json` |
| `region` | string | `ap-guangzhou` | API 访问地域 |
| `domain` | string | `tencentags.com` | AGS 服务基础域名 |
| `internal` | bool | `false` | 使用内网端点（腾讯云内网） |

### `[e2b]` 段

| 字段 | 类型 | 描述 |
|------|------|------|
| `api_key` | string | E2B API 密钥（`backend = "e2b"` 时必需） |

### `[cloud]` 段

| 字段 | 类型 | 描述 |
|------|------|------|
| `secret_id` | string | 腾讯云 SecretID（`backend = "cloud"` 时必需） |
| `secret_key` | string | 腾讯云 SecretKey（`backend = "cloud"` 时必需） |

### `[sandbox]` 段

| 字段 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `default_user` | string | `user` | 数据面操作的默认用户 |

## 环境变量

所有配置字段均可通过 `AGS_` 前缀的环境变量设置：

| 环境变量 | 配置字段 | 描述 |
|----------|----------|------|
| `AGS_BACKEND` | `backend` | API 后端 |
| `AGS_OUTPUT` | `output` | 输出格式 |
| `AGS_REGION` | `region` | 地域 |
| `AGS_DOMAIN` | `domain` | 基础域名 |
| `AGS_INTERNAL` | `internal` | 使用内网端点 |
| `AGS_E2B_API_KEY` | `e2b.api_key` | E2B API 密钥 |
| `AGS_CLOUD_SECRET_ID` | `cloud.secret_id` | 腾讯云 SecretID |
| `AGS_CLOUD_SECRET_KEY` | `cloud.secret_key` | 腾讯云 SecretKey |
| `AGS_SANDBOX_DEFAULT_USER` | `sandbox.default_user` | 默认沙箱用户 |

## 优先级

配置值按以下顺序解析（优先级从高到低）：

1. **命令行选项**（如 `--region`、`--domain`）
2. **环境变量**（如 `AGS_REGION`）
3. **配置文件**（`~/.ags/config.toml`）
4. **默认值**

## 内网模式（internal）

`internal` 字段控制 AGS CLI 是否使用腾讯云内网端点。设为 `true` 时，在配置解析阶段 `domain` 会自动加上 `internal.` 前缀。这种归一化确保所有后端的 endpoint 拼接一致。

### 工作原理

配置解析时，如果 `internal = true` 且 `domain` 尚未以 `internal.` 开头，则自动转换：

```
domain = "tencentags.com"  →  domain = "internal.tencentags.com"
```

归一化后，所有 endpoint 函数直接使用转换后的 `domain`：

| 端点 | 外网（`internal=false`） | 内网（`internal=true`） |
|------|--------------------------|------------------------|
| E2B 控制面 | `api.{region}.tencentags.com` | `api.{region}.internal.tencentags.com` |
| Cloud 控制面 | `ags.tencentcloudapi.com` | `ags.internal.tencentcloudapi.com` |
| 数据面 | `{region}.tencentags.com` | `{region}.internal.tencentags.com` |

### 等效配置

以下两种配置效果完全相同：

**使用 `internal` 标志（推荐）：**
```toml
domain = "tencentags.com"
internal = true
```

**直接设置 domain：**
```toml
domain = "internal.tencentags.com"
internal = false
```

> **注意**：如果两者同时设置（`domain = "internal.tencentags.com"` 且 `internal = true`），CLI 会检测到已有的 `internal.` 前缀，不会重复添加，不会报错。
>
> **注意**：`domain` 的值应为不含 `internal.` 前缀的基础域名。如需使用内网端点，请设置 `internal = true`，而非手动在 domain 前加 `internal.` 前缀。

## 已废弃字段

以下 `[e2b]` 和 `[cloud]` 段中的字段已废弃，请使用顶层字段替代：

| 废弃字段 | 替代字段 | 环境变量迁移 |
|----------|----------|-------------|
| `e2b.domain` | `domain` | `AGS_E2B_DOMAIN` → `AGS_DOMAIN` |
| `e2b.region` | `region` | `AGS_E2B_REGION` → `AGS_REGION` |
| `cloud.region` | `region` | `AGS_CLOUD_REGION` → `AGS_REGION` |
| `cloud.internal` | `internal` | `AGS_CLOUD_INTERNAL` → `AGS_INTERNAL` |

使用废弃字段时会输出警告到 stderr：

```
Warning: config field "e2b.region" is deprecated, please use top-level "region" instead.
```

旧字段的解析优先级：

1. 顶层字段（如已设置）
2. 当前后端对应的旧字段（基于 `backend` 值）
3. 其他后端的旧字段（静默兜底，不输出废弃警告）
4. 默认值

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [config.example.toml](../config.example.toml) - 配置文件示例
