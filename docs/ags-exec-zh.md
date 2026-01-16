# ags-exec

在沙箱中执行 Shell 命令

## 概要

```
ags exec [选项] <命令>
ags x [选项] <命令>
```

## 描述

在隔离的沙箱环境中执行 Shell 命令。支持流式输出、环境变量和工作目录配置。

## 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `-s, --stream` | bool | `false` | 实时流式输出 |
| `--instance` | string | - | 使用现有实例 ID |
| `-t, --tool` | string | `code-interpreter-v1` | 临时实例使用的工具 |
| `--keep-alive` | bool | `false` | 保持临时实例存活 |
| `--time` | bool | `false` | 显示耗时 |
| `--cwd` | string | - | 工作目录 |
| `--env` | string | - | 环境变量（KEY=VALUE，可重复） |

## 示例

### 基本执行

```bash
# 运行简单命令
ags exec "ls -la"
ags x "uname -a"

# 运行带参数的命令
ags exec "cat /etc/os-release"
```

### 流式输出

```bash
# 实时流式输出
ags exec -s "ping -c 5 localhost"

# 流式输出长时间运行的命令
ags exec -s "tail -f /var/log/syslog"
```

### 环境变量和工作目录

```bash
# 设置环境变量
ags exec --env FOO=bar --env BAZ=qux "echo \$FOO \$BAZ"

# 设置工作目录
ags exec --cwd /home/user "pwd && ls"

# 组合使用
ags exec --cwd /tmp --env DEBUG=1 "env | grep DEBUG"
```

### 实例管理

```bash
# 使用现有实例
ags exec --instance sbi-xxxxxxxx "whoami"

# 保持临时实例存活
ags exec --keep-alive "hostname"
```

### JSON 输出

```bash
# 获取 JSON 输出
ags exec -o json "ls"

# 带计时的 JSON（新建沙箱）
ags exec -o json --time "ls"
# 输出: {"stdout": "...", "exit_code": 0, "timing": {"total_ms": 2500, "create_ms": 2300, "exec_ms": 200}}

# 带计时的 JSON（使用现有实例）
ags exec -o json --time --instance sbi-xxx "ls"
# 输出: {"stdout": "...", "exit_code": 0, "timing": {"total_ms": 200}}
```

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [ags-run](ags-run-zh.md) - 代码执行
- [ags-file](ags-file-zh.md) - 文件操作
