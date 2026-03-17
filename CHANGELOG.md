# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- Add `--mode` flag to `instance login` command with `pty` (default) and `webshell` modes; PTY mode connects a native terminal session directly in the current console without requiring a browser or ttyd binary

## [0.2.1] - 2026-03-13

### Changed
- Expand supported tool types from `code-interpreter` and `browser` to also include `mobile`, `osworld`, `custom`, and `swebench`

## [0.2.0] - 2026-03-09

### Added
- Add `--user` flag to `exec`, `file`, and `instance login` commands to specify the user identity for data plane operations (default: "user")
- Add `sandbox.default_user` configuration option in config.toml for setting the default user globally
- Add unified top-level `region`, `domain`, and `internal` configuration fields to replace backend-specific duplicates
- Add `--region`, `--domain`, and `--internal` global CLI flags
- Add `AGS_REGION`, `AGS_DOMAIN`, and `AGS_INTERNAL` environment variables
- Add dedicated configuration reference documentation (`docs/ags-config.md`)

### Changed
- Unify region/domain/internal configuration: all data plane and control plane operations now read from top-level config fields instead of backend-specific `[e2b]` or `[cloud]` sections
- Control plane clients (`CloudControlPlane`, `E2BControlPlane`) now use unified config for region and domain
- Normalize `internal` flag into `domain` at config resolution time: when `internal=true`, the domain is automatically prefixed with `internal.` (e.g., `internal.tencentags.com`), ensuring consistent endpoint construction for both E2B and Cloud backends

### Deprecated
- Config fields `e2b.region`, `e2b.domain`, `cloud.region`, `cloud.internal` are deprecated in favor of top-level `region`, `domain`, `internal`
- CLI flags `--e2b-region`, `--e2b-domain`, `--cloud-region`, `--cloud-internal` are deprecated in favor of `--region`, `--domain`, `--internal`
- Environment variables `AGS_E2B_REGION`, `AGS_E2B_DOMAIN`, `AGS_CLOUD_REGION`, `AGS_CLOUD_INTERNAL` are deprecated in favor of `AGS_REGION`, `AGS_DOMAIN`, `AGS_INTERNAL`

## [0.1.2] - 2026-02-11

### Changed
- E2B backend now supports token acquisition via GET /sandboxes/{id}, removing the limitation that tokens were only available at instance creation time
- Unified token recovery logic for both Cloud and E2B backends when token cache is missing

## [0.1.1] - 2026-01-20

### Changed
- Separate control plane and data plane with token caching

## [0.1.0] - 2026-01-16

### Added
- Initial release
- Update module path to github.com/TencentCloudAgentRuntime/ags-cli
- Replace all git.woa.com references with github.com/TencentCloudAgentRuntime/ags-go-sdk v0.0.10
