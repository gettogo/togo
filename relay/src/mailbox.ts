import { DurableObject } from "cloudflare:workers";
import { constantTimeEqual, decodeSecret, HOST_ID_RE, hostIdFromSecret } from "./hostid";
import { json } from "./http";

// Phones post sealed answers here; the host's helper holds one outbound
// WebSocket and acks what it has handled. The relay stores ciphertext it
// cannot open and never logs it, the secret, or a full host id.

export const MESSAGE_TTL_MS = 600_000;
export const SWEEP_INTERVAL_MS = 60_000;
export const MAX_PENDING = 50;
export const REPLACED_CODE = 4000;
const MAX_CIPHERTEXT = 4096;
const MAX_BODY_BYTES = 6144;
// An ack frame is tiny; anything much larger is not from a well-behaved helper.
const MAX_FRAME_BYTES = 1024;

export const POST_IP_LIMIT = 60;
export const POST_HOST_LIMIT = 120;
export const CONNECT_IP_LIMIT = 30;
const WINDOW_MS = 60_000;

const PREFIX = "/v1/mailbox/";
const CIPHERTEXT_RE = /^[A-Za-z0-9_-]+$/;
const ULID_RE = /^[0-9A-HJKMNP-TV-Z]{26}$/;

interface Stored {
  ciphertext: string;
  receivedAt: number;
}

export type Enqueued = { status: "ok"; id: string } | { status: "full" };

export async function handleMailbox(request: Request, env: Env): Promise<Response> {
  const rest = new URL(request.url).pathname.slice(PREFIX.length);
  const [hostId, tail, ...extra] = rest.split("/");
  if (extra.length > 0 || (tail !== undefined && tail !== "ws")) return notFound();
  if (!HOST_ID_RE.test(hostId)) return notFound();
  const route = tail === "ws" ? connect : post;
  const method = tail === "ws" ? "GET" : "POST";
  if (request.method !== method) {
    const res = json(405, { error: "method_not_allowed" });
    res.headers.set("allow", method);
    return res;
  }
  return route(request, env, hostId);
}

async function post(request: Request, env: Env, hostId: string): Promise<Response> {
  const ciphertext = await readCiphertext(request);
  if (ciphertext === null) return json(400, { error: "bad_request", field: "ciphertext" });

  const ip = clientIp(request, env);
  if (ip === null) return json(400, { error: "bad_request", field: "cf-connecting-ip" });
  const ipGate = await admit(env, `mbip:${ip}`, POST_IP_LIMIT);
  if (ipGate !== null) return ipGate;
  const hostGate = await admit(env, `mbhost:${hostId}`, POST_HOST_LIMIT);
  if (hostGate !== null) return hostGate;

  const res = await mailbox(env, hostId).enqueue(ciphertext);
  if (res.status === "full") return json(507, { error: "mailbox_full" });
  return json(202, { id: res.id });
}

async function connect(request: Request, env: Env, hostId: string): Promise<Response> {
  if (request.headers.get("upgrade")?.toLowerCase() !== "websocket") {
    return json(426, { error: "upgrade_required" });
  }
  const ip = clientIp(request, env);
  if (ip === null) return json(400, { error: "bad_request", field: "cf-connecting-ip" });
  const gate = await admit(env, `mbws:${ip}`, CONNECT_IP_LIMIT);
  if (gate !== null) return gate;

  const auth = request.headers.get("authorization") ?? "";
  const secret = auth.startsWith("Bearer ") ? decodeSecret(auth.slice(7)) : null;
  const derived = secret === null ? "" : await hostIdFromSecret(secret);
  if (!constantTimeEqual(derived, hostId)) return json(401, { error: "unauthorized" });

  // The Authorization header carries the secret; the object does not need it.
  const headers = new Headers({ upgrade: "websocket" });
  return mailbox(env, hostId).fetch(new Request(request.url, { headers }));
}

async function readCiphertext(request: Request): Promise<string | null> {
  const declared = Number(request.headers.get("content-length") ?? "0");
  if (declared > MAX_BODY_BYTES) return null;
  const buf = await request.arrayBuffer();
  if (buf.byteLength > MAX_BODY_BYTES) return null;
  let body: unknown;
  try {
    body = JSON.parse(new TextDecoder().decode(buf));
  } catch {
    return null;
  }
  if (typeof body !== "object" || body === null) return null;
  const { ciphertext } = body as Record<string, unknown>;
  if (typeof ciphertext !== "string") return null;
  if (ciphertext.length === 0 || ciphertext.length > MAX_CIPHERTEXT) return null;
  return CIPHERTEXT_RE.test(ciphertext) ? ciphertext : null;
}

