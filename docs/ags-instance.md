# ags-instance

Manage sandbox instances

## Synopsis

```
ags instance <subcommand> [flags]
ags i <subcommand> [flags]
```

## Description

Instances are running sandboxes created from tools. Each instance provides an isolated execution environment with its own filesystem, network, and process space.

## Subcommands

| Subcommand | Aliases | Description |
|------------|---------|-------------|
| `create` | `c` | Create a new instance |
| `start` | - | Start an instance (alias for create) |
| `list` | `ls` | List instances |
| `get` | - | Get instance details |
| `login` | - | Login to instance via webshell |
| `delete` | `rm`, `del` | Delete instances |
| `stop` | - | Stop instances (alias for delete) |

## create / start

Create and start a new sandbox instance.

```
ags instance create [flags]
ags instance start [flags]
ags i c [flags]
```

### Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-t, --tool` | string | - | Tool name |
| `--tool-id` | string | - | Tool ID (cloud backend only) |
| `--timeout` | int | `300` | Instance timeout in seconds |
| `--mount-option` | string | - | Mount option override (repeatable) |
| `--time` | bool | `false` | Print elapsed time |

Note: Must specify either `--tool` or `--tool-id`, but not both.

### Mount Option Format

```
name=<name>[,dst=<target-path>][,subpath=<sub-path>][,readonly]
```

| Parameter | Required | Description |
|-----------|----------|-------------|
| `name` | Yes | Storage mount name defined in tool |
| `dst` | No | Override target mount path |
| `subpath` | No | Sub-directory isolation path |
| `readonly` | No | Force read-only mount |

### Examples

```bash
# Create with tool name
ags instance create -t code-interpreter-v1

# Create with tool ID
ags i c --tool-id sdt-xxxxxxxx

# Create with custom timeout (1 hour)
ags instance create -t code-interpreter-v1 --timeout 3600

# Create with mount option override
ags instance create -t my-tool \
  --mount-option "name=data,dst=/workspace,subpath=user-123"
```

## list

List sandbox instances.

```
ags instance list [flags]
ags i ls [flags]
```

### Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-t, --tool` | string | - | Filter by tool ID |
| `-s, --status` | string | - | Filter by status |
| `--short` | bool | `false` | Only show instance IDs |
| `--no-header` | bool | `false` | Hide table header |
| `--offset` | int | `0` | Pagination offset |
| `--limit` | int | `20` | Pagination limit |
| `--time` | bool | `false` | Print elapsed time |

### Examples

```bash
# List all instances
ags instance list

# Filter by tool ID
ags i ls --tool-id sdt-xxxxxxxx

# Filter by status
ags instance list -s Running

# Short format (IDs only)
ags i ls --short

# Pagination
ags instance list --offset 10 --limit 5
```

## get

Get detailed information about an instance.

```
ags instance get <instance-id>
```

### Examples

```bash
ags instance get sbi-xxxxxxxx
```

## login

Login to a sandbox instance via web-based terminal (webshell).

```
ags instance login <instance-id> [flags]
ags i login <instance-id> [flags]
```

This command will:
1. Verify the instance exists and is running
2. Download and start ttyd webshell service if not already running
   (or upload custom ttyd binary if --ttyd-binary is specified)
3. Open the webshell in your default browser

The webshell provides a full terminal interface accessible through your browser, allowing you to interact with the sandbox environment directly.

If the sandbox cannot download ttyd from GitHub due to network restrictions, you can use --ttyd-binary to upload a local ttyd binary file.

### Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--no-browser` | bool | `false` | Don't open browser automatically |
| `--ttyd-binary` | string | - | Path to custom ttyd binary file to upload |
| `--time` | bool | `false` | Print elapsed time |

### Supported Instance Types

- `code-interpreter` - Python/code execution environments
- `browser` - Browser-based environments

### Examples

```bash
# Login to instance with automatic browser opening
ags instance login sbi-xxxxxxxx

# Login without opening browser (manual URL access)
ags i login sbi-xxxxxxxx --no-browser

# Login with custom ttyd binary (for network-restricted environments)
ags instance login sbi-xxxxxxxx --ttyd-binary /path/to/ttyd

# Login with timing information
ags instance login sbi-xxxxxxxx --time
```

## delete / stop

Delete one or more instances.

```
ags instance delete <instance-id> [instance-id...]
ags instance stop <instance-id> [instance-id...]
ags i rm <instance-id> [instance-id...]
```

### Examples

```bash
# Delete single instance
ags instance delete sbi-xxxxxxxx

# Delete multiple instances
ags i rm sbi-xxx sbi-yyy sbi-zzz

# Stop instance
ags instance stop sbi-xxxxxxxx
```

## See Also

- [ags](ags.md) - Main command
- [ags-tool](ags-tool.md) - Tool management
- [ags-run](ags-run.md) - Code execution
