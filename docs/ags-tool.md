# ags-tool

Manage sandbox tools (templates)

## Synopsis

```
ags tool <subcommand> [flags]
ags t <subcommand> [flags]
```

## Description

Tools are sandbox templates that define the runtime environment. Each tool specifies the base image, network configuration, storage mounts, and other settings for sandbox instances.

## Subcommands

| Subcommand | Aliases | Description |
|------------|---------|-------------|
| `list` | `ls` | List available tools |
| `get` | - | Get tool details |
| `create` | - | Create a new tool (cloud backend only) |
| `update` | - | Update a tool (cloud backend only) |
| `delete` | `rm`, `del` | Delete tools (cloud backend only) |

## list

List available tools.

```
ags tool list [flags]
```

### Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--id` | string | - | Specific tool IDs (repeatable) |
| `--status` | string | - | Filter: CREATING, ACTIVE, DELETING, FAILED |
| `--type` | string | - | Filter: code-interpreter, browser |
| `--tag` | string | - | Filter by tag (key=value, repeatable) |
| `--created-since` | string | - | Filter by relative time (e.g., 1h, 24h) |
| `--short` | bool | `false` | Show only ID and NAME |
| `--offset` | int | `0` | Pagination offset |
| `--limit` | int | `20` | Pagination limit |
| `--time` | bool | `false` | Print elapsed time |

### Examples

```bash
# List all tools
ags tool list

# Short format
ags t ls --short

# Filter by type
ags tool list --type code-interpreter

# Filter by status
ags tool list --status ACTIVE

# Filter by tag
ags tool list --tag env=prod --tag team=ai

# Pagination
ags tool list --offset 20 --limit 10
```

## get

Get detailed information about a tool.

```
ags tool get <tool-id>
```

### Examples

```bash
ags tool get sdt-xxxxxxxx
```

## create

Create a new tool (cloud backend only).

```
ags tool create [flags]
```

### Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-n, --name` | string | - | Tool name (required) |
| `-t, --type` | string | - | Tool type (required): code-interpreter, browser |
| `-d, --description` | string | - | Tool description |
| `--timeout` | duration | - | Default timeout (e.g., 5m, 1h) |
| `--network` | string | - | Network mode: PUBLIC |
| `--tag` | string | - | Tags (key=value, repeatable) |
| `--role-arn` | string | - | CAM Role ARN for COS access |
| `--mount` | string | - | Storage mount config (repeatable) |

### Mount Format

```
type=cos,name=<name>,bucket=<bucket>,src=<source>,dst=<target>[,readonly][,endpoint=<endpoint>]
```

| Parameter | Required | Description |
|-----------|----------|-------------|
| `type` | Yes | Storage type: `cos` |
| `name` | Yes | Mount name (DNS-1123 format) |
| `bucket` | Yes | COS bucket name |
| `src` | Yes | Source path (must start with `/`) |
| `dst` | Yes | Target path (must start with `/`) |
| `readonly` | No | Mount as read-only |
| `endpoint` | No | COS endpoint |

### Examples

```bash
# Basic creation
ags tool create -n my-tool -t code-interpreter

# With description and tags
ags tool create -n my-tool -t code-interpreter \
  -d "My custom tool" \
  --tag env=dev --tag team=ai

# With COS mount
ags tool create -n my-tool -t code-interpreter \
  --role-arn "qcs::cam::uin/100000:roleName/AGS_COS_Role" \
  --mount "type=cos,name=data,bucket=my-bucket-1250000000,src=/data,dst=/mnt/data"
```

## update

Update an existing tool (cloud backend only).

```
ags tool update <tool-id> [flags]
```

### Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-d, --description` | string | - | Tool description |
| `--network` | string | - | Network mode: PUBLIC, SANDBOX, INTERNAL_SERVICE |
| `--tag` | string | - | Tags (key=value, repeatable) |
| `--clear-tags` | bool | `false` | Clear all tags |

At least one flag must be specified.

### Examples

```bash
# Update description
ags tool update sdt-xxx -d "Updated description"

# Update network mode
ags tool update sdt-xxx --network SANDBOX

# Update tags
ags tool update sdt-xxx --tag env=staging --tag team=ai

# Clear all tags
ags tool update sdt-xxx --clear-tags
```

## delete

Delete one or more tools (cloud backend only).

```
ags tool delete <tool-id> [tool-id...]
ags tool rm <tool-id> [tool-id...]
```

### Examples

```bash
# Delete single tool
ags tool delete sdt-xxx

# Delete multiple tools
ags t rm sdt-xxx sdt-yyy sdt-zzz
```

## See Also

- [ags](ags.md) - Main command
- [ags-instance](ags-instance.md) - Instance management
