# AGS CLI E2E Test

[中文文档](README-zh.md)

End-to-end test script for AGS CLI, testing the complete lifecycle of API keys, tools, and instances.

## Prerequisites

- AGS CLI built and available in PATH (or run from repo root)
- Cloud backend configured (`~/.ags/config.toml` or environment variables)
- `jq` installed for JSON parsing

## Usage

```bash
# Run with default settings (ap-guangzhou, external network)
./examples/e2e-test/e2e_test.sh

# Show help
./examples/e2e-test/e2e_test.sh -h
```

## Options

| Option | Description | Default |
|--------|-------------|---------|
| `-r, --region REGION` | Set region | `ap-guangzhou` |
| `-i, --internal` | Use internal endpoints | disabled |
| `-d, --domain DOMAIN` | Set E2B domain | `tencentags.com` |
| `-h, --help` | Show help message | - |

## Examples

```bash
# Use internal network
./examples/e2e-test/e2e_test.sh -i

# Use Shanghai region
./examples/e2e-test/e2e_test.sh -r ap-shanghai

# Use custom E2B domain
./examples/e2e-test/e2e_test.sh -d custom-domain.com

# Combine options
./examples/e2e-test/e2e_test.sh -r ap-shanghai -i
```

## Test Steps

The script performs the following 14 steps:

1. **List API Keys** - Get initial API key count
2. **Create API Key** - Create a new test API key
3. **Verify API Key** - Confirm the new key exists in the list
4. **List Tools** - Get initial tool count
5. **Create Tool** - Create a new code-interpreter tool
6. **Verify Tool** - Confirm the new tool exists in the list
7. **List Instances** - Get initial instance count (using E2B backend)
8. **Create & Run Instance** - Execute code and test streaming output
9. **Verify Instance** - Confirm the new instance exists in the list
10. **Test exec Command** - Test shell execution (exec, exec with cwd/env, exec ps)
11. **Test file Command** - Test file operations (list, mkdir, upload, cat, stat, download, remove)
12. **Delete Instance** - Remove the created instance
13. **Delete Tool** - Remove the created tool
14. **Delete API Key** - Remove the created API key

## Output

The script provides colored output:
- 🟢 Green (`✓`) - Success
- 🔴 Red (`✗`) - Error
- 🟡 Yellow - Info/Warning
- 🔵 Blue - Step headers

## Exit Codes

- `0` - All tests passed
- `1` - One or more tests failed
