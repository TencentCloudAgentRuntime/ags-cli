# ags

AGS CLI - Command-line tool for Tencent Cloud Agent Sandbox

## Synopsis

```
ags [command] [flags]
ags [flags]              # Enter REPL mode
```

## Description

AGS CLI provides a convenient way to manage sandbox tools, instances, and execute code in isolated environments. It supports both E2B API and Tencent Cloud API backends.

When invoked without arguments, AGS enters interactive REPL mode with auto-completion support.

## Commands

| Command | Aliases | Description |
|---------|---------|-------------|
| [tool](ags-tool.md) | `t` | Tool (sandbox template) management |
| [instance](ags-instance.md) | `i` | Sandbox instance management |
| [run](ags-run.md) | `r` | Execute code in sandbox |
| [exec](ags-exec.md) | `x` | Execute shell commands in sandbox |
| [file](ags-file.md) | `f`, `fs` | File operations in sandbox |
| [apikey](ags-apikey.md) | `ak`, `key` | API key management (cloud backend only) |
| `completion` | - | Generate shell completion scripts |
| `help` | - | Help about any command |

## Global Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--backend` | string | `e2b` | API backend: `e2b` or `cloud` |
| `--config` | string | `~/.ags/config.toml` | Config file path |
| `-o, --output` | string | `text` | Output format: `text` or `json` |
| `--e2b-api-key` | string | - | E2B API key |
| `--e2b-domain` | string | - | E2B domain |
| `--e2b-region` | string | - | E2B region |
| `--cloud-secret-id` | string | - | Tencent Cloud SecretID |
| `--cloud-secret-key` | string | - | Tencent Cloud SecretKey |
| `--cloud-region` | string | - | Tencent Cloud region |
| `--cloud-internal` | bool | `false` | Use internal endpoints |

## Configuration

### Configuration File

Create `~/.ags/config.toml`:

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

### Environment Variables

```bash
# E2B Backend
export AGS_E2B_API_KEY="your-api-key"
export AGS_E2B_DOMAIN="tencentags.com"
export AGS_E2B_REGION="ap-guangzhou"

# Cloud Backend
export AGS_CLOUD_SECRET_ID="your-secret-id"
export AGS_CLOUD_SECRET_KEY="your-secret-key"
export AGS_CLOUD_REGION="ap-guangzhou"
```

## Examples

```bash
# Enter REPL mode
ags

# List tools
ags tool list

# Execute Python code
ags run -c "print('Hello')"

# Execute shell command
ags exec "ls -la"

# Use cloud backend
ags --backend cloud tool list
```

## See Also

- [ags-tool](ags-tool.md) - Tool management
- [ags-instance](ags-instance.md) - Instance management
- [ags-run](ags-run.md) - Code execution
- [ags-exec](ags-exec.md) - Shell command execution
- [ags-file](ags-file.md) - File operations
- [ags-apikey](ags-apikey.md) - API key management
