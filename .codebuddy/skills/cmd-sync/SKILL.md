---
name: cmd-sync
description: Automatically synchronize CLI command documentation and REPL completions when cmd/*.go files are modified. Use this skill when adding, modifying, or removing commands, subcommands, flags, or aliases.
---

# Command Synchronization Skill

This skill automates the synchronization of CLI documentation and REPL completions when command definitions change.

## When to Use

Trigger this skill when:
- Adding a new command or subcommand in `cmd/*.go`
- Modifying command flags, aliases, or descriptions
- Removing commands or subcommands
- User asks to "sync docs", "update command docs", or "refresh completions"

## Architecture

```
cmd/*.go (Source of Truth)
    │
    ├── docs/ags-<cmd>.md (English)
    ├── docs/ags-<cmd>-zh.md (Chinese)
    ├── internal/repl/repl.go (REPL completions)
    └── man/*.1 (Man pages, auto-generated)
```

## Workflow

### Step 1: Analyze Command Changes

Read the modified `cmd/*.go` file(s) and extract:

1. **Command metadata**:
   - `Use` field → command name and arguments
   - `Aliases` field → command aliases
   - `Short` field → brief description
   - `Long` field → detailed description
   - `Example` field → usage examples

2. **Flags**:
   - Flag name (short and long form)
   - Flag type (string, bool, int, etc.)
   - Default value
   - Description

3. **Subcommands**:
   - Subcommand names and their metadata
   - Subcommand-specific flags

### Step 2: Update/Create Command Documentation

For each command, create or update `docs/ags-<command>.md` and `docs/ags-<command>-zh.md`.

#### English Documentation Template (docs/ags-<cmd>.md)

```markdown
# ags-<command>

<Short description from cobra command>

## Synopsis

```
ags <command> [subcommand] [flags]
```

## Description

<Long description from cobra command>

## Subcommands

| Subcommand | Description |
|------------|-------------|
| `sub1` | Description |

## Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-f, --flag` | string | "" | Description |

## Global Options

See [ags(1)](ags.md) for global options.

## Examples

```bash
# Example 1
ags <command> ...

# Example 2
ags <command> ...
```

## See Also

- [ags](ags.md) - Main command
- [ags-related](ags-related.md) - Related command
```

#### Chinese Documentation Template (docs/ags-<cmd>-zh.md)

```markdown
# ags-<command>

<简短描述>

## 概要

```
ags <command> [子命令] [选项]
```

## 描述

<详细描述>

## 子命令

| 子命令 | 描述 |
|--------|------|
| `sub1` | 描述 |

## 选项

| 选项 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| `-f, --flag` | string | "" | 描述 |

## 全局选项

参见 [ags(1)](ags-zh.md) 了解全局选项。

## 示例

```bash
# 示例 1
ags <command> ...

# 示例 2
ags <command> ...
```

## 另请参阅

- [ags](ags-zh.md) - 主命令
- [ags-related](ags-related-zh.md) - 相关命令
```

### Step 3: Update REPL Completions

Update `internal/repl/repl.go`:

1. **commands slice**: Add/update main command entries
2. **subcommands slice**: Add/update `<cmd>Subcommands` variable
3. **flags slice**: Add/update `<cmd>Flags` or `<cmd><Subcmd>Flags` variable
4. **completer function**: Add/update case for new command
5. **printHelp function**: Update help text

### Step 4: Update Main README

Keep README.md and README-zh.md minimal with:
- Project overview
- Installation
- Quick start (3-5 basic examples)
- Link to detailed docs

Replace detailed command reference with:

```markdown
## Command Reference

For detailed documentation on each command, see:

- [ags](docs/ags.md) - Main command and global options
- [ags-tool](docs/ags-tool.md) - Tool management
- [ags-instance](docs/ags-instance.md) - Instance management
- [ags-run](docs/ags-run.md) - Code execution
- [ags-exec](docs/ags-exec.md) - Shell command execution
- [ags-file](docs/ags-file.md) - File operations
- [ags-apikey](docs/ags-apikey.md) - API key management
```

## Command Extraction Patterns

### Cobra Command Pattern

```go
var cmdName = &cobra.Command{
    Use:     "name <args>",
    Aliases: []string{"n", "alias"},
    Short:   "Brief description",
    Long:    `Detailed description...`,
    Example: `  # Example
  ags name --flag value`,
    RunE:    cmdNameFunc,
}
```

### Flag Patterns

```go
// Persistent flags (inherited by subcommands)
cmd.PersistentFlags().StringVarP(&varName, "flag", "f", "default", "description")

// Local flags
cmd.Flags().BoolVar(&varName, "flag", false, "description")
cmd.Flags().IntVar(&varName, "count", 10, "description")
```

### Subcommand Pattern

```go
func init() {
    parentCmd.AddCommand(subCmd)
}
```

## Translation Reference

| English | Chinese |
|---------|---------|
| Synopsis | 概要 |
| Description | 描述 |
| Subcommands | 子命令 |
| Options | 选项 |
| Global Options | 全局选项 |
| Examples | 示例 |
| See Also | 另请参阅 |
| Default | 默认值 |
| Type | 类型 |
| Required | 必需 |
| Optional | 可选 |

## Validation Checklist

After synchronization, verify:

1. [ ] All commands have corresponding docs in `docs/` directory
2. [ ] Both English and Chinese versions exist and are in sync
3. [ ] REPL `commands` slice includes all commands and aliases
4. [ ] REPL `*Subcommands` slices are complete
5. [ ] REPL `*Flags` slices include all flags
6. [ ] REPL `completer()` handles all command paths
7. [ ] REPL `printHelp()` is updated
8. [ ] README links to all command docs
9. [ ] Man pages can be regenerated with `make man`

## Man Pages

Man pages are auto-generated from cobra command definitions using `ags docs man`.

### Generation

```bash
# Generate man pages to man/ directory
make man
# or
ags docs man -o man
```

### Installation

```bash
# Install to system (requires sudo)
make install-man

# Uninstall
make uninstall-man
```

### Notes

- Man pages are generated from cobra's `Use`, `Short`, `Long`, `Example` fields
- No manual editing needed - they stay in sync automatically
- The `man/` directory is in `.gitignore` (generated files)
- Users can regenerate locally with `make man`

## File Locations

| File | Purpose |
|------|---------|
| `cmd/*.go` | Command definitions (source of truth) |
| `cmd/docs.go` | Documentation generation command |
| `docs/ags.md` | Main command doc (English) |
| `docs/ags-zh.md` | Main command doc (Chinese) |
| `docs/ags-<cmd>.md` | Subcommand doc (English) |
| `docs/ags-<cmd>-zh.md` | Subcommand doc (Chinese) |
| `internal/repl/repl.go` | REPL completions |
| `man/*.1` | Man pages (auto-generated, not committed) |
| `README.md` | Project overview (English) |
| `README-zh.md` | Project overview (Chinese) |
