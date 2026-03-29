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
- **Port Forwarding**: Forward sandbox ports to localhost, with full HTTP and WebSocket support
- **Mobile Sandbox ADB Access**: Secure ADB access to remote Android sandboxes via WebSocket tunnels
- **Interactive REPL**: Built-in interactive mode with auto-completion
- **Streaming Output**: Real-time output streaming for long-running code

## Installation

### Using go install

```bash
go install github.com/TencentCloudAgentRuntime/ags-cli@latest
```

**Note**: The installed command will be `ags-cli`. If you prefer to use `ags` as the command name, you can create an alias:

```bash
# Add to your shell configuration file (~/.zshrc, ~/.bashrc, etc.)
alias ags='ags-cli'

# Reload your shell configuration
source ~/.zshrc  # or source ~/.bashrc
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

### Architecture: Control Plane vs Data Plane

AGS CLI separates operations into two layers:

- **Control Plane**: Instance lifecycle management (create/delete/list), tool management, API key management
  - E2B Backend: Uses API Key + E2B REST API
  - Cloud Backend: Uses AKSK + Tencent Cloud API
  
- **Data Plane**: Code execution, shell commands, file operations
  - Both backends use the same E2B-compatible data plane gateway with Access Token

The `backend` configuration only affects control plane operations. Data plane operations always use the E2B protocol via `ags-go-sdk`.

Access tokens are automatically cached in `~/.ags/tokens.json` during instance creation and used for subsequent data plane operations

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

## Port Forwarding

`ags proxy` forwards a remote sandbox port to localhost, similar to `kubectl port-forward`. Both HTTP and WebSocket protocols are fully supported.

```bash
# Forward sandbox port 8080 to localhost:8080
ags proxy sandbox-xxx 8080

# Forward sandbox port 8080 to a different local port
ags proxy sandbox-xxx 3000:8080
```

> **Note**: Before using this command, open the target port in the AGS sandbox console: navigate to your sandbox instance → **Network** → **Open Port**, and add the remote port number to the allowlist. Requests to ports that have not been configured will be rejected by the gateway.

## Mobile Sandbox (ADB Access)

For **mobile** type sandboxes (Android), AGS CLI provides secure ADB access via WebSocket tunnels. This allows you to use standard `adb` commands to interact with remote Android sandbox instances.

### Prerequisites

- A mobile type sandbox tool (e.g., Android 13 sandbox)
- Local `adb` installed ([Android SDK Platform Tools](https://developer.android.com/tools/releases/platform-tools))

### Workflow

```bash
# Step 1: Create a mobile sandbox instance
ags instance create -t <mobile-tool-name>
# ✓ Instance created: 8d7a3c17ef84******************************e73c58

# Step 2: Connect to the mobile sandbox via ADB tunnel
ags mobile connect 8d7a3c17ef84******************************e73c58
# connected to 127.0.0.1:61876
# ℹ connected to 8d7a3c17ef84******************************e73c58 (127.0.0.1:61876)
# ℹ tunnel log: /Users/<user>/.ags/tunnel-8d7a3c17ef84******************************e73c58.log

# Step 3: List active mobile connections and verify ADB device
ags mobile list
# SANDBOX                                   ADB ADDRESS        STATUS
# 8d7a3c17ef84******************************e73c58  127.0.0.1:61876    connected
adb devices
# List of devices attached
# 127.0.0.1:61876    device

# Step 4: Now you can use any native adb commands (shell, install, push, pull, screencap, etc.)
adb -s 127.0.0.1:61876 shell getprop ro.build.display.id

# Step 5: Disconnect when done
ags mobile disconnect 8d7a3c17ef84******************************e73c58
# ℹ disconnected from 8d7a3c17ef84******************************e73c58

# Or disconnect all active connections at once
ags mobile disconnect --all
```

> **Note**: The `ags mobile` commands are only applicable to **mobile** type sandbox instances (e.g., Android sandboxes). They do not apply to regular code execution sandboxes.

## Command Reference

For detailed documentation on each command, see:

| Command | Aliases | Description | Documentation |
|---------|---------|-------------|---------------|
| `tool` | `t` | Tool management | [ags-tool](docs/ags-tool.md) |
| `instance` | `i` | Instance management | [ags-instance](docs/ags-instance.md) |
| `run` | `r` | Code execution | [ags-run](docs/ags-run.md) |
| `exec` | `x` | Shell command execution | [ags-exec](docs/ags-exec.md) |
| `file` | `f`, `fs` | File operations | [ags-file](docs/ags-file.md) |
| `proxy` | - | Port forwarding | [ags-proxy](docs/ags-proxy.md) |
| `mobile` | `m` | Mobile sandbox ADB access | [ags-mobile](docs/ags-mobile.md) |
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
