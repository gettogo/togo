import { apnsJwt, dropCachedJwt } from "./jwt";

export type ApnsEnvironment = "sandbox" | "production";
export type ApnsPushType = "alert" | "background" | "liveactivity";

export interface ApnsRequest {
  token: string;
  environment: ApnsEnvironment;
  pushType: ApnsPushType;
  topic: string;
  collapseId?: string;
  payload: unknown;
}

export interface ApnsResult {
  status: number;
  apnsId: string;
  reason: string | null;
}

const HOSTS: Record<ApnsEnvironment, string> = {
  sandbox: "https://api.sandbox.push.apple.com",
  production: "https://api.push.apple.com",
};

// Background pushes must use priority 5 or APNs rejects them.
const PRIORITY: Record<ApnsPushType, string> = {
  alert: "10",
  background: "5",
  liveactivity: "10",
};

export async function apnsSend(env: Env, req: ApnsRequest): Promise<ApnsResult> {
  const jwt = await apnsJwt(env);
  const apnsId = crypto.randomUUID();
  const headers: Record<string, string> = {
    authorization: `bearer ${jwt}`,
    "apns-id": apnsId,
    "apns-push-type": req.pushType,
    "apns-topic": req.topic,
    "apns-priority": PRIORITY[req.pushType],
    "content-type": "application/json",
  };
  if (req.collapseId) headers["apns-collapse-id"] = req.collapseId;
  const res = await fetch(`${HOSTS[req.environment]}/3/device/${req.token}`, {
    method: "POST",
    headers,
    body: JSON.stringify(req.payload),
  });
  let reason: string | null = null;
  if (res.status !== 200) {
    reason = await readReason(res);
    if (reason === "ExpiredProviderToken" || reason === "InvalidProviderToken") dropCachedJwt();
  } else {
    await res.body?.cancel();
  }
  return { status: res.status, apnsId: res.headers.get("apns-id") ?? apnsId, reason };
}

async function readReason(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { reason?: unknown };
    if (typeof body.reason === "string" && /^[A-Za-z]{1,64}$/.test(body.reason)) {
      return body.reason;
    }
  } catch {
    // APNs always answers errors with JSON; anything else is a proxy fault.
  }
  return `status_${res.status}`;
}

// Only Unregistered (410) means the device dropped the token. BadDeviceToken
// also fires when a caller names the wrong env, so it must not tombstone.
export function isUnregistered(result: ApnsResult): boolean {
  return result.status === 410 || result.reason === "Unregistered";
}
