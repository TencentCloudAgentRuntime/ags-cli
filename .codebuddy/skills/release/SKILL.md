---
name: release
description: This skill automates the version release process including changelog generation, git tagging, and bilingual documentation updates. Use this skill when the user wants to create a new release, bump version, generate changelog, or tag a new version.
---

# Release Automation Skill

This skill handles the complete release workflow for this Go CLI project, ensuring bilingual documentation stays in sync.

## When to Use

- User requests to "release a new version"
- User wants to "bump version" or "create a tag"
- User asks to "generate changelog" or "update changelog"
- User mentions "prepare release" or "cut a release"

## Release Workflow

### Step 1: Determine Version

Ask the user for the version type if not specified.

#### Stable Releases

- **patch**: Bug fixes (v1.0.0 → v1.0.1)
- **minor**: New features (v1.0.0 → v1.1.0)
- **major**: Breaking changes (v1.0.0 → v2.0.0)

#### Pre-release Versions

- **alpha**: Early development (v0.0.1-alpha → v0.0.1-alpha.1 → v0.0.1-alpha.2)
- **beta**: Feature complete, testing (v0.0.1-beta → v0.0.1-beta.1)
- **rc**: Release candidate (v0.0.1-rc.1 → v0.0.1-rc.2)

#### Pre-release Progression

```
v0.0.1-alpha → v0.0.1-alpha.1 → v0.0.1-alpha.2
    ↓
v0.0.1-beta → v0.0.1-beta.1 → v0.0.1-beta.2
    ↓
v0.0.1-rc.1 → v0.0.1-rc.2
    ↓
v0.0.1 (stable)
```

To get the current version:

```bash
git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"
```

#### Version Calculation Examples

| Current Tag | Release Type | New Version |
|-------------|--------------|-------------|
| v0.0.1-alpha.2 | alpha | v0.0.1-alpha.3 |
| v0.0.1-alpha.2 | beta | v0.0.1-beta |
| v0.0.1-beta.1 | beta | v0.0.1-beta.2 |
| v0.0.1-beta.2 | rc | v0.0.1-rc.1 |
| v0.0.1-rc.2 | stable | v0.0.1 |
| v0.0.1 | patch | v0.0.2 |
| v0.0.1 | minor | v0.1.0 |
| v0.0.1 | major | v1.0.0 |

### Step 2: Gather Changes

Collect commits since last tag:

```bash
git log $(git describe --tags --abbrev=0 2>/dev/null || echo "")..HEAD --oneline --no-merges
```

Categorize commits into:

- **Added**: New features (commits with "feat:", "add:", "new:")
- **Changed**: Changes (commits with "change:", "update:", "refactor:")
- **Fixed**: Bug fixes (commits with "fix:", "bug:", "patch:")
- **Removed**: Removals (commits with "remove:", "delete:", "deprecate:")

### Step 3: Update CHANGELOG Files

Update both `CHANGELOG.md` and `CHANGELOG-zh.md` simultaneously.

#### English CHANGELOG.md Format

```markdown
# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

## [X.Y.Z] - YYYY-MM-DD

### Added
- Feature description

### Changed
- Change description

### Fixed
- Bug fix description

### Removed
- Removal description
```

#### Chinese CHANGELOG-zh.md Format

```markdown
# 更新日志

本项目的所有重要更改都将记录在此文件中。

## [未发布]

## [X.Y.Z] - YYYY-MM-DD

### 新增
- 功能描述

### 变更
- 变更描述

### 修复
- 修复描述

### 移除
- 移除描述
```

### Step 4: Create Git Tag

After changelog updates are committed:

```bash
# Create annotated tag
git tag -a vX.Y.Z -m "Release vX.Y.Z"

# Show tag info
git show vX.Y.Z
```

### Step 5: Verification Checklist

Before finalizing, verify:

1. [ ] CHANGELOG.md has new version section
2. [ ] CHANGELOG-zh.md has matching Chinese version
3. [ ] Both changelogs have same structure and content
4. [ ] Git tag created with correct version
5. [ ] Tag message is descriptive

### Step 6: Push (Optional)

If user confirms, push tag to remote:

```bash
git push origin vX.Y.Z
```

## Section Header Translations

| English | Chinese |
|---------|---------|
| Changelog | 更新日志 |
| Unreleased | 未发布 |
| Added | 新增 |
| Changed | 变更 |
| Fixed | 修复 |
| Removed | 移除 |
| Deprecated | 废弃 |
| Security | 安全 |

## Commit Message Conventions

This skill recognizes conventional commit prefixes:

| Prefix | Category |
|--------|----------|
| `feat:`, `feature:`, `add:` | Added |
| `fix:`, `bug:`, `bugfix:` | Fixed |
| `change:`, `update:`, `refactor:` | Changed |
| `remove:`, `delete:`, `deprecate:` | Removed |
| `docs:` | Documentation (usually skip) |
| `test:`, `ci:` | Testing/CI (usually skip) |
| `chore:` | Maintenance (usually skip) |

## Error Handling

- If no tags exist, start from v0.0.1-alpha
- If CHANGELOG files don't exist, create them with proper headers
- Always create both language versions together
- Never push tags without user confirmation
- For pre-release versions, increment the numeric suffix (alpha.1 → alpha.2)
- When transitioning phases (alpha → beta), reset the suffix
