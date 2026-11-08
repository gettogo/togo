export type Handler = (request: Request, env: Env) => Response | Promise<Response>;

interface Route {
  method: string;
  pattern: string;
  handler: Handler;
}

export function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

export class Router {
  private routes: Route[] = [];

  route(method: string, pattern: string, handler: Handler): this {
    this.routes.push({ method, pattern, handler });
    return this;
  }

  async handle(request: Request, env: Env): Promise<Response> {
    const { pathname } = new URL(request.url);
    const matches = this.routes.filter((r) => r.pattern === pathname);
    if (matches.length === 0) return json(404, { error: "not_found" });
    const match = matches.find((r) => r.method === request.method);
    if (!match) {
      const res = json(405, { error: "method_not_allowed" });
      res.headers.set("allow", matches.map((r) => r.method).join(", "));
      return res;
    }
    return match.handler(request, env);
  }
}

// readBodyCapped reads at most max bytes of the request body, whatever its
// content-length says: a chunked body with no length header is cut off as
// soon as it passes the cap instead of being buffered in full. Returns
// null when the body is too large.
export async function readBodyCapped(request: Request, max: number): Promise<Uint8Array | null> {
  const declared = Number(request.headers.get("content-length") ?? "0");
  if (declared > max) return null;
  if (!request.body) return new Uint8Array(0);
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > max) {
      await reader.cancel().catch(() => {});
      return null;
    }
    chunks.push(value);
  }
  const out = new Uint8Array(total);
  let at = 0;
  for (const c of chunks) {
    out.set(c, at);
    at += c.byteLength;
  }
  return out;
}
