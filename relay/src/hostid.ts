// A host's id is derived from its mailbox secret, so the relay can check a
// Bearer secret against the id in the URL without storing anything per host.

export const HOST_ID_RE = /^h_[a-z2-7]{26}$/;
export const SECRET_BYTES = 32;

const BASE32 = "abcdefghijklmnopqrstuvwxyz234567";

export async function hostIdFromSecret(secret: Uint8Array): Promise<string> {
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", secret));
  return "h_" + base32(digest).slice(0, 26);
}

function base32(bytes: Uint8Array): string {
  let out = "";
  let buffer = 0;
  let bits = 0;
  for (const b of bytes) {
    buffer = (buffer << 8) | b;
    bits += 8;
    while (bits >= 5) {
      out += BASE32[(buffer >>> (bits - 5)) & 31];
      bits -= 5;
    }
    buffer &= (1 << bits) - 1;
  }
  if (bits > 0) out += BASE32[(buffer << (5 - bits)) & 31];
  return out;
}

const B64URL_RE = /^[A-Za-z0-9_-]+$/;

// Returns null for anything that is not unpadded base64url of exactly 32 bytes.
export function decodeSecret(b64: string): Uint8Array | null {
  if (b64.length !== 43 || !B64URL_RE.test(b64)) return null;
  const bin = atob(b64.replace(/-/g, "+").replace(/_/g, "/") + "=");
  const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
  return bytes.length === SECRET_BYTES ? bytes : null;
}

// Both inputs are public-length ids; comparing every byte keeps the time
// independent of where they first differ.
export function constantTimeEqual(a: string, b: string): boolean {
  const ea = new TextEncoder().encode(a);
  const eb = new TextEncoder().encode(b);
  let diff = ea.length ^ eb.length;
  for (let i = 0; i < Math.max(ea.length, eb.length); i++) diff |= (ea[i] ?? 0) ^ (eb[i] ?? 0);
  return diff === 0;
}
