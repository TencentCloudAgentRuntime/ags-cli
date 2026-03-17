# ags-mobile

Mobile sandbox ADB connection management

## Synopsis

```
ags mobile <subcommand> [options]
ags m <subcommand> [options]
```

## Description

Provides secure ADB access to remote Android sandboxes through encrypted WebSocket tunnels. Supports multiple concurrent connections with automatic reconnection on network disruptions.

## Prerequisites

- ADB (Android SDK Platform-Tools) must be installed locally
- Specify the ADB path via the `ADB_PATH` environment variable, or ensure `adb` is in your `PATH`

## Subcommands

| Subcommand | Description |
|------------|-------------|
| `connect` | Connect to mobile sandbox (background tunnel + adb connect) |
| `disconnect` | Disconnect from mobile sandbox |
| `list` | List active connections |
| `adb` | Execute ADB command on mobile sandbox by ID |
| `tunnel` | Run ADB tunnel in foreground (internal/debug) |

## connect

Connect to a mobile sandbox by establishing a background WebSocket tunnel and registering the ADB device.

```
ags mobile connect <sandbox_id>
```

This command:
1. Acquires an access token for the sandbox
2. Spawns a background tunnel process
3. Automatically runs `adb connect` to the local tunnel port
4. Records the connection in `~/.ags/tunnels.json`

### Examples

```bash
# Connect to a mobile sandbox
ags mobile connect 3239677239b46cca9bc50a969b19fcad80360375
```

## disconnect

Disconnect from a mobile sandbox by terminating the background tunnel and running `adb disconnect`.

```
ags mobile disconnect <sandbox_id>
ags mobile disconnect --all
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--all` | bool | `false` | Disconnect all active connections |

### Examples

```bash
# Disconnect a specific sandbox
ags mobile disconnect 3239677239b46cca9bc50a969b19fcad80360375

# Disconnect all connections
ags mobile disconnect --all
```

## list

List all active ADB tunnel connections with their sandbox IDs, local ports, and status.

```
ags mobile list
```

### Example Output

```
SANDBOX                  ADB ADDRESS            STATUS
3239677239b46cca9bc5...  127.0.0.1:60504        connected
```

## adb

Execute an ADB command targeting a specific mobile sandbox using its ID. Automatically looks up the local tunnel port and passes the command through to the native `adb` binary.

```
ags mobile adb <sandbox_id> [adb_args...]
```

### Examples

```bash
# Check Android version
ags mobile adb <sandbox_id> shell getprop ro.build.version.release

# Check SDK version
ags mobile adb <sandbox_id> shell getprop ro.build.version.sdk

# Check screen resolution
ags mobile adb <sandbox_id> shell wm size

# List installed packages
ags mobile adb <sandbox_id> shell pm list packages

# List files
ags mobile adb <sandbox_id> shell ls /sdcard

# Install APK
ags mobile adb <sandbox_id> install app.apk

# View logs
ags mobile adb <sandbox_id> logcat
```

## tunnel

Run an ADB tunnel in the foreground. Primarily used internally by `connect`; can also be used for debugging.

```
ags mobile tunnel <sandbox_id> [options]
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--daemon` | bool | `false` | Run in daemon mode (used by connect) |
| `--port` | int | `0` | Local port to listen on (0 = auto-assign) |

## Using Native ADB

After connecting via `ags mobile connect`, `ags mobile list` will show the local ADB address. You can then use native `adb` commands directly:

```bash
# Check connection info
ags mobile list
# Output: 127.0.0.1:60504

# Use native adb
adb -s 127.0.0.1:60504 shell
adb -s 127.0.0.1:60504 push local.apk /data/local/tmp/
adb -s 127.0.0.1:60504 install app.apk
adb -s 127.0.0.1:60504 logcat
```

## Full Workflow Example

```bash
# 1. Set environment variables
export AGS_E2B_API_KEY="your-api-key"
export AGS_REGION=ap-chongqing
export AGS_DOMAIN=tencentags.com

# 2. List available mobile sandbox tools
ags tool list

# 3. Create a mobile sandbox instance
ags instance create -t <mobile_tool_name> --timeout 1800

# 4. Connect to sandbox (auto tunnel + ADB connect)
ags mobile connect <sandbox_id>

# 5. Check connection status
ags mobile list

# 6. Execute ADB commands
ags mobile adb <sandbox_id> shell getprop ro.build.version.release

# 7. Or use native adb (available after connect)
adb -s 127.0.0.1:<port> shell

# 8. Disconnect when done
ags mobile disconnect <sandbox_id>
```

## See Also

- [ags](ags.md) - Main command
- [ags-instance](ags-instance.md) - Instance management
