# ags-tool

管理沙箱工具（模板）

## 概要

```
ags tool <子命令> [选项]
ags t <子命令> [选项]
```

## 描述

工具是定义运行时环境的沙箱模板。每个工具指定了沙箱实例的基础镜像、网络配置、存储挂载等设置。

## 子命令

| 子命令 | 别名 | 描述 |
|--------|------|------|
| `list` | `ls` | 列出可用工具 |
| `get` | - | 获取工具详情 |
| `create` | - | 创建新工具（仅云端后端） |
| `update` | - | 更新工具（仅云端后端） |
| `delete` | `rm`, `del` | 删除工具（仅云端后端） |

## list

列出可用工具。

```
ags tool list [选项]
```

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `--id` | string | - | 指定工具 ID（可重复） |
| `--status` | string | - | 状态过滤：CREATING, ACTIVE, DELETING, FAILED |
| `--type` | string | - | 类型过滤：code-interpreter, browser |
| `--tag` | string | - | 标签过滤（key=value，可重复） |
| `--created-since` | string | - | 相对时间过滤（如 1h, 24h） |
| `--short` | bool | `false` | 仅显示 ID 和名称 |
| `--offset` | int | `0` | 分页偏移 |
| `--limit` | int | `20` | 分页限制 |
| `--time` | bool | `false` | 显示耗时 |

### 示例

```bash
# 列出所有工具
ags tool list

# 简短格式
ags t ls --short

# 按类型过滤
ags tool list --type code-interpreter

# 按状态过滤
ags tool list --status ACTIVE

# 按标签过滤
ags tool list --tag env=prod --tag team=ai

# 分页
ags tool list --offset 20 --limit 10
```

## get

获取工具详细信息。

```
ags tool get <tool-id>
```

### 示例

```bash
ags tool get sdt-xxxxxxxx
```

## create

创建新工具（仅云端后端）。

```
ags tool create [选项]
```

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `-n, --name` | string | - | 工具名称（必需） |
| `-t, --type` | string | - | 工具类型（必需）：code-interpreter, browser |
| `-d, --description` | string | - | 工具描述 |
| `--timeout` | duration | - | 默认超时（如 5m, 1h） |
| `--network` | string | - | 网络模式：PUBLIC |
| `--tag` | string | - | 标签（key=value，可重复） |
| `--role-arn` | string | - | COS 访问的 CAM 角色 ARN |
| `--mount` | string | - | 存储挂载配置（可重复） |

### 挂载格式

```
type=cos,name=<名称>,bucket=<桶名>,src=<源路径>,dst=<目标路径>[,readonly][,endpoint=<端点>]
```

| 参数 | 必需 | 描述 |
|------|------|------|
| `type` | 是 | 存储类型：`cos` |
| `name` | 是 | 挂载名称（DNS-1123 格式） |
| `bucket` | 是 | COS 桶名 |
| `src` | 是 | 源路径（必须以 `/` 开头） |
| `dst` | 是 | 目标路径（必须以 `/` 开头） |
| `readonly` | 否 | 只读挂载 |
| `endpoint` | 否 | COS 端点 |

### 示例

```bash
# 基本创建
ags tool create -n my-tool -t code-interpreter

# 带描述和标签
ags tool create -n my-tool -t code-interpreter \
  -d "我的自定义工具" \
  --tag env=dev --tag team=ai

# 带 COS 挂载
ags tool create -n my-tool -t code-interpreter \
  --role-arn "qcs::cam::uin/100000:roleName/AGS_COS_Role" \
  --mount "type=cos,name=data,bucket=my-bucket-1250000000,src=/data,dst=/mnt/data"
```

## update

更新现有工具（仅云端后端）。

```
ags tool update <tool-id> [选项]
```

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `-d, --description` | string | - | 工具描述 |
| `--network` | string | - | 网络模式：PUBLIC, SANDBOX, INTERNAL_SERVICE |
| `--tag` | string | - | 标签（key=value，可重复） |
| `--clear-tags` | bool | `false` | 清除所有标签 |

必须指定至少一个选项。

### 示例

```bash
# 更新描述
ags tool update sdt-xxx -d "更新后的描述"

# 更新网络模式
ags tool update sdt-xxx --network SANDBOX

# 更新标签
ags tool update sdt-xxx --tag env=staging --tag team=ai

# 清除所有标签
ags tool update sdt-xxx --clear-tags
```

## delete

删除一个或多个工具（仅云端后端）。

```
ags tool delete <tool-id> [tool-id...]
ags tool rm <tool-id> [tool-id...]
```

### 示例

```bash
# 删除单个工具
ags tool delete sdt-xxx

# 删除多个工具
ags t rm sdt-xxx sdt-yyy sdt-zzz
```

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [ags-instance](ags-instance-zh.md) - 实例管理
