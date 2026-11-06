import { FieldError } from "./liveactivity";
import { validatePushBody, type PushBody } from "./push";

// A will is a set of pushes the helper sealed ahead of time and leaves with
// its mailbox. The relay sends them once if the helper's socket goes quiet
// for afterMs, so the phone learns the machine went silent. The relay cannot
// read them: each is the same body the helper would POST to /v1/push.
//
// Frame, sent on the authenticated mailbox socket:
//
//   {"type":"will","v":1,"afterMs":300000,"pushes":[<PushBody>, ...]}
//
// 0 to 8 pushes, afterMs 120000 to 3600000, at most 32768 bytes of UTF-8.
// An empty pushes array disarms; afterMs, if present, must still be in range
// and is ignored. A new will
// replaces the old one. A will older than 24 hours is dropped unsent.

export const WILL_MAX_FRAME = 32_768;
export const WILL_MAX_PUSHES = 8;
export const WILL_MIN_AFTER_MS = 120_000;
export const WILL_MAX_AFTER_MS = 3_600_000;
export const WILL_TTL_MS = 86_400_000;
// The helper rebuilds its will at most every 30 s; this leaves room for
// reconnects without letting one host churn storage.
export const WILL_SETS_PER_MINUTE = 20;

export interface StoredWill {
  pushes: PushBody[];
  afterMs: number;
  setAt: number;
}

const KEYS = new Set(["type", "v", "afterMs", "pushes"]);
// Lets the mailbox refuse a large frame without parsing it first.
const WILL_PREFIX_RE = /^\s*\{\s*"type"\s*:\s*"will"\s*,/;

export function looksLikeWill(frame: string): boolean {
  return WILL_PREFIX_RE.test(frame);
}

// null means refuse the frame.
export function parseWill(frame: unknown): StoredWill | "disarm" | null {
  if (typeof frame !== "object" || frame === null || Array.isArray(frame)) return null;
  const f = frame as Record<string, unknown>;
  if (Object.keys(f).some((k) => !KEYS.has(k))) return null;
  if (f.type !== "will" || f.v !== 1 || !Array.isArray(f.pushes)) return null;
  if (f.pushes.length > WILL_MAX_PUSHES) return null;
  const afterValid =
    typeof f.afterMs === "number" &&
    Number.isSafeInteger(f.afterMs) &&
    f.afterMs >= WILL_MIN_AFTER_MS &&
    f.afterMs <= WILL_MAX_AFTER_MS;
  if (f.pushes.length === 0) return f.afterMs === undefined || afterValid ? "disarm" : null;
  if (!afterValid) return null;
  const pushes: PushBody[] = [];
  for (const raw of f.pushes) {
    try {
      pushes.push(validatePushBody(raw));
    } catch (err) {
      if (err instanceof FieldError) return null;
      throw err;
    }
  }
  return { pushes, afterMs: f.afterMs as number, setAt: Date.now() };
}

// When the will next needs a look: the moment silence would reach afterMs,
// or its expiry if that comes first.
export function nextCheck(will: StoredWill, lastSeen: number): number {
  return Math.min(lastSeen + will.afterMs, will.setAt + WILL_TTL_MS);
}

export function due(will: StoredWill, lastSeen: number, now: number): "fire" | "wait" | "expired" {
  if (now - will.setAt >= WILL_TTL_MS) return "expired";
  if (now - lastSeen >= will.afterMs) return "fire";
  return "wait";
}
