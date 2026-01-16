# ags-run

Execute code in sandbox

## Synopsis

```
ags run [flags]
ags r [flags]
```

## Description

Execute code in an isolated sandbox environment. Supports multiple programming languages including Python, JavaScript, TypeScript, R, Java, and Bash.

Code can be provided via:
- `-c` flag (inline code)
- `-f` flag (file path)
- Standard input (pipe)
- Interactive editor (when no input provided)

## Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-c, --code` | string | - | Code to execute |
| `-f, --file` | string | - | File(s) containing code (repeatable) |
| `-l, --language` | string | `python` | Language: python, javascript, typescript, r, java, bash |
| `-s, --stream` | bool | `false` | Stream output in real-time |
| `-n, --repeat` | int | `1` | Run same code N times |
| `-p, --parallel` | bool | `false` | Execute tasks in parallel |
| `--max-parallel` | int | `0` | Maximum parallel executions (0 = unlimited) |
| `-t, --tool` | string | `code-interpreter-v1` | Tool for temporary instance |
| `--instance` | string | - | Use existing instance ID |
| `--keep-alive` | bool | `false` | Keep temporary instance alive |
| `--time` | bool | `false` | Print elapsed time |

## Examples

### Basic Execution

```bash
# Execute inline Python code
ags run -c "print('Hello, World!')"

# Execute from file
ags run -f script.py

# Execute from pipe
echo "print('Hello')" | ags run

# Open editor to write code
ags run
```

### Different Languages

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

### Streaming Output

```bash
# Stream output in real-time
ags run -s -c "
import time
for i in range(5):
    print(f'Processing {i}...')
    time.sleep(1)
"
```

### Instance Management

```bash
# Use existing instance
ags run --instance sbi-xxxxxxxx -c "print('Hello')"

# Keep temporary instance alive
ags run --keep-alive -c "print('Hello')"
# Output: Created instance: sbi-xxxxxxxx (kept alive)
```

### Concurrent Execution

```bash
# Run same code 5 times in parallel
ags run -c "print('hello')" -n 5 -p

# Run same code 5 times sequentially
ags run -c "print('hello')" -n 5

# Execute multiple files in parallel
ags run -f a.py -f b.py -f c.py -p

# Limit parallel executions
ags run -f a.py -f b.py -f c.py -p --max-parallel 2

# Combine: each file runs 2 times, all in parallel
ags run -f a.py -f b.py -n 2 -p
```

### JSON Output

```bash
# Get JSON output
ags run -o json -c "print('hello')"

# JSON with timing
ags run -o json --time -c "print('hello')"
# Output: {"stdout": [...], "timing": {"total_ms": 1234, "create_ms": 800, "exec_ms": 434}}
```

## Output Behavior

- **Text mode** (`-o text`): Results are printed as each task completes
- **JSON mode** (`-o json`): All results are collected and output as a single JSON object after all tasks complete

## See Also

- [ags](ags.md) - Main command
- [ags-exec](ags-exec.md) - Shell command execution
- [ags-instance](ags-instance.md) - Instance management
