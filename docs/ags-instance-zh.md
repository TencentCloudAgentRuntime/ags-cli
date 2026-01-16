# ags-instance

管理沙箱实例

## 概要

```
ags instance <子命令> [选项]
ags i <子命令> [选项]
```

## 描述

实例是从工具创建的运行中沙箱。每个实例提供一个隔离的执行环境，拥有独立的文件系统、网络和进程空间。

## 子命令

| 子命令 | 别名 | 描述 |
|--------|------|------|
| `create` | `c` | 创建新实例 |
| `start` | - | 启动实例（create 的别名） |
| `list` | `ls` | 列出实例 |
| `get` | - | 获取实例详情 |
| `login` | - | 通过 webshell 登录实例 |
| `delete` | `rm`, `del` | 删除实例 |
| `stop` | - | 停止实例（delete 的别名） |

## create / start

创建并启动新的沙箱实例。

```
ags instance create [选项]
ags instance start [选项]
ags i c [选项]
```

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `-t, --tool` | string | - | 工具名称 |
| `--tool-id` | string | - | 工具 ID（仅云端后端） |
| `--timeout` | int | `300` | 实例超时时间（秒） |
| `--mount-option` | string | - | 挂载选项覆盖（可重复） |
| `--time` | bool | `false` | 显示耗时 |

注意：必须指定 `--tool` 或 `--tool-id` 之一，但不能同时指定。

### 挂载选项格式

```
name=<名称>[,dst=<目标路径>][,subpath=<子路径>][,readonly]
```

| 参数 | 必需 | 描述 |
|------|------|------|
| `name` | 是 | 工具中定义的存储挂载名称 |
| `dst` | 否 | 覆盖目标挂载路径 |
| `subpath` | 否 | 子目录隔离路径 |
| `readonly` | 否 | 强制只读挂载 |

### 示例

```bash
# 使用工具名称创建
ags instance create -t code-interpreter-v1

# 使用工具 ID 创建
ags i c --tool-id sdt-xxxxxxxx

# 创建时设置超时（1小时）
ags instance create -t code-interpreter-v1 --timeout 3600

# 创建时覆盖挂载选项
ags instance create -t my-tool \
  --mount-option "name=data,dst=/workspace,subpath=user-123"
```

## list

列出沙箱实例。

```
ags instance list [选项]
ags i ls [选项]
```

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `-t, --tool` | string | - | 按工具 ID 过滤 |
| `-s, --status` | string | - | 按状态过滤 |
| `--short` | bool | `false` | 仅显示实例 ID |
| `--no-header` | bool | `false` | 隐藏表头 |
| `--offset` | int | `0` | 分页偏移 |
| `--limit` | int | `20` | 分页限制 |
| `--time` | bool | `false` | 显示耗时 |

### 示例

```bash
# 列出所有实例
ags instance list

# 按工具 ID 过滤
ags i ls --tool-id sdt-xxxxxxxx

# 按状态过滤
ags instance list -s Running

# 简短格式（仅 ID）
ags i ls --short

# 分页
ags instance list --offset 10 --limit 5
```

## get

获取实例详细信息。

```
ags instance get <instance-id>
```

### 示例

```bash
ags instance get sbi-xxxxxxxx
```

## login

通过基于 Web 的终端（webshell）登录沙箱实例。

```
ags instance login <instance-id> [选项]
ags i login <instance-id> [选项]
```

此命令将：
1. 验证实例存在且正在运行
2. 如果尚未运行，下载并启动 ttyd webshell 服务
   （如果指定了 --ttyd-binary，则上传自定义 ttyd 二进制文件）
3. 在默认浏览器中打开 webshell

webshell 提供可通过浏览器访问的完整终端界面，允许您直接与沙箱环境交互。

如果沙箱由于网络限制无法从 GitHub 下载 ttyd，您可以使用 --ttyd-binary 上传本地 ttyd 二进制文件。

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `--no-browser` | bool | `false` | 不自动打开浏览器 |
| `--ttyd-binary` | string | - | 要上传的自定义 ttyd 二进制文件路径 |
| `--time` | bool | `false` | 显示耗时 |

### 支持的实例类型

- `code-interpreter` - Python/代码执行环境
- `browser` - 基于浏览器的环境

### 示例

```bash
# 登录实例并自动打开浏览器
ags instance login sbi-xxxxxxxx

# 登录但不打开浏览器（手动访问 URL）
ags i login sbi-xxxxxxxx --no-browser

# 使用自定义 ttyd 二进制文件登录（适用于网络受限环境）
ags instance login sbi-xxxxxxxx --ttyd-binary /path/to/ttyd

# 登录并显示耗时信息
ags instance login sbi-xxxxxxxx --time
```

## delete / stop

删除一个或多个实例。

```
ags instance delete <instance-id> [instance-id...]
ags instance stop <instance-id> [instance-id...]
ags i rm <instance-id> [instance-id...]
```

### 示例

```bash
# 删除单个实例
ags instance delete sbi-xxxxxxxx

# 删除多个实例
ags i rm sbi-xxx sbi-yyy sbi-zzz

# 停止实例
ags instance stop sbi-xxxxxxxx
```

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [ags-tool](ags-tool-zh.md) - 工具管理
- [ags-run](ags-run-zh.md) - 代码执行
