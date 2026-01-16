# ags-run

在沙箱中执行代码

## 概要

```
ags run [选项]
ags r [选项]
```

## 描述

在隔离的沙箱环境中执行代码。支持多种编程语言，包括 Python、JavaScript、TypeScript、R、Java 和 Bash。

代码可以通过以下方式提供：
- `-c` 选项（内联代码）
- `-f` 选项（文件路径）
- 标准输入（管道）
- 交互式编辑器（未提供输入时）

## 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `-c, --code` | string | - | 要执行的代码 |
| `-f, --file` | string | - | 包含代码的文件（可重复） |
| `-l, --language` | string | `python` | 语言：python, javascript, typescript, r, java, bash |
| `-s, --stream` | bool | `false` | 实时流式输出 |
| `-n, --repeat` | int | `1` | 运行相同代码 N 次 |
| `-p, --parallel` | bool | `false` | 并行执行任务 |
| `--max-parallel` | int | `0` | 最大并行数（0 = 无限制） |
| `-t, --tool` | string | `code-interpreter-v1` | 临时实例使用的工具 |
| `--instance` | string | - | 使用现有实例 ID |
| `--keep-alive` | bool | `false` | 保持临时实例存活 |
| `--time` | bool | `false` | 显示耗时 |

## 示例

### 基本执行

```bash
# 执行内联 Python 代码
ags run -c "print('Hello, World!')"

# 从文件执行
ags run -f script.py

# 从管道执行
echo "print('Hello')" | ags run

# 打开编辑器编写代码
ags run
```

### 不同语言

```bash
# JavaScript
ags run -l javascript -c "console.log('Hello')"

# TypeScript
ags run -l typescript -c "const x: number = 42; console.log(x)"

# Bash
ags run -l bash -c "echo Hello && pwd"

# R
ags run -l r -c "print('Hello from R')"

# Java
ags run -l java -c "System.out.println(\"Hello\");"
```

### 流式输出

```bash
# 实时流式输出
ags run -s -c "
import time
for i in range(5):
    print(f'Processing {i}...')
    time.sleep(1)
"
```

### 实例管理

```bash
# 使用现有实例
ags run --instance sbi-xxxxxxxx -c "print('Hello')"

# 保持临时实例存活
ags run --keep-alive -c "print('Hello')"
# 输出: Created instance: sbi-xxxxxxxx (kept alive)
```

### 并发执行

```bash
# 并行运行相同代码 5 次
ags run -c "print('hello')" -n 5 -p

# 顺序运行相同代码 5 次
ags run -c "print('hello')" -n 5

# 并行执行多个文件
ags run -f a.py -f b.py -f c.py -p

# 限制并行数
ags run -f a.py -f b.py -f c.py -p --max-parallel 2

# 组合：每个文件运行 2 次，全部并行
ags run -f a.py -f b.py -n 2 -p
```

### JSON 输出

```bash
# 获取 JSON 输出
ags run -o json -c "print('hello')"

# 带计时的 JSON
ags run -o json --time -c "print('hello')"
# 输出: {"stdout": [...], "timing": {"total_ms": 1234, "create_ms": 800, "exec_ms": 434}}
```

## 输出行为

- **文本模式** (`-o text`)：每个任务完成时打印结果
- **JSON 模式** (`-o json`)：所有任务完成后收集并输出单个 JSON 对象

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [ags-exec](ags-exec-zh.md) - Shell 命令执行
- [ags-instance](ags-instance-zh.md) - 实例管理
