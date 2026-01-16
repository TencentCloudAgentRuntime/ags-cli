# ags-apikey

管理 API 密钥（仅云端后端）

## 概要

```
ags apikey <子命令> [选项]
ags ak <子命令> [选项]
ags key <子命令> [选项]
```

## 描述

创建、列出和删除 AGS 云端后端的 API 密钥。API 密钥提供了 SecretID/SecretKey 之外的另一种认证方式。

**注意**：此命令仅在使用云端后端时可用（`--backend cloud`）。

## 子命令

| 子命令 | 别名 | 描述 |
|--------|------|------|
| `create` | - | 创建新的 API 密钥 |
| `list` | `ls` | 列出 API 密钥 |
| `delete` | `rm`, `del` | 删除 API 密钥 |

## create

创建新的 API 密钥。

```
ags apikey create [选项]
```

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `-n, --name` | string | - | API 密钥名称（必需） |

### 示例

```bash
# 创建 API 密钥
ags apikey create -n my-key

# 使用云端后端创建
ags --backend cloud apikey create -n production-key
```

## list

列出所有 API 密钥。

```
ags apikey list
ags ak ls
```

### 示例

```bash
# 列出所有 API 密钥
ags apikey list

# 以 JSON 格式列出
ags ak ls -o json
```

## delete

删除 API 密钥。

```
ags apikey delete <key-id>
ags ak rm <key-id>
```

### 示例

```bash
# 删除 API 密钥
ags apikey delete ak-xxxxxxxx

# 删除并确认
ags ak rm ak-xxxxxxxx
```

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [ags-tool](ags-tool-zh.md) - 工具管理
