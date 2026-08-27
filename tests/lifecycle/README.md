# Live lifecycle tests

The tests in this directory build the real `agr` binary and call the live AGS
control plane. They create remote resources and remove them during cleanup.

Credentials come from `TENCENTCLOUD_SECRET_ID` and
`TENCENTCLOUD_SECRET_KEY`, or from `~/.agr/config.toml`. Use
`AGR_REGION` and `AGR_CLOUD_ENDPOINT` when the target environment differs from
the CLI defaults.

The Deployment lifecycle test also requires
`AGR_TEST_DEPLOYMENT_TOOL_ID`. Set it to an existing Sandbox Tool that:

- is in the target account and region;
- has status `ACTIVE`; and
- has `Persistent` set to `true`.

The test reads that Tool but does not modify or delete it. It creates a
Deployment with `MinInstanceCount` set to zero, then exercises create, get,
list filtering, update, and waited deletion. The created Deployment is tracked
for cleanup even when a later assertion fails.

Run only the Deployment lifecycle test with:

```bash
export AGR_TEST_DEPLOYMENT_TOOL_ID=sdt-example
go test ./tests/lifecycle \
  -run TestLifecycle \
  -ginkgo.focus='Deployment CLI lifecycle' \
  -count=1 \
  -timeout 15m \
  -v
```

Without `AGR_TEST_DEPLOYMENT_TOOL_ID`, the Deployment spec is skipped while
the other lifecycle specs remain available.
