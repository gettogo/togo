import {
  apnsSend,
  isUnregistered,
  type ApnsEnvironment,
  type ApnsPushType,
  type ApnsResult,
} from "./apns";
import { json } from "./http";
import { ConfigError } from "./jwt";
import type { Admission } from "./ratelimit";

// The relay forwards sealed bytes it cannot read. It never logs the token,
// the ciphertext or the JWT; a 6-char hash prefix identifies a token in logs.

const MAX_BODY_BYTES = 8192;
const MAX_CIPHERTEXT = 3072;
const TOKEN_RE = /^[0-9a-f]{64,200}$/;
const CIPHERTEXT_RE = /^[A-Za-z0-9_-]+$/;
// Sent as a header; printable ASCII only so CR/LF can never split it.
const COLLAPSE_RE = /^[\x21-\x7e]{1,64}$/;
// Push-to-start is not supported yet: it needs the attributes-type and
// attributes fields, which the relay does not send, so APNs would refuse a
// start. The relay rejects one up front.
const LIVE_EVENTS = ["update", "end"] as const;
const MAX_STATE_STRING = 64;

export const TOKEN_LIMIT = 30;
export const IP_LIMIT = 300;
const WINDOW_MS = 60_000;

declare global {
  interface __BaseEnv_Env {
    // Test-only switch. Cloudflare always sets CF-Connecting-IP in front of a
    // deployed Worker, so a request without it is not from the edge.
    ALLOW_MISSING_CLIENT_IP?: string;
  }
}

interface ContentState {
  bib: string | null;
  agent: string;
  status: string;
  waiting: number;
  since: number;
}

interface PushBody {
  token: string;
  env: ApnsEnvironment;
  ciphertext?: string;
  collapseId?: string;
  kind: ApnsPushType;
  liveActivity?: { event: (typeof LIVE_EVENTS)[number]; contentState: ContentState };
}

class FieldError extends Error {
  constructor(readonly field: string) {
    super(field);
  }
}

export async function handlePush(request: Request, env: Env): Promise<Response> {
  let body: PushBody;
  try {
    body = parsePush(await readCapped(request));
  } catch (err) {
    if (err instanceof FieldError) return json(400, { error: "bad_request", field: err.field });
    throw err;
  }

  const ip = request.headers.get("cf-connecting-ip") ?? (env.ALLOW_MISSING_CLIENT_IP === "1" ? "test" : null);
  if (ip === null) return json(400, { error: "bad_request", field: "cf-connecting-ip" });
  const ipGate = await limiter(env, `ip:${ip}`).admit(IP_LIMIT, WINDOW_MS);
  if (ipGate.status === "limited") return limited(ipGate.retryAfter);

  // Keyed by env as well: a caller naming the wrong env gets BadDeviceToken
  // from APNs and must not be able to affect the token's real env.
  const tokHash = await sha256Hex(`${body.env}|${body.token}`);
  const tokLimiter = limiter(env, `tok:${tokHash.slice(0, 32)}`);
  const tokGate: Admission = await tokLimiter.admit(TOKEN_LIMIT, WINDOW_MS);
  if (tokGate.status === "unregistered") return json(410, { error: "unregistered" });
  if (tokGate.status === "limited") return limited(tokGate.retryAfter);

  const live = body.kind === "liveactivity";
  let result: ApnsResult;
  try {
    result = await apnsSend(env, {
      token: body.token,
      environment: body.env,
      pushType: body.kind,
      topic: live ? `${env.APNS_TOPIC}.push-type.liveactivity` : env.APNS_TOPIC,
      collapseId: body.collapseId,
      payload: apnsPayload(body),
    });
  } catch (err) {
    if (err instanceof ConfigError) {
      console.error("apns config invalid", err.message);
      return json(502, { error: "apns", reason: "relay_config" });
    }
    console.error("apns fetch failed", `tok=${tokHash.slice(0, 6)}`);
    return json(502, { error: "apns", reason: "unreachable" });
  }

  if (result.status === 200) return json(202, { apnsId: result.apnsId });
  console.warn("apns rejected", `tok=${tokHash.slice(0, 6)}`, result.status, result.reason);
  if (isUnregistered(result)) {
    await tokLimiter.markUnregistered();
    return json(410, { error: "unregistered" });
  }
  return json(502, { error: "apns", reason: result.reason });
}

