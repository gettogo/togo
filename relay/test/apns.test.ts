import { env } from "cloudflare:test";
import { afterEach, describe, expect, it, vi } from "vitest";
import { apnsSend } from "../src/apns";
import { apnsJwt, ConfigError, JWT_TTL_MS } from "../src/jwt";
import { mockApns, randomToken } from "./helpers";

afterEach(() => vi.restoreAllMocks());

function decodePart(part: string): Record<string, unknown> {
  const b64 = part.replace(/-/g, "+").replace(/_/g, "/");
  return JSON.parse(atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4)));
}

function b64urlBytes(part: string): Uint8Array {
  const b64 = part.replace(/-/g, "+").replace(/_/g, "/");
  return Uint8Array.from(atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4)), (c) => c.charCodeAt(0));
}

describe("apns jwt", () => {
  it("signs an ES256 token the key's public half verifies", async () => {
    const now = Date.UTC(2026, 10, 4, 12, 0, 0);
    const jwt = await apnsJwt(env, now);
    const [h, c, s] = jwt.split(".");
    expect(decodePart(h)).toEqual({ alg: "ES256", kid: "TESTKEY123" });
    expect(decodePart(c)).toEqual({ iss: "TESTTEAM12", iat: now / 1000 });
    const jwk = JSON.parse((env as unknown as Record<string, string>).TEST_APNS_PUBLIC_JWK);
    const pub = await crypto.subtle.importKey("jwk", jwk, { name: "ECDSA", namedCurve: "P-256" }, false, [
      "verify",
    ]);
    const ok = await crypto.subtle.verify(
      { name: "ECDSA", hash: "SHA-256" },
      pub,
      b64urlBytes(s),
      new TextEncoder().encode(`${h}.${c}`),
    );
    expect(ok).toBe(true);
  });

  it("jwt is reused within 50 minutes and refreshed after", async () => {
    const t0 = Date.UTC(2026, 10, 5, 9, 0, 0);
    const first = await apnsJwt(env, t0);
    expect(await apnsJwt(env, t0 + JWT_TTL_MS - 1000)).toBe(first);
    const refreshed = await apnsJwt(env, t0 + JWT_TTL_MS);
    expect(refreshed).not.toBe(first);
    expect(decodePart(refreshed.split(".")[1]).iat).toBe((t0 + JWT_TTL_MS) / 1000);
    expect(await apnsJwt(env, t0 + JWT_TTL_MS + 60_000)).toBe(refreshed);
  });

  it("rejects missing or malformed key configuration", async () => {
    await expect(apnsJwt({ ...env, APNS_KEY_ID: "short" })).rejects.toBeInstanceOf(ConfigError);
    await expect(apnsJwt({ ...env, APNS_TEAM_ID: "" })).rejects.toBeInstanceOf(ConfigError);
    await expect(apnsJwt({ ...env, APNS_KEY_ID: "OTHERKEY12", APNS_KEY: "garbage" })).rejects.toBeInstanceOf(
      ConfigError,
    );
  });
});

describe("apnsSend", () => {
  it("returns the APNs id and reason from the response", async () => {
    mockApns(() =>
      Response.json({ reason: "PayloadTooLarge" }, { status: 413, headers: { "apns-id": "abc" } }),
    );
    const result = await apnsSend(env, {
      token: randomToken(),
      environment: "sandbox",
      pushType: "alert",
      topic: env.APNS_TOPIC,
      payload: { aps: {} },
    });
    expect(result).toEqual({ status: 413, apnsId: "abc", reason: "PayloadTooLarge" });
  });

  it("drops the cached provider token when APNs reports it expired", async () => {
    const send = () =>
      apnsSend(env, { token: randomToken(), environment: "sandbox", pushType: "alert", topic: env.APNS_TOPIC, payload: {} });
    mockApns();
    await send();
    const before = await apnsJwt(env);
    expect(await apnsJwt(env)).toBe(before);
    vi.restoreAllMocks();
    mockApns(() => Response.json({ reason: "ExpiredProviderToken" }, { status: 403 }));
    await send();
    expect(await apnsJwt(env)).not.toBe(before);
  });

  it("never echoes a non-identifier reason", async () => {
    mockApns(() => new Response("<html>proxy</html>", { status: 503 }));
    const result = await apnsSend(env, {
      token: randomToken(),
      environment: "production",
      pushType: "alert",
      topic: env.APNS_TOPIC,
      payload: {},
    });
    expect(result.reason).toBe("status_503");
  });
});
