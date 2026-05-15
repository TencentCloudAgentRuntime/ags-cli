# Troubleshooting

This guide covers common errors you may encounter when using AGS CLI and how to resolve them.

## Authentication Failures

### E2B Backend: Invalid or Missing API Key

**Symptom:**

```
Error: E2B API key is required (set AGS_E2B_API_KEY or e2b.api_key in config)
```

**Cause:** The CLI cannot find your E2B API key.

**Fix:**

1. Set the environment variable:
   ```bash
   export AGS_E2B_API_KEY="your-api-key"
   ```
2. Or add it to `~/.ags/config.toml`:
   ```toml
   backend = "e2b"

   [e2b]
   api_key = "your-e2b-api-key"
   ```
3. Verify the key is loaded:
   ```bash
   ags config show  # check that api_key is set
   ```

### Cloud Backend: Invalid or Missing AKSK Credentials

**Symptom:**

```
Error: cloud API credentials are required (set AGS_CLOUD_SECRET_ID/AGS_CLOUD_SECRET_KEY or cloud.secret_id/cloud.secret_key in config)
```

**Cause:** Tencent Cloud SecretID or SecretKey is not configured.

**Fix:**

1. Obtain your credentials from the [Tencent Cloud Console - API Keys](https://console.cloud.tencent.com/cam/capi).
2. Set environment variables:
   ```bash
   export AGS_CLOUD_SECRET_ID="your-secret-id"
   export AGS_CLOUD_SECRET_KEY="your-secret-key"
   ```
3. Or add them to `~/.ags/config.toml`:
   ```toml
   backend = "cloud"

   [cloud]
   secret_id = "your-secret-id"
   secret_key = "your-secret-key"
   region = "ap-guangzhou"
   ```

### Authentication Succeeds but Requests Are Rejected

**Symptom:** HTTP 403 Forbidden or "authorization failed" errors when calling the API.

**Cause:** Your credentials are valid but do not have the required permissions.

**Fix:**

1. Ensure your Tencent Cloud sub-account has the `QcloudAGSFullAccess` policy attached (or equivalent permissions).
2. If using a temporary credential (STS token), make sure it has not expired.

---

## Backend Capability Mismatch

### "Not Supported" Error for Tool or API Key Operations

**Symptom:**

```
Error: operation not supported by e2b backend
```

**Cause:** The E2B backend only supports instance operations, code execution, and file operations. Tool management (`ags tool`) and API key management (`ags apikey`) require the Cloud backend.

**Fix:**

Switch to the Cloud backend:

```bash
# Via environment variables
export AGS_CLOUD_SECRET_ID="your-secret-id"
export AGS_CLOUD_SECRET_KEY="your-secret-key"

# Then run with --backend flag
ags tool list --backend cloud
```

Or set the default backend in `~/.ags/config.toml`:

```toml
backend = "cloud"
```

**Reference:**

| Feature | E2B Backend | Cloud Backend |
|---------|-------------|---------------|
| Tool management | ✗ | ✓ |
| Instance operations | ✓ | ✓ |
| Code execution | ✓ | ✓ |
| File operations | ✓ | ✓ |
| API key management | ✗ | ✓ |

### Invalid Backend Value

**Symptom:**

```
Error: invalid backend: xyz (must be 'e2b' or 'cloud')
```

**Fix:** Set `backend` to either `e2b` or `cloud` in your config file or `--backend` flag.

---

## Network and Region Issues

### Connection Timeout or Refused

**Symptom:**

```
Error: failed to create sandbox: Post "https://api.ap-guangzhou.tencentags.com/...": dial tcp: i/o timeout
```

**Cause:** The CLI cannot reach the AGS API endpoint. Possible reasons:
- Corporate firewall or proxy blocking outbound HTTPS
- Incorrect region or domain configuration
- DNS resolution failure

**Fix:**

1. **Check connectivity:**
   ```bash
   curl -I https://api.ap-guangzhou.tencentags.com
   ```
2. **If behind a proxy**, configure your HTTP proxy:
   ```bash
   export HTTP_PROXY="http://your-proxy:port"
   export HTTPS_PROXY="http://your-proxy:port"
   ```
3. **If on Tencent Cloud internal network**, use the internal endpoint:
   ```bash
   ags --internal <command>
   ```
4. **Verify your region** is correct. Available regions include:
   - `ap-guangzhou` (default)
   - Check the [AGS documentation](https://cloud.tencent.com/document/product/1732) for the latest region list.

### Wrong Region

**Symptom:** Sandbox tools or instances created in one region are not visible when querying another region.

**Fix:** Make sure the `region` in your config matches where your resources were created:

```toml
[e2b]
region = "ap-guangzhou"

[cloud]
region = "ap-guangzhou"
```

Or pass `--region` on the command line:

```bash
ags tool list --region ap-guangzhou
```

---

## Port Forwarding Issues

### Port Forwarding Returns 403 or Connection Refused

**Symptom:** `ags proxy` connects but all requests return HTTP 403 or are immediately closed.

**Cause:** The target port has not been opened in the AGS console.

**Fix:**

1. Go to the [AGS sandbox console](https://console.cloud.tencent.com/ags).
2. Navigate to your sandbox instance → **Network** → **Open Port**.
3. Add the remote port number to the allowlist.
4. Retry:
   ```bash
   ags proxy <sandbox-id> 8080
   ```

---

## Configuration Issues

### Deprecation Warnings

**Symptom:**

```
Warning: config field "e2b.region" is deprecated, please use top-level "region" instead.
```

**Cause:** Your config file uses a legacy field layout.

**Fix:** Move deprecated fields to the top level in `~/.ags/config.toml`:

```toml
# Old (deprecated)
[e2b]
region = "ap-guangzhou"
domain = "tencentags.com"

# New (recommended)
region = "ap-guangzhou"
domain = "tencentags.com"

[e2b]
api_key = "your-api-key"
```

### Config File Not Found

**Symptom:** CLI uses default values and ignores your settings.

**Cause:** The config file is not at the expected path.

**Fix:** The default config path is `~/.ags/config.toml`. You can specify a custom path:

```bash
ags --config /path/to/config.toml <command>
```

---

## Instance and Sandbox Errors

### Cannot Specify Both --instance and --tool-name

**Symptom:**

```
Error: cannot specify both --instance and --tool-name/--tool
```

**Fix:** Use only one of these options:
- `--instance <id>` to target an existing instance
- `--tool-name <name>` or `--tool <name>` to create a new instance from a tool

### Sandbox Creation Fails

**Symptom:**

```
Error: failed to create sandbox: ...
```

**Possible causes and fixes:**

1. **Tool not found:** Verify the tool name exists:
   ```bash
   ags tool list
   ```
2. **Quota exceeded:** Check your AGS resource quota in the [Tencent Cloud Console](https://console.cloud.tencent.com/ags).
3. **Credentials invalid:** See the [Authentication Failures](#authentication-failures) section above.

---

## Mobile Sandbox Issues

### ADB Not Found

**Symptom:**

```
Error: adb not found in PATH
```

**Fix:** Install [Android SDK Platform Tools](https://developer.android.com/tools/releases/platform-tools) and ensure `adb` is in your `$PATH`:

```bash
# macOS (via Homebrew)
brew install android-platform-tools

# Verify
adb version
```

### Mobile Connect Fails

**Symptom:** `ags mobile connect` fails to establish a tunnel.

**Fix:**

1. Confirm the instance is a **mobile** type sandbox (not a code sandbox).
2. Ensure the instance is running:
   ```bash
   ags instance list
   ```
3. Check the tunnel log for details:
   ```bash
   cat ~/.ags/tunnel-<sandbox-id>.log
   ```

---

## Common Error Quick Reference

| Error Message | Likely Cause | Quick Fix |
|---|---|---|
| `E2B API key is required` | Missing API key | Set `AGS_E2B_API_KEY` |
| `cloud API credentials are required` | Missing AKSK | Set `AGS_CLOUD_SECRET_ID` and `AGS_CLOUD_SECRET_KEY` |
| `invalid backend` | Typo in backend name | Use `e2b` or `cloud` |
| `operation not supported` | Wrong backend for this command | Switch to `cloud` backend |
| `dial tcp: i/o timeout` | Network issue | Check firewall, proxy, region |
| `cannot specify both --instance and --tool-name` | Conflicting flags | Use only one |
| `adb not found` | ADB not installed | Install Android Platform Tools |
| `failed to create sandbox` | Auth, quota, or tool issue | Check credentials and tool name |

---

## Still Stuck?

If the above steps do not resolve your issue:

1. Run the failing command with `--output json` to get structured error details.
2. Open an issue on [GitHub](https://github.com/TencentCloudAgentRuntime/ags-cli/issues) with:
   - The full error message
   - Your CLI version (`ags --version`)
   - Your OS and Go version
   - The command you ran (redact credentials)
