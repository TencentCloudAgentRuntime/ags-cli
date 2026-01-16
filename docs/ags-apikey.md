# ags-apikey

Manage API keys (cloud backend only)

## Synopsis

```
ags apikey <subcommand> [flags]
ags ak <subcommand> [flags]
ags key <subcommand> [flags]
```

## Description

Create, list, and delete API keys for the AGS cloud backend. API keys provide an alternative authentication method to SecretID/SecretKey.

**Note**: This command is only available when using the cloud backend (`--backend cloud`).

## Subcommands

| Subcommand | Aliases | Description |
|------------|---------|-------------|
| `create` | - | Create a new API key |
| `list` | `ls` | List API keys |
| `delete` | `rm`, `del` | Delete an API key |

## create

Create a new API key.

```
ags apikey create [flags]
```

### Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-n, --name` | string | - | API key name (required) |

### Examples

```bash
# Create API key
ags apikey create -n my-key

# Create with cloud backend
ags --backend cloud apikey create -n production-key
```

## list

List all API keys.

```
ags apikey list
ags ak ls
```

### Examples

```bash
# List all API keys
ags apikey list

# List in JSON format
ags ak ls -o json
```

## delete

Delete an API key.

```
ags apikey delete <key-id>
ags ak rm <key-id>
```

### Examples

```bash
# Delete API key
ags apikey delete ak-xxxxxxxx

# Delete with confirmation
ags ak rm ak-xxxxxxxx
```

## See Also

- [ags](ags.md) - Main command
- [ags-tool](ags-tool.md) - Tool management