function clientIp(request: Request, env: Env): string | null {
  return request.headers.get("cf-connecting-ip") ?? (env.ALLOW_MISSING_CLIENT_IP === "1" ? "test" : null);
}

async function admit(env: Env, key: string, limit: number): Promise<Response | null> {
  const stub = env.RATE_LIMITER.get(env.RATE_LIMITER.idFromName(key));
  const gate = await stub.admit(limit, WINDOW_MS);
  if (gate.status !== "limited") return null;
  const res = json(429, { error: "rate_limited", retryAfter: gate.retryAfter });
  res.headers.set("retry-after", String(gate.retryAfter));
  return res;
}

function mailbox(env: Env, hostId: string) {
  return env.MAILBOX.get(env.MAILBOX.idFromName(hostId));
}

function notFound(): Response {
  return json(404, { error: "not_found" });
}

// One instance per host id. Messages live in storage as `msg:<ulid>` so a
// prefix list returns them in arrival order; the hibernation API keeps the
// helper's socket open without holding the object in memory.
export class Mailbox extends DurableObject<Env> {
  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair('{"type":"ping"}', '{"type":"pong"}'));
  }

  async enqueue(ciphertext: string): Promise<Enqueued> {
    const now = Date.now();
    const pending = await this.live(now);
    if (pending.size >= MAX_PENDING) return { status: "full" };
    const id = ulid(now);
    const msg: Stored = { ciphertext, receivedAt: now };
    await this.ctx.storage.put(`msg:${id}`, msg);
    if ((await this.ctx.storage.getAlarm()) === null) {
      await this.ctx.storage.setAlarm(now + SWEEP_INTERVAL_MS);
    }
    for (const ws of this.ctx.getWebSockets()) send(ws, id, msg);
    return { status: "ok", id };
  }

  async fetch(request: Request): Promise<Response> {
    if (request.headers.get("upgrade") !== "websocket") return json(426, { error: "upgrade_required" });
    for (const old of this.ctx.getWebSockets()) {
      try {
        old.close(REPLACED_CODE, "replaced");
      } catch {
        // Already closing; nothing to replace.
      }
    }
    const { 0: client, 1: server } = new WebSocketPair();
    this.ctx.acceptWebSocket(server);
    for (const [key, msg] of await this.live(Date.now())) send(server, key.slice(4), msg);
    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void> {
    if (typeof message !== "string" || message.length > MAX_FRAME_BYTES) {
      ws.close(1003, "unsupported frame");
      return;
    }
    let frame: unknown;
    try {
      frame = JSON.parse(message);
    } catch {
      return;
    }
    if (typeof frame !== "object" || frame === null) return;
    const { type, id } = frame as Record<string, unknown>;
    if (type === "ack" && typeof id === "string" && ULID_RE.test(id)) {
      await this.ctx.storage.delete(`msg:${id}`);
    } else if (type === "ping") {
      ws.send('{"type":"pong"}');
    }
  }

  async webSocketClose(ws: WebSocket): Promise<void> {
    try {
      ws.close(1000, "closing");
    } catch {
      // The runtime may already have completed the close handshake.
    }
  }

  async alarm(): Promise<void> {
    const remaining = await this.live(Date.now());
    if (remaining.size > 0) await this.ctx.storage.setAlarm(Date.now() + SWEEP_INTERVAL_MS);
  }

  // Returns unexpired messages and deletes the rest, so an expired message is
  // never delivered even between sweeps.
  private async live(now: number): Promise<Map<string, Stored>> {
    const all = await this.ctx.storage.list<Stored>({ prefix: "msg:" });
    const expired: string[] = [];
    for (const [key, msg] of all) {
      if (now - msg.receivedAt >= MESSAGE_TTL_MS) {
        expired.push(key);
        all.delete(key);
      }
    }
    if (expired.length > 0) await this.ctx.storage.delete(expired);
    return all;
  }
}

function send(ws: WebSocket, id: string, msg: Stored): void {
  try {
    ws.send(JSON.stringify({ type: "msg", id, ciphertext: msg.ciphertext, receivedAt: msg.receivedAt }));
  } catch {
    // A socket that is closing gets the message on the next connect.
  }
}

const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

// 48-bit millisecond time then 80 random bits; ids sort by arrival time.
function ulid(now: number): string {
  let time = "";
  let t = now;
  for (let i = 0; i < 10; i++) {
    time = CROCKFORD[t % 32] + time;
    t = Math.floor(t / 32);
  }
  const rand = crypto.getRandomValues(new Uint8Array(16));
  let tail = "";
  for (let i = 0; i < 16; i++) tail += CROCKFORD[rand[i] & 31];
  return time + tail;
}
