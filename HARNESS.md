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

Ctrl+C or `--timeout` requests cancellation; the remote turn may still finish.
Unfinished turns are never automatically resent. JSON/NDJSON failures preserve
available session and affinity identifiers.
`harness proxy --port 0 -o ndjson` emits the local ACP address in its first
`ready` event and runs until interrupted; single-envelope `-o json` is
unsupported. `harness acp` provides stdio ACP for editors.

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
