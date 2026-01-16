# ags-browser

Manage browser sandbox

## Synopsis

```
ags browser <subcommand> [flags]
ags b <subcommand> [flags]
```

## Description

Browser sandboxes provide a remote browser environment accessible via VNC. You can view and interact with the browser through a web-based VNC client or programmatically via the Chrome DevTools Protocol (CDP).

## Subcommands

| Subcommand | Description |
|------------|-------------|
| `vnc` | Show VNC URL for browser sandbox |

## vnc

Show the VNC URL for accessing a browser sandbox. You can either connect to an existing instance or create a new one.

```
ags browser vnc [flags]
ags b vnc [flags]
```

### Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--instance` | string | - | Instance ID to connect to |
| `-t, --tool` | string | - | Tool name for creating new instance |
| `--tool-id` | string | - | Tool ID (cloud backend only) |
| `--timeout` | int | `300` | Instance timeout in seconds |
| `-p, --port` | int | `9000` | VNC service port |
| `--time` | bool | `false` | Print elapsed time |

Note: Must specify either `--instance` or `--tool`/`--tool-id`, but not both.

### Output

The command outputs:

| Field | Description |
|-------|-------------|
| `instance_id` | The sandbox instance ID |
| `tool` | Tool name used |
| `status` | Instance status |
| `vnc_url` | URL to access the browser via noVNC web client |
| `cdp_url` | Chrome DevTools Protocol URL for programmatic access |

### Examples

```bash
# Show VNC URL for existing instance
ags browser vnc --instance sbi-xxxxxxxx

# Create new browser sandbox and show VNC URL
ags browser vnc --tool browser-v1

# Create using tool ID
ags browser vnc --tool-id sdt-xxxxxxxx

# Create with custom timeout (1 hour)
ags browser vnc --tool browser-v1 --timeout 3600

# Use custom port
ags browser vnc --tool browser-v1 --port 5900

# JSON output
ags browser vnc --tool browser-v1 -o json
```

### VNC URL Format

The VNC URL follows this format:
```
https://{port}-{instance_id}.{region}.{domain}/novnc/vnc_lite.html?&path=websockify?access_token={token}
```

### CDP URL Format

The CDP URL for programmatic browser control:
```
https://{port}-{instance_id}.{region}.{domain}/cdp?access_token={token}
```

## Global Options

See [ags(1)](ags.md) for global options.

## See Also

- [ags](ags.md) - Main command
- [ags-instance](ags-instance.md) - Instance management
- [ags-tool](ags-tool.md) - Tool management
