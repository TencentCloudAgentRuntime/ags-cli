# ags-exec

Execute shell commands in sandbox

## Synopsis

```
ags exec [flags] <command>
ags x [flags] <command>
```

## Description

Execute shell commands in an isolated sandbox environment. Supports streaming output, environment variables, and working directory configuration.

## Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-s, --stream` | bool | `false` | Stream output in real-time |
| `--instance` | string | - | Use existing instance ID |
| `-t, --tool` | string | `code-interpreter-v1` | Tool for temporary instance |
| `--keep-alive` | bool | `false` | Keep temporary instance alive |
| `--time` | bool | `false` | Print elapsed time |
| `--cwd` | string | - | Working directory |
| `--env` | string | - | Environment variables (KEY=VALUE, repeatable) |

## Examples

### Basic Execution

```bash
# Run a simple command
ags exec "ls -la"
ags x "uname -a"

# Run command with arguments
ags exec "cat /etc/os-release"
```

### Streaming Output

```bash
# Stream output in real-time
ags exec -s "ping -c 5 localhost"

# Stream long-running command
ags exec -s "tail -f /var/log/syslog"
```

### Environment and Working Directory

```bash
# Set environment variables
ags exec --env FOO=bar --env BAZ=qux "echo \$FOO \$BAZ"

# Set working directory
ags exec --cwd /home/user "pwd && ls"

# Combine both
ags exec --cwd /tmp --env DEBUG=1 "env | grep DEBUG"
```

### Instance Management

```bash
# Use existing instance
ags exec --instance sbi-xxxxxxxx "whoami"

# Keep temporary instance alive
ags exec --keep-alive "hostname"
```

### JSON Output

```bash
# Get JSON output
ags exec -o json "ls"

# JSON with timing (new sandbox)
ags exec -o json --time "ls"
# Output: {"stdout": "...", "exit_code": 0, "timing": {"total_ms": 2500, "create_ms": 2300, "exec_ms": 200}}

# JSON with timing (existing instance)
ags exec -o json --time --instance sbi-xxx "ls"
# Output: {"stdout": "...", "exit_code": 0, "timing": {"total_ms": 200}}
```

## See Also

- [ags](ags.md) - Main command
- [ags-run](ags-run.md) - Code execution
- [ags-file](ags-file.md) - File operations