function apnsPayload(body: PushBody): unknown {
  switch (body.kind) {
    case "alert":
      return {
        aps: { alert: { title: "Togo", body: "Needs you" }, "mutable-content": 1, sound: "default" },
        c: body.ciphertext,
      };
    case "background":
      return { aps: { "content-available": 1 }, c: body.ciphertext };
    case "liveactivity": {
      // Live Activity updates render in the system UI without app code, so
      // they carry metadata only; the ciphertext stays off this path.
      const la = body.liveActivity!;
      return {
        aps: {
          timestamp: Math.floor(Date.now() / 1000),
          event: la.event,
          "content-state": la.contentState,
        },
      };
    }
  }
}

async function readCapped(request: Request): Promise<unknown> {
  const declared = Number(request.headers.get("content-length") ?? "0");
  if (declared > MAX_BODY_BYTES) throw new FieldError("body");
  const buf = await request.arrayBuffer();
  if (buf.byteLength > MAX_BODY_BYTES) throw new FieldError("body");
  try {
    return JSON.parse(new TextDecoder().decode(buf));
  } catch {
    throw new FieldError("body");
  }
}

function parsePush(raw: unknown): PushBody {
  if (!isObject(raw)) throw new FieldError("body");
  const { token, env, ciphertext, collapseId, kind, liveActivity } = raw;
  if (typeof token !== "string" || !TOKEN_RE.test(token.toLowerCase())) {
    throw new FieldError("token");
  }
  if (env !== "sandbox" && env !== "production") throw new FieldError("env");
  // Live Activity pushes carry metadata only; a ciphertext there is ignored.
  const ciphertextOptional = kind === "liveactivity" && ciphertext === undefined;
  if (!ciphertextOptional && !validCiphertext(ciphertext)) throw new FieldError("ciphertext");
  if (collapseId !== undefined && (typeof collapseId !== "string" || !COLLAPSE_RE.test(collapseId))) {
    throw new FieldError("collapseId");
  }
  if (kind !== "alert" && kind !== "background" && kind !== "liveactivity") {
    throw new FieldError("kind");
  }
  const body: PushBody = { token: token.toLowerCase(), env, kind };
  if (kind !== "liveactivity") body.ciphertext = ciphertext as string;
  if (collapseId !== undefined) body.collapseId = collapseId as string;
  if (kind === "liveactivity") {
    body.liveActivity = parseLiveActivity(liveActivity);
  } else if (liveActivity !== undefined) {
    throw new FieldError("liveActivity");
  }
  return body;
}

function parseLiveActivity(raw: unknown): PushBody["liveActivity"] {
  if (!isObject(raw)) throw new FieldError("liveActivity");
  const event = raw.event;
  if (typeof event !== "string" || !(LIVE_EVENTS as readonly string[]).includes(event)) {
    throw new FieldError("liveActivity.event");
  }
  const cs = raw.contentState;
  if (!isObject(cs)) throw new FieldError("liveActivity.contentState");
  const bib = cs.bib === null ? null : boundedString(cs.bib, "bib");
  const state: ContentState = {
    bib,
    agent: boundedString(cs.agent, "agent"),
    status: boundedString(cs.status, "status"),
    waiting: nonNegativeInt(cs.waiting, "waiting"),
    since: nonNegativeInt(cs.since, "since"),
  };
  return { event: event as (typeof LIVE_EVENTS)[number], contentState: state };
}

function boundedString(v: unknown, name: string): string {
  if (typeof v !== "string" || v.length === 0 || v.length > MAX_STATE_STRING) {
    throw new FieldError(`liveActivity.contentState.${name}`);
  }
  return v;
}

function nonNegativeInt(v: unknown, name: string): number {
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0) {
    throw new FieldError(`liveActivity.contentState.${name}`);
  }
  return v;
}

function validCiphertext(v: unknown): v is string {
  return typeof v === "string" && v.length > 0 && v.length <= MAX_CIPHERTEXT && CIPHERTEXT_RE.test(v);
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function limiter(env: Env, key: string) {
  return env.RATE_LIMITER.get(env.RATE_LIMITER.idFromName(key));
}

function limited(retryAfter: number): Response {
  const res = json(429, { error: "rate_limited", retryAfter });
  res.headers.set("retry-after", String(retryAfter));
  return res;
}

async function sha256Hex(s: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}
