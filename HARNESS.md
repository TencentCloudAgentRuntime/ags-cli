# Managed Harness (preview)

[中文](HARNESS-zh.md)

These commands require a **preview build**. From a reviewed checkout, build
`go build -tags preview -o agr ./cmd/agr` and use `./agr` below. Configure
credentials, region and data-plane domain as described in the main README.

A ModelProvider registers models and their credential source. A Harness has an
editable draft and immutable revisions, published to a managed Tool and
Deployment. Prepare an accepted role, VPC subnet, security group and TokenHub
API key with model access in the selected region. Replace example variables
with reviewed resources; engine/version availability comes from the live list.

## Create and talk

~~~bash
agr harness engine list -o text
agr model-provider create --name demo-models \
  --tokenhub-api-key-id "$TOKENHUB_API_KEY_ID" --model hy3 -o text
~~~

Save the returned ID as `MODEL_PROVIDER_ID`. This example uses `pi` / `1.3.0`:

~~~bash
agr harness create --name demo-agent --engine pi --engine-version 1.3.0 \
  --model "$MODEL_PROVIDER_ID:hy3" --role-arn "$ROLE_ARN" \
  --subnet-id "$SUBNET_ID" --security-group-id "$SECURITY_GROUP_ID" --wait -o text
~~~

Save `HARNESS_ID` from the result. Creation saves a draft, creates revision 1
and starts publishing. If publication fails, inspect and retry the same revision
after fixing the reported cause, rather than creating another Harness:

~~~bash
agr harness status "$HARNESS_ID" -o text
agr harness revision get "$HARNESS_ID" 1 -o text
agr harness revision publish "$HARNESS_ID" 1 --wait -o text
~~~

`PUBLISHED` confirms active platform resources. A readiness probe can start an
instance. `prompt` defaults to `/workspace`; use `--cwd` to choose another
directory that exists inside the instance, and keep it unchanged when continuing
a session. With `harness acp`, the editor supplies `cwd`: its path is forwarded
unchanged and must also exist inside the remote instance.

~~~bash
agr harness endpoint "$HARNESS_ID" --check -o text
agr harness prompt "$HARNESS_ID" 'Reply only HELLO' -o json
~~~

Copy `Data.SessionId` and `Data.AffinityId` into `SESSION_ID` and
`AFFINITY_ID` to continue the same conversation on the same instance:

~~~bash
agr harness prompt "$HARNESS_ID" 'What did I ask you to reply with?' \
  --session-id "$SESSION_ID" --affinity-id "$AFFINITY_ID" -o json
agr harness prompt "$HARNESS_ID" - -o ndjson < task.md
~~~

`--timeout` covers endpoint/token preparation, ACP initialization, session setup
and the turn. Ctrl+C or `--timeout` requests cancellation; the remote turn may still finish.
Unfinished turns are never automatically resent. JSON/NDJSON failures preserve
available session and affinity identifiers.
`harness proxy --port 0 -o ndjson` emits the local ACP address in its first
`ready` event and runs until interrupted; single-envelope `-o json` is
unsupported. `harness acp` provides stdio ACP for editors.

## Open the DSH Web console

For a Harness with a **PUBLISHED DSH revision** and an **ACTIVE Deployment**:

~~~bash
agr harness web "$HARNESS_ID" -o text
~~~

The CLI opens the native DSH console at the Deployment's HTTPS Webview root on
service port **3080**, using the configured region and `--domain`. The existing
ACP endpoint remains on 8080 at `/acp`. The published revision determines the
engine even when the draft has changed.

Before opening a browser on the machine running the CLI, the command waits for
HTTP 200 on the native Web entry. Preparation and readiness share a 90-second
deadline; use `--timeout 2m` to change it, or Ctrl+C to cancel. Only startup
HTTP 404/502/503 responses are retried; authentication errors fail immediately.
The gateway's confirmed affinity is carried into the browser URL. The readiness
request may start an instance under the Deployment's existing policy. The command
exits after requesting the launch; `Opened` confirms that launch request, not
successful rendering in the browser. No local proxy or running CLI process is needed.

~~~bash
# Safe URL only: no browser and no token acquisition.
agr harness web "$HARNESS_ID" --no-open -o json
# SSH/headless: explicitly return a login link to copy into your browser.
agr harness web "$HARNESS_ID" --no-open --show-token -o json \
  --jq '.Data | {AccessURL, ExpiresAt}'
