# ags-file

沙箱文件操作

## 概要

```
ags file <子命令> [选项]
ags f <子命令> [选项]
ags fs <子命令> [选项]
```

## 描述

管理沙箱实例中的文件。支持上传、下载、列表、删除等文件操作。

## 子命令

| 子命令 | 别名 | 描述 |
|--------|------|------|
| `list` | `ls` | 列出目录中的文件 |
| `upload` | `up`, `put` | 上传文件到沙箱 |
| `download` | `down`, `get` | 从沙箱下载文件 |
| `cat` | - | 打印文件内容到标准输出 |
| `stat` | - | 获取文件或目录信息 |
| `mkdir` | - | 创建目录 |
| `remove` | `rm`, `del` | 删除文件或目录 |

## 通用选项

这些选项适用于所有子命令：

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `--instance` | string | - | 要使用的实例 ID（未指定工具时必需） |
| `-t, --tool` | string | `code-interpreter-v1` | 临时实例使用的工具 |
| `--keep-alive` | bool | `false` | 保持临时实例存活 |
| `--time` | bool | `false` | 显示耗时 |

## list

列出目录中的文件。

```
ags file list <路径> [选项]
ags f ls <路径> [选项]
```

### 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `--depth` | int | `1` | 目录深度（1 = 仅当前目录） |

### 示例

```bash
# 列出文件
ags file ls /home/user

# 带深度列出
ags f ls /home/user --depth 2
```

## upload

上传文件到沙箱。

```
ags file upload <本地路径> <远程路径>
ags f up <本地路径> <远程路径>
```

### 示例

```bash
# 上传文件
ags file upload local.txt /home/user/remote.txt

# 上传到现有实例
ags f up data.csv /data/input.csv --instance sbi-xxx
```

## download

从沙箱下载文件。

```
ags file download <远程路径> [本地路径]
ags f down <远程路径> [本地路径]
```

如果未指定本地路径，文件将以相同名称保存到当前目录。

### 示例

```bash
# 下载文件
ags file download /home/user/output.txt ./result.txt

# 下载到当前目录
ags f down /home/user/data.csv
```

## cat

打印文件内容到标准输出。

```
ags file cat <路径>
```

### 示例

```bash
# 查看文件内容
ags file cat /home/user/.bashrc

# 查看并管道到其他命令
ags f cat /etc/passwd | grep root
```

## stat

获取文件或目录信息。

```
ags file stat <路径>
```

### 示例

```bash
# 获取文件信息
ags file stat /home/user/.bashrc
```

输出包括：名称、路径、类型、大小、权限、所有者、组、修改时间。

## mkdir

创建目录。

```
ags file mkdir <路径>
```

### 示例

```bash
# 创建目录
ags file mkdir /home/user/newdir
```

## remove

删除文件或目录。

```
ags file remove <路径> [路径...]
ags f rm <路径> [路径...]
```

### 示例

```bash
# 删除单个文件
ags file rm /home/user/temp.txt

# 删除多个文件
ags f rm /tmp/a.txt /tmp/b.txt /tmp/c.txt
```

## JSON 输出

所有子命令都支持带计时信息的 JSON 输出：

```bash
# 带计时的 JSON（新建沙箱）
ags file ls /home/user -o json --time
# 输出: {"entries": [...], "timing": {"total_ms": 2500, "create_ms": 2300, "exec_ms": 200}}

# 带计时的 JSON（使用现有实例）
ags file ls /home/user -o json --time --instance sbi-xxx
# 输出: {"entries": [...], "timing": {"total_ms": 200}}
```

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [ags-exec](ags-exec-zh.md) - Shell 命令执行
- [ags-instance](ags-instance-zh.md) - 实例管理
