// APNs provider tokens: ES256 JWTs signed with the team's .p8 key.
// Apple rejects tokens older than 60 minutes and throttles callers that mint
// a new one more often than every 20, so one token is reused for 50 minutes.

// Secrets never appear in wrangler.jsonc, so `wrangler types` cannot see them;
// declaring them on the generated base reaches both Env and Cloudflare.Env.
declare global {
  interface __BaseEnv_Env {
    APNS_KEY: string;
    APNS_KEY_ID: string;
    APNS_TEAM_ID: string;
  }
}

export const JWT_TTL_MS = 50 * 60 * 1000;

const KEY_ID_RE = /^[A-Z0-9]{10}$/;

interface CachedJwt {
  keyId: string;
  teamId: string;
  issuedAtMs: number;
  token: string;
}

let cached: CachedJwt | null = null;
let cachedKey: { pem: string; key: CryptoKey } | null = null;

export class ConfigError extends Error {}

export async function apnsJwt(env: Env, nowMs: number = Date.now()): Promise<string> {
  const keyId = env.APNS_KEY_ID ?? "";
  const teamId = env.APNS_TEAM_ID ?? "";
  if (!KEY_ID_RE.test(keyId)) throw new ConfigError("APNS_KEY_ID");
  if (!KEY_ID_RE.test(teamId)) throw new ConfigError("APNS_TEAM_ID");
  if (
    cached &&
    cached.keyId === keyId &&
    cached.teamId === teamId &&
    nowMs >= cached.issuedAtMs &&
    nowMs - cached.issuedAtMs < JWT_TTL_MS
  ) {
    return cached.token;
  }
  const key = await signingKey(env.APNS_KEY);
  const header = b64urlJson({ alg: "ES256", kid: keyId });
  const claims = b64urlJson({ iss: teamId, iat: Math.floor(nowMs / 1000) });
  const signingInput = `${header}.${claims}`;
  // WebCrypto emits the raw r||s form JWS expects, not DER.
  const sig = await crypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    key,
    new TextEncoder().encode(signingInput),
  );
  const token = `${signingInput}.${b64url(new Uint8Array(sig))}`;
  cached = { keyId, teamId, issuedAtMs: nowMs, token };
  return token;
}

export function dropCachedJwt(): void {
  cached = null;
}

async function signingKey(pem: string | undefined): Promise<CryptoKey> {
  if (!pem) throw new ConfigError("APNS_KEY");
  if (cachedKey && cachedKey.pem === pem) return cachedKey.key;
  const body = pem
    .replace(/-----BEGIN PRIVATE KEY-----/, "")
    .replace(/-----END PRIVATE KEY-----/, "")
    .replace(/\s+/g, "");
  let der: Uint8Array;
  try {
    der = Uint8Array.from(atob(body), (c) => c.charCodeAt(0));
  } catch {
    throw new ConfigError("APNS_KEY");
  }
  let key: CryptoKey;
  try {
    key = await crypto.subtle.importKey(
      "pkcs8",
      der,
      { name: "ECDSA", namedCurve: "P-256" },
      false,
      ["sign"],
    );
  } catch {
    // The import error can quote key material; never surface it.
    throw new ConfigError("APNS_KEY");
  }
  cachedKey = { pem, key };
  return key;
}

function b64urlJson(value: unknown): string {
  return b64url(new TextEncoder().encode(JSON.stringify(value)));
}

export function b64url(bytes: Uint8Array): string {
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
