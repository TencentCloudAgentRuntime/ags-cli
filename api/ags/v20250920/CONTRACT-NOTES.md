# AGS v20250920 contract notes

## Source of the keepalive and log format members

The preview overlay adds `KeepaliveConfiguration` (`KeepaliveMode`,
`KeepaliveTimeout`), `AutoPause`, `AutoResume` to `CreateSandboxToolRequest`,
`UpdateSandboxToolRequest`, `StartSandboxInstanceRequest`,
`UpdateSandboxInstanceRequest`, `SandboxTool` and `SandboxInstance`, and
`LogConfiguration.ContentFormat` to `LogConfiguration`.

- Public candidate source: the AGS contract description published in
  `TencentCloudAgentRuntime/ags-cli#172` (2026-11-13), which names the same AGS
  service, API version `v20250920`, endpoint `ags.tencentcloudapi.com` and
  resource namespace as this repository. The downloaded `tccli` canonical
  `api.json` was byte-identical to `api/ags/v20250920/api.json` and contains
  none of the members, so this is **not** an upstream sync.
- `api/ags/v20250920/api.json` stays untouched; the members live only in
  `api.patch.json` and therefore only in the preview channel.
- Transport strategy: no SDK upgrade and no handwritten SDK structs. The
  existing `needsDynamic` compatibility check routes the seven affected actions
  (`CreateSandboxTool`, `UpdateSandboxTool`, `StartSandboxInstance`,
  `UpdateSandboxInstance`, `DescribeSandboxToolList`,
  `DescribeSandboxInstanceList`, `DescribeSandboxInstances`) through the signed
  raw request path, and `apivalue.Extend` keeps unknown response members.
- No client-side defaults: `KeepaliveConfiguration` may be absent entirely and
  the CLI never fills `{}`, a mode, a duration or `false`. `KeepaliveTimeout`
  keeps the spelling the caller sent (for example `600s`, `5m`, `30m`).
  `ContentFormat` is neither defaulted to `raw` nor case-normalized locally.

- Observed on the live service (ap-guangzhou, 2026-11-17):
  `CreateSandboxTool` rejects `LogConfiguration.CLSConfig` when
  `NetworkConfiguration.NetworkMode` is `SANDBOX` with
  `InvalidParameterValue.CLSConfig` and the message "LogConfiguration.CLSConfig
  cannot be used when NetworkMode is SANDBOX (sidecar requires network access to
  push logs to CLS)". A CLS topic therefore needs a network mode with egress; the
  CLI sends the user's choice unchanged and adds no validation of its own.
- Observed on the live service (ap-guangzhou, 2026-11-17): `CreateSandboxTool`
  rejects `LogConfiguration.CLSConfig` on the built-in tool types with
  `InvalidParameter` and the message "LogConfiguration with CLSConfig is only
  supported for custom sandbox type for now". A tool that ships logs to CLS is
  therefore `ToolType: custom` with a `CustomConfiguration`; the restriction is
  service-side ("for now"), and the CLI again sends the caller's choice
  unchanged. A CLS topic is not required for the rejection: an `auto`
  `ContentFormat` without a topic is rejected on any tool type with
  "LogConfiguration.CLSConfig.TopicId is required when ContentFormat is auto".

## Review status

Not yet accepted by an AGS API owner. The inheritance order (instance member >
tool current value > platform default `REQUEST`/`10m`), the `300s`-`1800s`
window and the `raw`/`auto` semantics are claims from the issue, verified in
this repository by a loopback fixture and by the registered live scenarios, not
by an API owner confirmation. The live scenarios prove configuration
persistence, per-member inheritance, normalization and cleanup; they do not
exercise the server-side log extraction engine, data-plane renewal or actual
log delivery.

Evidence boundary for the invalid-value matrix: service-side rejections are
observed through `agr api call` (raw pass-through), while unknown top-level
members are rejected locally by the request contract. Local rejections are
never reported as backend evidence.

## Exit condition

Delete the corresponding `api.patch.json` additions (keepalive members,
`/objects/KeepaliveConfiguration`, `LogConfiguration.ContentFormat`), remove the
`e2e-coverage.yaml` bindings and the two `sandbox.*` scenarios, then regenerate
both channels with `go run ./cmd/internal/cobragen` once the official AGS
contract or SDK carries these members. Re-check the affected commands, channels
and README sections at that point; the preview-only flags become stable behavior
and the `preview` labels must be removed.
