# ags-config

Configuration reference for AGS CLI

## Configuration File

The default configuration file is located at `~/.ags/config.toml`. You can specify a different path with the `--config` flag.

```bash
ags --config /path/to/config.toml
```

### Full Example

```toml
# Backend type: "e2b" or "cloud"
backend = "e2b"

# Default output format: "text" or "json"
output = "text"

# Region for API access (default: ap-guangzhou)
# region = "ap-guangzhou"

# Base domain for AGS services (default: tencentags.com)
# domain = "tencentags.com"

# Use internal endpoints for Tencent Cloud internal network (default: false)
# When true, "internal." is automatically prepended to the domain.
# internal = false

# E2B API Configuration
[e2b]
api_key = "your-e2b-api-key"

# Cloud API Configuration (Tencent Cloud)
[cloud]
secret_id = "your-secret-id"
secret_key = "your-secret-key"

# Sandbox Configuration
[sandbox]
default_user = "user"
```

## Configuration Fields

### Top-Level Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `backend` | string | `e2b` | API backend: `e2b` or `cloud` |
| `output` | string | `text` | Output format: `text` or `json` |
| `region` | string | `ap-guangzhou` | Region for API access |
| `domain` | string | `tencentags.com` | Base domain for AGS services |
| `internal` | bool | `false` | Use internal endpoints (Tencent Cloud internal network) |

### `[e2b]` Section

| Field | Type | Description |
|-------|------|-------------|
| `api_key` | string | E2B API key (required when `backend = "e2b"`) |

### `[cloud]` Section

| Field | Type | Description |
|-------|------|-------------|
| `secret_id` | string | Tencent Cloud SecretID (required when `backend = "cloud"`) |
| `secret_key` | string | Tencent Cloud SecretKey (required when `backend = "cloud"`) |

### `[sandbox]` Section

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `default_user` | string | `user` | Default user for data plane operations |

## Environment Variables

All configuration fields can be set via environment variables with the `AGS_` prefix:

| Environment Variable | Config Field | Description |
|---------------------|--------------|-------------|
| `AGS_BACKEND` | `backend` | API backend |
| `AGS_OUTPUT` | `output` | Output format |
| `AGS_REGION` | `region` | Region |
| `AGS_DOMAIN` | `domain` | Base domain |
| `AGS_INTERNAL` | `internal` | Use internal endpoints |
| `AGS_E2B_API_KEY` | `e2b.api_key` | E2B API key |
| `AGS_CLOUD_SECRET_ID` | `cloud.secret_id` | Tencent Cloud SecretID |
| `AGS_CLOUD_SECRET_KEY` | `cloud.secret_key` | Tencent Cloud SecretKey |
| `AGS_SANDBOX_DEFAULT_USER` | `sandbox.default_user` | Default sandbox user |

## Priority

Configuration values are resolved in the following order (highest priority first):

1. **Command-line flags** (e.g., `--region`, `--domain`)
2. **Environment variables** (e.g., `AGS_REGION`)
3. **Configuration file** (`~/.ags/config.toml`)
4. **Default values**

## Internal Network (internal)

The `internal` field controls whether AGS CLI uses Tencent Cloud internal network endpoints. When set to `true`, the `domain` field is automatically prefixed with `internal.` during configuration resolution. This normalization ensures consistent endpoint construction across all backends.

### How It Works

At config resolution time, if `internal = true` and `domain` does not already start with `internal.`, the domain is automatically transformed:

```
domain = "tencentags.com"  →  domain = "internal.tencentags.com"
```

After normalization, all endpoint functions use the resolved `domain` directly:

| Endpoint | External (`internal=false`) | Internal (`internal=true`) |
|----------|---------------------------|---------------------------|
| E2B Control Plane | `api.{region}.tencentags.com` | `api.{region}.internal.tencentags.com` |
| Cloud Control Plane | `ags.tencentcloudapi.com` | `ags.internal.tencentcloudapi.com` |
| Data Plane | `{region}.tencentags.com` | `{region}.internal.tencentags.com` |

### Equivalent Configurations

The following two configurations produce identical behavior:

**Using `internal` flag (recommended):**
```toml
domain = "tencentags.com"
internal = true
```

**Using domain directly:**
```toml
domain = "internal.tencentags.com"
internal = false
```

> **Note**: If both are set (`domain = "internal.tencentags.com"` and `internal = true`), the CLI detects the existing `internal.` prefix and avoids double-prepending. No error will occur.
>
> **Note**: The `domain` value should be the base domain without the `internal.` prefix. If you need internal endpoints, use `internal = true` instead of manually prepending `internal.` to the domain.

## Deprecated Fields

The following fields under `[e2b]` and `[cloud]` sections are deprecated. Use the top-level fields instead:

| Deprecated Field | Replacement | Environment Variable |
|-----------------|-------------|---------------------|
| `e2b.domain` | `domain` | `AGS_E2B_DOMAIN` → `AGS_DOMAIN` |
| `e2b.region` | `region` | `AGS_E2B_REGION` → `AGS_REGION` |
| `cloud.region` | `region` | `AGS_CLOUD_REGION` → `AGS_REGION` |
| `cloud.internal` | `internal` | `AGS_CLOUD_INTERNAL` → `AGS_INTERNAL` |

When deprecated fields are used, a warning is printed to stderr:

```
Warning: config field "e2b.region" is deprecated, please use top-level "region" instead.
```

Legacy fields are still resolved with the following priority:

1. Top-level field (if set)
2. Backend-specific legacy field (based on current `backend`)
3. Other backend's legacy field (as silent fallback, no deprecation warning)
4. Default value

## See Also

- [ags](ags.md) - Main command
- [config.example.toml](../config.example.toml) - Example configuration file
