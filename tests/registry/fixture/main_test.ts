import { handle } from "./main.ts";

Deno.test("metadata changes without shared state", async () => {
  for (
    const [changeAt, expected] of [[Date.now() + 60000, "1.0.0"], [1, "2.0.0"]]
  ) {
    const mcp = await handle(
      new Request(`https://fixture.example/case/${changeAt}/0/mcp`, {
        method: "POST",
        body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize" }),
      }),
    );
    if ((await mcp.json()).result.serverInfo.version !== expected) {
      throw new Error("MCP version mismatch");
    }
    const agent = await handle(
      new Request(`https://fixture.example/case/${changeAt}/0/agent.json`),
    );
    const card = await agent.json();
    if (
      card.version !== expected ||
      card.supportedInterfaces[0].protocolVersion !== "1.0"
    ) throw new Error("A2A version mismatch");
  }
});
Deno.test("failure and unknown routes are deterministic", async () => {
  if (
    (await handle(new Request("https://fixture.example/case/0/1/mcp")))
      .status !== 503
  ) throw new Error("missing failure");
  if (
    (await handle(new Request("https://fixture.example/anything"))).status !==
      404
  ) throw new Error("unexpected route");
});
