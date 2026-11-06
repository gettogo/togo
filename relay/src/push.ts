import {
  apnsSend,
  isUnregistered,
  type ApnsEnvironment,
  type ApnsPushType,
  type ApnsResult,
} from "./apns";
import { json } from "./http";
import { ConfigError } from "./jwt";
import { FieldError, liveActivityPayload, parseLiveActivity, type LiveActivity } from "./liveactivity";
import type { Admission } from "./ratelimit";

// The relay forwards sealed bytes it cannot read. It never logs the token,
// the ciphertext or the JWT; a 6-char hash prefix identifies a token in logs.

const MAX_BODY_BYTES = 8192;
const MAX_CIPHERTEXT = 3072;
const TOKEN_RE = /^[0-9a-f]{64,200}$/;
const CIPHERTEXT_RE = /^[A-Za-z0-9_-]+$/;
// Sent as a header; printable ASCII only so CR/LF can never split it.
const COLLAPSE_RE = /^[\x21-\x7e]{1,64}$/;

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

export interface PushBody {
  token: string;
  env: ApnsEnvironment;
  ciphertext?: string;
  collapseId?: string;
  kind: ApnsPushType;
  target: "app" | "watch";
  liveActivity?: LiveActivity;
}

export type SendResult =
  | { status: "sent"; apnsId: string }
  | { status: "limited"; retryAfter: number }
  | { status: "unregistered" }
  | { status: "failed"; reason: string };

export async function handlePush(request: Request, env: Env): Promise<Response> {
  let body: PushBody;
  try {
    body = validatePushBody(await readCapped(request));
  } catch (err) {
    if (err instanceof FieldError) return json(400, { error: "bad_request", field: err.field });
    throw err;
  }

  const ip = request.headers.get("cf-connecting-ip") ?? (env.ALLOW_MISSING_CLIENT_IP === "1" ? "test" : null);
  if (ip === null) return json(400, { error: "bad_request", field: "cf-connecting-ip" });
  const ipGate = await limiter(env, `ip:${ip}`).admit(IP_LIMIT, WINDOW_MS);
  if (ipGate.status === "limited") return limited(ipGate.retryAfter);

  const result = await sendOne(env, body);
  switch (result.status) {
    case "sent":
      return json(202, { apnsId: result.apnsId });
    case "limited":
      return limited(result.retryAfter);
    case "unregistered":
      return json(410, { error: "unregistered" });
    case "failed":
      return json(502, { error: "apns", reason: result.reason });
  }
}

// Sends one validated body to APNs under the per-token limit. Used by
// POST /v1/push and by the mailbox when a will fires.
export async function sendOne(env: Env, body: PushBody): Promise<SendResult> {
  // Keyed by env as well: a caller naming the wrong env gets BadDeviceToken
  // from APNs and must not be able to affect the token's real env.
  const tokHash = await sha256Hex(`${body.env}|${body.token}`);
  const tokLimiter = limiter(env, `tok:${tokHash.slice(0, 32)}`);
  const tokGate: Admission = await tokLimiter.admit(TOKEN_LIMIT, WINDOW_MS);
  if (tokGate.status === "unregistered") return { status: "unregistered" };
  if (tokGate.status === "limited") return { status: "limited", retryAfter: tokGate.retryAfter };

  let result: ApnsResult;
  try {
    result = await apnsSend(env, {
      token: body.token,
      environment: body.env,
      pushType: body.kind,
      topic: topicFor(env, body),
      collapseId: body.collapseId,
      payload: apnsPayload(body),
    });
  } catch (err) {
    if (err instanceof ConfigError) {
      console.error("apns config invalid", err.message);
      return { status: "failed", reason: "relay_config" };
    }
    console.error("apns fetch failed", `tok=${tokHash.slice(0, 6)}`);
    return { status: "failed", reason: "unreachable" };
  }

  if (result.status === 200) return { status: "sent", apnsId: result.apnsId };
  console.warn("apns rejected", `tok=${tokHash.slice(0, 6)}`, result.status, result.reason);
  if (isUnregistered(result)) {
    await tokLimiter.markUnregistered();
    return { status: "unregistered" };
  }
  return { status: "failed", reason: result.reason ?? `status_${result.status}` };
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
    case "liveactivity":
      return liveActivityPayload(body.liveActivity!, Math.floor(Date.now() / 1000));
  }
}

function topicFor(env: Env, body: PushBody): string {
  if (body.kind === "liveactivity") return `${env.APNS_TOPIC}.push-type.liveactivity`;
  return body.target === "watch" ? `${env.APNS_TOPIC}.watchkitapp` : env.APNS_TOPIC;
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

// Throws FieldError naming the first bad field.
export function validatePushBody(raw: unknown): PushBody {
  if (!isObject(raw)) throw new FieldError("body");
  const { token, env, ciphertext, collapseId, kind, liveActivity, target } = raw;
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
  if (target !== undefined && target !== "app" && target !== "watch") throw new FieldError("target");
  if (kind === "liveactivity" && target === "watch") throw new FieldError("target");
  const body: PushBody = { token: token.toLowerCase(), env, kind, target: target ?? "app" };
  if (kind !== "liveactivity") body.ciphertext = ciphertext as string;
  if (collapseId !== undefined) body.collapseId = collapseId as string;
  if (kind === "liveactivity") {
    body.liveActivity = parseLiveActivity(liveActivity);
  } else if (liveActivity !== undefined) {
    throw new FieldError("liveActivity");
  }
  return body;
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
