# Integration Tests

This directory contains integration tests that run the real `agr` binary and assert on its output — no cloud
credentials, no network access, no deployed resources.

## What Integration Tests Cover

Integration tests verify that CLI commands behave correctly by checking:

- **Exit code and stdout** — the command exits `0` on success, non-zero on failure
- **`-o json` output** — JSON envelope has the correct `agr.v1` shape (`SchemaVersion`, `Status`, `Data`, `Failure`)
- **Help text** — root and subcommand help contains expected commands/flags
- **Error handling** — unknown commands, missing args, and invalid flags produce correct exit codes and messages
- **Offline commands** — `version`, `config path`, `explain`, `doctor`, `completion` work without credentials

They do **not** verify API calls, deployed resources, or credential-dependent workflows. Those belong in
`tests/credential/` (mock server) and `tests/lifecycle/` (live E2E).

## Prerequisites

- Go toolchain on PATH (builds the binary automatically)
- No cloud credentials needed
- No network access required

## Running

```bash
# Run all integration tests
make integ

# Or directly with go test
go test ./tests/integ/ -v -count=1

# Run a specific test
go test ./tests/integ/ -run TestHelp_RootHelp -v
```

## Test Architecture

```
tests/integ/integ_test.go
  ├── testBinary()— builds agr once per suite (sync.Once)
  ├── run(args...)          — runs binary in credential-free env
  ├── parseEnvelope(output) — parses agr.v1 JSON envelope
  └── Test* functions— individual assertions
```

### Key patterns

| Pattern | Why |
|---------|-----|
| `testBinary()` with `sync.Once` | Build once, share across all tests (~2s total) |
| Isolated `HOME` via `t.TempDir()` | No config file interference |
| Clear credential env vars | Guarantee credential-free execution |
| Assert exit code first | Fail fast with useful message before asserting output |
| `-o json` flag | Makes stdout machine-readable for assertions |

### File naming

Currently a single file. When the suite grows beyond ~30 tests, split by feature area:

- `help_test.go` — help text tests
- `schema_test.go` — schema output tests
- `envelope_test.go` — JSON envelope compliance
- `config_test.go` — config set/show/path tests
- `error_test.go` — exit codes and error messages
- `edge_test.go` — flag interactions, edge cases

## Relationship to Other Test Layers

```
Unit tests (internal/commands/*_test.go)
  └── Fast, mockControlPlane, verify params/action mapping

Integration tests (tests/integ/)← this directory
  └── Real binary, no network, verify CLI UX contract

Mock server tests (tests/credential/)
  └── Real binary + httptest TLS server, verify full HTTP pipeline

E2E lifecycle tests (tests/lifecycle/)
  └── Real binary + live API, verify deployed resources (requires credentials)
```
