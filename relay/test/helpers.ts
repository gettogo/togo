import { env } from "cloudflare:test";
import { vi } from "vitest";
import worker from "../src/index";

export interface Captured {
  url: string;
  headers: Headers;
  body: unknown;
}

export function randomToken(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
}

export const CIPHERTEXT = "AQ" + "x".repeat(100) + "_-Z";

// Stands in for APNs: records each request and answers with `reply`.
export function mockApns(reply: () => Response = () => new Response(null, { status: 200 })) {
  const calls: Captured[] = [];
  const spy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const req = new Request(input as RequestInfo, init as RequestInit);
    calls.push({ url: req.url, headers: req.headers, body: await req.json() });
    return reply();
  });
  return { calls, spy };
}

export function push(
  body: unknown,
  ip: string | null = crypto.randomUUID(),
  envOverride: Partial<Env> = {},
): Promise<Response> {
  const headers: Record<string, string> = { "content-type": "application/json" };
  if (ip !== null) headers["cf-connecting-ip"] = ip;
  const req = new Request("https://relay.test/v1/push", {
    method: "POST",
    headers,
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
  return worker.fetch(req as Request<unknown, IncomingRequestCfProperties>, { ...env, ...envOverride });
}
