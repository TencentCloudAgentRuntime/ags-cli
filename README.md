# AGS CLI

[中文文档](README-zh.md)

AGS CLI is a command-line tool for managing Tencent Cloud Agent Sandbox (AGS). It provides a convenient way to manage sandbox tools, instances, and execute code in isolated environments.

## Features

- **Tool Management**: Create, list, and delete sandbox tools (templates)
- **Instance Management**: Start/stop sandbox instances with flexible lifecycle control
- **Code Execution**: Execute code in multiple languages (Python, JavaScript, TypeScript, R, Java, Bash)
- **Shell Command Execution**: Run shell commands in sandbox with streaming support
- **File Operations**: Upload, download, and manage files in sandbox
- **Dual Backend Support**: Support both E2B API and Tencent Cloud API
- **Interactive REPL**: Built-in interactive mode with auto-completion
- **Streaming Output**: Real-time output streaming for long-running code

## Installation

### Using go install

```bash
go install github.com/TencentCloudAgentRuntime/ags-cli@latest
```

### From Source

```bash
git clone https://github.com/TencentCloudAgentRuntime/ags-cli.git
cd ags-cli
make build
```

### Cross-platform Build

```bash
make build-all  # Build for Linux, macOS, Windows
```

## Configuration

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
```

Or use environment variables:

```bash
export AGS_E2B_API_KEY="your-api-key"
export AGS_CLOUD_SECRET_ID="your-secret-id"
export AGS_CLOUD_SECRET_KEY="your-secret-key"
```

### Backend Differences

AGS CLI supports two backends with different capabilities:

| Feature | E2B Backend | Cloud Backend |
|---------|-------------|---------------|
| Authentication | API Key only | SecretID + SecretKey |
| Tool Management | ✗ | ✓ |
| Instance Operations | ✓ | ✓ |
| Code Execution | ✓ | ✓ |
| File Operations | ✓ | ✓ |
| API Key Management | ✗ | ✓ |

The **E2B configuration** provides compatibility with the E2B API. With E2B backend, you only need an API key for sandbox instance operations (create, list, delete instances, execute code, file operations), but you cannot manage sandbox tools.

To manage sandbox tools (list/get/create/update/delete) and API keys, you must use the **Cloud backend** with Tencent Cloud SecretID and SecretKey. You can obtain your AKSK from: https://console.cloud.tencent.com/cam/capi

## Quick Start

```bash
# Enter REPL mode
ags

# List available tools
ags tool list

# Create an instance
ags instance create -t code-interpreter-v1

# Execute Python code
ags run -c "print('Hello, World!')"

# Execute with streaming output
ags run -s -c "import time; [print(i) or time.sleep(1) for i in range(5)]"

# Execute shell command
ags exec "ls -la"

# Upload/download files
ags file upload local.txt /home/user/remote.txt
ags file download /home/user/file.txt ./local.txt
```

## Command Reference

For detailed documentation on each command, see:

| Command | Aliases | Description | Documentation |
|---------|---------|-------------|---------------|
| `tool` | `t` | Tool management | [ags-tool](docs/ags-tool.md) |
| `instance` | `i` | Instance management | [ags-instance](docs/ags-instance.md) |
| `run` | `r` | Code execution | [ags-run](docs/ags-run.md) |
| `exec` | `x` | Shell command execution | [ags-exec](docs/ags-exec.md) |
| `file` | `f`, `fs` | File operations | [ags-file](docs/ags-file.md) |
| `apikey` | `ak`, `key` | API key management | [ags-apikey](docs/ags-apikey.md) |

See [ags](docs/ags.md) for global options and configuration details.

### Man Pages

Generate and install man pages for offline documentation:

```bash
# Generate man pages
make man

# Install to system (requires sudo)
make install-man

# View documentation
man ags
man ags-tool
man ags-instance
```

## Shell Completion

```bash
# Bash
ags completion bash > /etc/bash_completion.d/ags

# Zsh
ags completion zsh > "${fpath[1]}/_ags"

# Fish
ags completion fish > ~/.config/fish/completions/ags.fish
```

## License

This project is open-sourced under the Apache License 2.0. See [LICENSE](LICENSE-AGS%20CLI.txt) file for details.