# Request an affinity when entering the console.
agr harness web "$HARNESS_ID" --affinity-id "$AFFINITY_ID" -o text
~~~

`WebURL` never contains a token. Only `--show-token` includes the
credential-bearing `AccessURL` and `ExpiresAt` in text/JSON output; protect that
link. Rerun the command when the token or browser authentication expires.
`--no-open`, including `--no-open --show-token`, skips readiness requests and
does not start an instance; the browser may encounter a cold start on first use.
`--affinity-id` sets the initial `affinity_id` query parameter; subsequent browser
requests use the gateway cookie. Tabs on the same host share that cookie and
do not provide instance isolation. The command does not publish revisions or
change Deployment configuration.

## Add a real MCP server

Inspect a ready, fixed market version and its required headers/credentials:

~~~bash
agr harness tool list --type mcp --all -o text
agr harness tool get "$RECORD_ID/$VERSION_ID" -o text
agr harness update "$HARNESS_ID" --mcp "docs=MARKETPLACE/$RECORD_ID/$VERSION_ID" -o text
agr harness revision create "$HARNESS_ID" --publish --wait -o text
~~~

The server must support Streamable HTTP. If required, use the credential/header
flags shown by `tool get`. Keep secrets out of logs and source control.
`--mcp-servers @file` can read an existing protected JSON configuration and
cannot be mixed with the shortcut flags.

On a new instance, request an actual read-only tool invocation. A model answer
alone is insufficient: inspect streamed `tool_call` / `tool_call_update`
events and the completed tool result. `harness prompt` declines tool permission
requests; use an interactive ACP client when approval is required.

## Publish and roll back

~~~bash
agr harness update "$HARNESS_ID" --system-prompt @prompt.md -o text
agr harness revision diff "$HARNESS_ID" live draft -o text
agr harness revision create "$HARNESS_ID" --publish --wait -o text
agr harness revision publish "$HARNESS_ID" 1 --wait -o text
~~~

Updates edit the draft. Creating a revision freezes it and pins model-provider
revisions. Republishing history is a rollback; publishing an already-published
revision is a no-op.

**Publication affects newly created instances. Existing instances and sessions
retain their startup configuration. A new session or ACP connection can reuse
an existing instance.** Account for the Deployment's scaling/affinity policy
when verifying publication. The opt-in tests use distinct exclusive instances
and also assert that older snapshots remain unchanged.

## Limits and security

- Each MCP server accepts one managed credential. Every `${credential}` template
  for that server uses the same credential; other secret header values are stored
  as plaintext in the Harness configuration and its revisions. Create/update warn
  about non-empty plaintext values on headers whose names contain `Authorization`,
  `Key`, `Token` or `Secret` (case-insensitive), unless the value uses `${credential}`.
  `revision diff` and `status` hide all MCP header values, including credential
  templates; requests and stored values remain unchanged. Other configuration
  read/preview outputs can contain plaintext.
- MCP names use 1-32 ASCII letters, digits, `_` or `-`. Pi treats `-` and `_` as
  equivalent, so names such as `my-tool` and `my_tool` collide.
- Sessions from the same Harness share its Harness ID as the session-space
  `UserId`; there is no per-end-user isolation within that Harness. Use a single
  writer per session and send turns serially.
- Harness and ModelProvider configurations store references to TokenHub keys.
  At publication, the resolved key is written as a literal into the Sandbox
  Tool's `NetworkPolicy` header-injection rule. Identities allowed to describe
  that Tool can read the key; scope Tool read permissions accordingly.

## Clean up owned resources

Read and save `TOOL_ID` before deleting the Harness:

~~~bash
agr harness get "$HARNESS_ID" -o json
agr harness delete "$HARNESS_ID" --yes --wait -o text
# Only remove resources you own and no longer need.
agr tool delete "$TOOL_ID" --yes --wait -o text
agr model-provider delete "$MODEL_PROVIDER_ID" --yes -o text
~~~

Deletion removes the Deployment and Harness revisions but keeps the Tool, model
providers, session spaces, Registry assets and credentials. Inspect and stop
independently created test instances explicitly; never remove existing
resources just to restore a test count.

See [the opt-in live E2E instructions](internal/patchscenarios/HARNESS-E2E.md)
for repeatable runtime/MCP validation and cleanup.
