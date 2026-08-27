# Live lifecycle tests

The tests in this directory build the real `agr` binary and call the live AGS
control plane. They create remote resources and remove them during cleanup.

Credentials come from `TENCENTCLOUD_SECRET_ID` and
`TENCENTCLOUD_SECRET_KEY`, or from `~/.agr/config.toml`. Use
`AGR_REGION` and `AGR_CLOUD_ENDPOINT` when the target environment differs from
the live-test defaults. Most live tests retain the CLI's `ap-guangzhou` default;
the Deployment lifecycle test defaults to `ap-shanghai`.

The Deployment lifecycle test creates its own persistent `mobile` Sandbox Tool
and waits for it to become active. It then creates a Deployment with
`MinInstanceCount` set to zero and exercises create, get, list filtering,
update, and waited deletion. Both resources are tracked and deleted, including
when a later assertion fails.

Run only the Deployment lifecycle test with:

```bash
go test ./tests/lifecycle \
  -run TestLifecycle \
  -ginkgo.focus='Deployment CLI lifecycle' \
  -count=1 \
  -timeout 15m \
  -v
```
