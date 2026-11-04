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
