// Public, stateless metadata fixture. No credentials, tool execution, or proxies.
// Path timestamps make changes deterministic across instances and cold starts.
export async function handle(req: Request): Promise<Response> {
  const url = new URL(req.url);
  const match = url.pathname.match(
    /^\/case\/(\d+)\/(\d+)(\/(?:mcp|agent\.json))$/,
  );
  const changeAt = Number(match?.[1] ?? "0");
  const failAt = Number(match?.[2] ?? "0");
  const path = match?.[3] ?? url.pathname;
  if (failAt > 0 && Date.now() >= failAt) {
    return new Response("fixture unavailable", { status: 503 });
  }
  const version = changeAt > 0 && Date.now() >= changeAt ? "2.0.0" : "1.0.0";
  const headers = { "Cache-Control": "no-store" };
  if (path === "/health") {
    return Response.json({ fixture: "registry", version }, { headers });
  }
  if (path === "/agent.json" && req.method === "GET") {
    return Response.json({
      name: "registry-fixture",
      description: "Disposable metadata fixture",
      version,
      supportedInterfaces: [{
        url: `${url.origin}/agent`,
        protocolBinding: "JSONRPC",
        protocolVersion: "1.0",
      }],
      capabilities: {},
      defaultInputModes: ["text/plain"],
      defaultOutputModes: ["text/plain"],
      skills: [{
        id: "echo",
        name: "Echo",
        description: "Fixture only",
        tags: ["test"],
      }],
    }, { headers });
  }
  if (path !== "/mcp") {
    return new Response("Not found", { status: 404 });
  }
  if (req.method !== "POST") return new Response(null, { status: 405 });
  let rpc;
  try {
    rpc = await req.json();
  } catch {
    return new Response("Invalid JSON", { status: 400 });
  }
  if (rpc.method === "notifications/initialized") {
    return new Response(null, { status: 202 });
  }
  let result;
  switch (rpc.method) {
    case "initialize":
      result = {
        protocolVersion: "2024-11-05",
        capabilities: { tools: {} },
        serverInfo: {
          name: "registry-fixture",
          title: "Disposable metadata fixture",
          version,
        },
      };
      break;
    case "tools/list":
      result = {
        tools: [{
          name: "echo",
          description: "Fixture only",
          inputSchema: { type: "object", properties: {} },
        }],
      };
      break;
    default:
      return Response.json({
        jsonrpc: "2.0",
        id: rpc.id,
        error: { code: -32601, message: "Method not found" },
      }, { headers });
  }
  return Response.json({ jsonrpc: "2.0", id: rpc.id, result }, { headers });
}
if (import.meta.main) Deno.serve(handle);
