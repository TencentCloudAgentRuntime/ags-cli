# Opt-in Harness live E2E

Ordinary tests skip live cases. Run them only on an approved test runner with
authorization to create billable resources and invoke a real model. Provide
cloud credentials through the existing CLI mechanism, outside source control
and logs. Optional `AGR_HARNESS_E2E_CONFIG` points to an existing CLI config;
environment-only credentials avoid copied credential files.

Build the preview CLI and tests from the same reviewed source:

~~~bash
mkdir -p build/live
go build -tags preview -o build/live/agr-preview ./cmd/agr
go test -c -tags preview -o build/live/harness-live.test ./internal/patchscenarios
~~~

Set reviewed inputs in the selected account and region:

~~~bash
export AGR_HARNESS_E2E_BINARY="$(pwd)/build/live/agr-preview"
export AGR_REGION=ap-chongqing
export AGR_HARNESS_ROLE_ARN='qcs::cam::uin/100000000001:roleName/ReviewedTestRole'
export AGR_HARNESS_SUBNET_ID=subnet-reviewed
export AGR_HARNESS_SECURITY_GROUP_ID=sg-reviewed
export AGR_HARNESS_TOKENHUB_API_KEY_ID=key-reviewed
export AGR_HARNESS_MODEL=hy3
export AGR_HARNESS_ENGINE=pi
export AGR_HARNESS_ENGINE_VERSION=1.3.0
export AGR_HARNESS_RUNTIME_E2E=1
~~~

Replace identifiers and select an available engine version.
`AGR_HARNESS_INITIALIZE_CONFIGURATION` can supply additional reviewed JSON,
such as CPU/memory. Runtime E2E retains those Tool settings and uses Min=0,
Max=6, `EXCLUSIVE_BEST_EFFORT` for six independent ACP clients. It checks
distinct instance IDs, rather than assuming a new session means a new instance.

For MCP, choose a ready fixed MARKETPLACE version of a read-only server. Supply
all four inputs; the test names the server `live`:

~~~bash
export AGR_HARNESS_MCP_E2E=1
export AGR_HARNESS_MCP_REFERENCE='MARKETPLACE/rec-reviewed/rv-reviewed'
export AGR_HARNESS_MCP_TOOL=microsoft_docs_search
export AGR_HARNESS_MCP_EXPECT=learn.microsoft.com/en-us/azure
export AGR_HARNESS_MCP_PROMPT='Call the exact MCP tool mcp__live__microsoft_docs_search through codemode. Execute:
const result = await tools.mcp__live__microsoft_docs_search({query: "Azure Blob Storage documentation"});
if (result.isError) throw new Error(JSON.stringify(result.content));
return result;
Then return the first result URL. Do not substitute a different tool or invent a URL.'
~~~

This example fits a reviewed Microsoft Learn record; it does not automatically
select one. Inspect it with `agr harness tool get` first. The test requires a
completed matching tool call **and matching content in a tool-result update**.
A model answer alone cannot pass. Pi uses server-qualified tool names through
`codemode`; adapt the prompt to the reviewed engine. Select a result marker
specific to successful content, rather than the MCP endpoint's own address.

Run the three cases together:

~~~bash
build/live/harness-live.test \
  -test.run '^TestHarness(Live|RuntimeLive|MCPLive)$' -test.v -test.timeout 30m
~~~

Alternatively use `go test -count=1 -v -timeout 30m ./internal/patchscenarios`
with the same opt-ins. Disable caching and report skipped cases separately.

Runtime E2E checks real prompts, conversation continuation, NDJSON success and
SIGINT cancellation, stdio ACP initialization/EOF, proxy initialization,
connection deletion and shutdown, JSON-output rejection, model behavior through
revision 1 → 2 → 1, new instances and retained older snapshots.
MCP E2E separately creates, publishes and invokes the pinned real server.

Cleanup is registered before creation. It enumerates only each owned Tool's
instances, stops and checks them, deletes the owned Harness, Tool and provider,
then checks bound resources are absent. Existing credentials, market assets
and unrelated resources remain. Keep private execution evidence separate from
public PR summaries.
