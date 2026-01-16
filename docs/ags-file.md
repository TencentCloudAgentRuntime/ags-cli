# ags-file

File operations in sandbox

## Synopsis

```
ags file <subcommand> [flags]
ags f <subcommand> [flags]
ags fs <subcommand> [flags]
```

## Description

Manage files in a sandbox instance. Supports upload, download, list, remove, and other file operations.

## Subcommands

| Subcommand | Aliases | Description |
|------------|---------|-------------|
| `list` | `ls` | List files in a directory |
| `upload` | `up`, `put` | Upload a file to sandbox |
| `download` | `down`, `get` | Download a file from sandbox |
| `cat` | - | Print file contents to stdout |
| `stat` | - | Get file or directory info |
| `mkdir` | - | Create a directory |
| `remove` | `rm`, `del` | Remove files or directories |

## Common Options

These options apply to all subcommands:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--instance` | string | - | Instance ID to use (required if no tool) |
| `-t, --tool` | string | `code-interpreter-v1` | Tool for temporary instance |
| `--keep-alive` | bool | `false` | Keep temporary instance alive |
| `--time` | bool | `false` | Print elapsed time |

## list

List files in a directory.

```
ags file list <path> [flags]
ags f ls <path> [flags]
```

### Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--depth` | int | `1` | Directory depth (1 = current dir only) |

### Examples

```bash
# List files
ags file ls /home/user

# List with depth
ags f ls /home/user --depth 2
```

## upload

Upload a file to sandbox.

```
ags file upload <local-path> <remote-path>
ags f up <local-path> <remote-path>
```

### Examples

```bash
# Upload file
ags file upload local.txt /home/user/remote.txt

# Upload to existing instance
ags f up data.csv /data/input.csv --instance sbi-xxx
```

## download

Download a file from sandbox.

```
ags file download <remote-path> [local-path]
ags f down <remote-path> [local-path]
```

If local-path is not specified, the file is saved with the same name in the current directory.

### Examples

```bash
# Download file
ags file download /home/user/output.txt ./result.txt

# Download to current directory
ags f down /home/user/data.csv
```

## cat

Print file contents to stdout.

```
ags file cat <path>
```

### Examples

```bash
# View file contents
ags file cat /home/user/.bashrc

# View and pipe to other commands
ags f cat /etc/passwd | grep root
```

## stat

Get file or directory information.

```
ags file stat <path>
```

### Examples

```bash
# Get file info
ags file stat /home/user/.bashrc
```

Output includes: name, path, type, size, permissions, owner, group, modified time.

## mkdir

Create a directory.

```
ags file mkdir <path>
```

### Examples

```bash
# Create directory
ags file mkdir /home/user/newdir
```

## remove

Remove files or directories.

```
ags file remove <path> [path...]
ags f rm <path> [path...]
```

### Examples

```bash
# Remove single file
ags file rm /home/user/temp.txt

# Remove multiple files
ags f rm /tmp/a.txt /tmp/b.txt /tmp/c.txt
```

## JSON Output

All subcommands support JSON output with timing information:

```bash
# JSON with timing (new sandbox)
ags file ls /home/user -o json --time
# Output: {"entries": [...], "timing": {"total_ms": 2500, "create_ms": 2300, "exec_ms": 200}}

# JSON with timing (existing instance)
ags file ls /home/user -o json --time --instance sbi-xxx
# Output: {"entries": [...], "timing": {"total_ms": 200}}
```

## See Also

- [ags](ags.md) - Main command
- [ags-exec](ags-exec.md) - Shell command execution
- [ags-instance](ags-instance.md) - Instance management
