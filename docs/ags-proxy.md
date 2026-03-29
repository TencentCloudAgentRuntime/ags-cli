# ags-proxy

Forward a sandbox port to localhost

## Synopsis

```
ags proxy <sandbox_id> [local_port:]<remote_port> [flags]
```

## Description

Creates a local HTTP/WebSocket reverse proxy that forwards all traffic to the specified port on a remote sandbox, similar to `kubectl port-forward`. Access tokens are automatically injected into every proxied request.

Both HTTP and WebSocket protocols are fully supported, making this command suitable for web development servers (e.g., Vite, webpack-dev-server), API servers, and any HTTP-based service running in the sandbox.

> **Note**: Before using this command, open the target port in the AGS sandbox console: navigate to your sandbox instance → **Network** → **Open Port**, and add the remote port number to the allowlist. Requests to ports that have not been configured will be rejected by the gateway.

## Port Syntax

| Format | Description |
|--------|-------------|
| `<remote_port>` | Forward the remote port to the same local port |
| `<local_port>:<remote_port>` | Forward the remote port to a specific local port |

## Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--address` | string | `127.0.0.1` | Local address to bind to |
| `--verbose` | bool | `false` | Enable verbose request logging |

## Examples

```bash
# Forward sandbox port 8080 to localhost:8080
ags proxy sandbox-xxx 8080

# Forward sandbox port 8080 to localhost:3000
ags proxy sandbox-xxx 3000:8080

# Bind to all interfaces (accessible from other machines on the network)
ags proxy sandbox-xxx 8080 --address 0.0.0.0

# Enable verbose logging to see each proxied request
ags proxy sandbox-xxx 8080 --verbose
```

## Output

When the proxy starts, it prints the local and remote addresses:

```
Forwarding from 127.0.0.1:8080 -> 8080
  Local:  http://127.0.0.1:8080
  Remote: https://8080-sandbox-xxx.ap-guangzhou.tencentags.com

Press Ctrl+C to stop.
```

Press **Ctrl+C** to stop the proxy. In-flight requests are given up to 5 seconds to complete before the process exits.

## Full Workflow Example

```bash
# 1. Create a sandbox instance
ags instance create -t <tool-name>
# ✓ Instance created: sandbox-xxx

# 2. Start a web server inside the sandbox (e.g., via exec or run)
ags exec sandbox-xxx "python3 -m http.server 8080 &"

# 3. Forward the sandbox port to localhost
ags proxy sandbox-xxx 8080
# Forwarding from 127.0.0.1:8080 -> 8080
#   Local:  http://127.0.0.1:8080
#   Remote: https://8080-sandbox-xxx.ap-guangzhou.tencentags.com

# 4. Access the service locally
curl http://127.0.0.1:8080
# or open in a browser

# 5. Press Ctrl+C to stop the proxy when done
```

## See Also

- [ags](ags.md) - Main command
- [ags-instance](ags-instance.md) - Instance management
- [ags-exec](ags-exec.md) - Shell command execution

## Known Limitations

- **WebSocket Ping/Pong frames are not forwarded**: The proxy handles Ping/Pong
  control frames internally (automatically replying with Pong) rather than
  passing them through. Applications that rely on custom Ping payloads for
  application-level heartbeats may see unexpected behaviour.

- **Token is acquired once at startup**: The access token is tied to the sandbox
  instance lifecycle. If the sandbox is destroyed and recreated while the proxy
  is running, the proxy will continue using the old (now-invalid) token and all
  requests will fail. Restart `ags proxy` after recreating the sandbox.
