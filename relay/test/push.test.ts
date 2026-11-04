import { env, runInDurableObject } from "cloudflare:test";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IP_LIMIT } from "../src/push";
import type { RateLimiter } from "../src/ratelimit";
import { CIPHERTEXT, mockApns, push, randomToken } from "./helpers";

afterEach(() => vi.restoreAllMocks());

function alert(token: string, extra: Record<string, unknown> = {}) {
  return { token, env: "sandbox", ciphertext: CIPHERTEXT, kind: "alert", ...extra };
}

describe("POST /v1/push", () => {
  it("valid alert push returns 202 and sends mutable-content payload", async () => {
    const { calls } = mockApns();
    const token = randomToken();
    const res = await push(alert(token, { collapseId: "s1" }));
    expect(res.status).toBe(202);
    const { apnsId } = (await res.json()) as { apnsId: string };
    expect(apnsId).toMatch(/^[0-9a-f-]{36}$/);
    expect(calls).toHaveLength(1);
    const call = calls[0];
    expect(call.url).toBe(`https://api.sandbox.push.apple.com/3/device/${token}`);
    expect(call.headers.get("apns-topic")).toBe("app.togo.Togo");
    expect(call.headers.get("apns-push-type")).toBe("alert");
    expect(call.headers.get("apns-priority")).toBe("10");
    expect(call.headers.get("apns-collapse-id")).toBe("s1");
    expect(call.headers.get("authorization")).toMatch(/^bearer [\w-]+\.[\w-]+\.[\w-]+$/);
    expect(call.body).toEqual({
      aps: { alert: { title: "Togo", body: "Needs you" }, "mutable-content": 1, sound: "default" },
      c: CIPHERTEXT,
    });
  });

  it("production env targets the production host", async () => {
    const { calls } = mockApns();
    const res = await push(alert(randomToken(), { env: "production" }));
    expect(res.status).toBe(202);
    expect(new URL(calls[0].url).host).toBe("api.push.apple.com");
  });

  it("background push uses content-available and priority 5", async () => {
    const { calls } = mockApns();
    const res = await push(alert(randomToken(), { kind: "background" }));
    expect(res.status).toBe(202);
    expect(calls[0].headers.get("apns-push-type")).toBe("background");
    expect(calls[0].headers.get("apns-priority")).toBe("5");
    expect(calls[0].body).toEqual({ aps: { "content-available": 1 }, c: CIPHERTEXT });
  });

  it("liveactivity push uses topic suffix .push-type.liveactivity and apns-push-type liveactivity", async () => {
    const { calls } = mockApns();
    const contentState = { bib: "a1", agent: "claude", status: "needs_you", waiting: 2, since: 1762300000 };
    const { ciphertext: _omitted, ...withoutCiphertext } = alert(randomToken(), {
      kind: "liveactivity",
      liveActivity: { event: "update", contentState },
    });
    const res = await push(withoutCiphertext);
    expect(res.status).toBe(202);
    const call = calls[0];
    expect(call.headers.get("apns-topic")).toBe("app.togo.Togo.push-type.liveactivity");
    expect(call.headers.get("apns-push-type")).toBe("liveactivity");
    const aps = (call.body as { aps: Record<string, unknown>; c?: string }).aps;
    expect(aps.event).toBe("update");
    expect(aps["content-state"]).toEqual(contentState);
    expect(typeof aps.timestamp).toBe("number");
    expect((call.body as { c?: string }).c).toBeUndefined();
  });

  it("rejects non-hex token / bad env / oversized ciphertext with field name", async () => {
    const { calls } = mockApns();
    const token = randomToken();
    const cases: [unknown, string][] = [
      [alert("zz" + token.slice(2)), "token"],
      [alert(token.slice(0, 62)), "token"],
      [alert(token, { env: "staging" }), "env"],
      [alert(token, { ciphertext: "A".repeat(3073) }), "ciphertext"],
      [alert(token, { ciphertext: "not base64!" }), "ciphertext"],
      [alert(token, { kind: "voip" }), "kind"],
      [alert(token, { collapseId: "a\r\nb" }), "collapseId"],
      [alert(token, { kind: "liveactivity" }), "liveActivity"],
      [
        alert(token, {
          kind: "liveactivity",
          liveActivity: { event: "start", contentState: { bib: "a", agent: "c", status: "s", waiting: 0, since: 0 } },
        }),
        "liveActivity.event",
      ],
      [
        alert(token, {
          kind: "liveactivity",
          ciphertext: "bad!",
          liveActivity: { event: "end", contentState: { bib: "a", agent: "c", status: "s", waiting: 0, since: 0 } },
        }),
        "ciphertext",
      ],
      [alert(token, { liveActivity: { event: "update", contentState: {} } }), "liveActivity"],
      [
        alert(token, {
          kind: "liveactivity",
          liveActivity: { event: "update", contentState: { bib: "a", agent: "c", status: "s", waiting: -1, since: 0 } },
        }),
        "liveActivity.contentState.waiting",
      ],
      ["{not json", "body"],
      [JSON.stringify(alert(token, { pad: "x".repeat(9000) })), "body"],
    ];
    for (const [body, field] of cases) {
      const res = await push(body);
      expect(res.status, field).toBe(400);
      expect(await res.json()).toEqual({ error: "bad_request", field });
    }
    expect(calls).toHaveLength(0);
  });

  it("31st push in a minute for one token returns 429 with retryAfter", async () => {
    const { calls } = mockApns();
    const token = randomToken();
    for (let i = 0; i < 30; i++) {
      expect((await push(alert(token))).status).toBe(202);
    }
    const res = await push(alert(token));
    expect(res.status).toBe(429);
    const body = (await res.json()) as { error: string; retryAfter: number };
    expect(body.error).toBe("rate_limited");
    expect(body.retryAfter).toBeGreaterThanOrEqual(1);
    expect(body.retryAfter).toBeLessThanOrEqual(60);
    expect(res.headers.get("retry-after")).toBe(String(body.retryAfter));
    expect(calls).toHaveLength(30);
    expect((await push(alert(randomToken()))).status).toBe(202);
  });

  it("caps pushes per source address", async () => {
    const { calls } = mockApns();
    const ip = "198.51.100.7";
    const rl = env.RATE_LIMITER.get(env.RATE_LIMITER.idFromName(`ip:${ip}`));
    // Seed the window at the cap instead of sending 300 real requests.
    await runInDurableObject(rl, async (_obj: RateLimiter, state) => {
      await state.storage.put("window", { start: Date.now(), count: IP_LIMIT - 1 });
    });
    expect((await push(alert(randomToken()), ip)).status).toBe(202);
    const res = await push(alert(randomToken()), ip);
    expect(res.status).toBe(429);
    expect(calls).toHaveLength(1);
  });

  it("unregistered token returns 410 and second call skips APNs", async () => {
    const { calls } = mockApns(() => Response.json({ reason: "Unregistered" }, { status: 410 }));
    const token = randomToken();
    const first = await push(alert(token));
    expect(first.status).toBe(410);
    expect(await first.json()).toEqual({ error: "unregistered" });
    const second = await push(alert(token));
    expect(second.status).toBe(410);
    expect(calls).toHaveLength(1);
  });

  it("BadDeviceToken returns an error without tombstoning the token", async () => {
    const { calls } = mockApns(() => Response.json({ reason: "BadDeviceToken" }, { status: 400 }));
    const token = randomToken();
    for (let i = 0; i < 2; i++) {
      const res = await push(alert(token));
      expect(res.status).toBe(502);
      expect(await res.json()).toEqual({ error: "apns", reason: "BadDeviceToken" });
    }
    expect(calls).toHaveLength(2);
  });

  it("unregistered in one env does not block the same token in the other", async () => {
    let n = 0;
    const { calls } = mockApns(() =>
      n++ === 0 ? Response.json({ reason: "Unregistered" }, { status: 410 }) : new Response(null, { status: 200 }),
    );
    const token = randomToken();
    expect((await push(alert(token, { env: "sandbox" }))).status).toBe(410);
    expect((await push(alert(token, { env: "production" }))).status).toBe(202);
    expect((await push(alert(token, { env: "sandbox" }))).status).toBe(410);
    expect(calls).toHaveLength(2);
  });

  it("rejects a request without CF-Connecting-IP unless the test switch is set", async () => {
    const { calls } = mockApns();
    const res = await push(alert(randomToken()), null, { ALLOW_MISSING_CLIENT_IP: undefined });
    expect(res.status).toBe(400);
    expect(await res.json()).toEqual({ error: "bad_request", field: "cf-connecting-ip" });
    expect(calls).toHaveLength(0);
    expect((await push(alert(randomToken()), null)).status).toBe(202);
  });

  it("other APNs errors return 502 with the reason", async () => {
    mockApns(() => Response.json({ reason: "TopicDisallowed" }, { status: 400 }));
    const res = await push(alert(randomToken()));
    expect(res.status).toBe(502);
    expect(await res.json()).toEqual({ error: "apns", reason: "TopicDisallowed" });
  });

  it("console output contains no token or ciphertext", async () => {
    const lines: string[] = [];
    for (const level of ["log", "info", "warn", "error", "debug"] as const) {
      vi.spyOn(console, level).mockImplementation((...args: unknown[]) => {
        lines.push(args.map(String).join(" "));
      });
    }
    let n = 0;
    mockApns(() => {
      n++;
      return n === 1
        ? Response.json({ reason: "Unregistered" }, { status: 410 })
        : Response.json({ reason: "InternalServerError" }, { status: 500 });
    });
    const tokens = [randomToken(), randomToken()];
    for (const t of tokens) await push(alert(t));
    await push(alert(tokens[0]));
    expect(lines.length).toBeGreaterThan(0);
    const out = lines.join("\n");
    for (const t of tokens) expect(out).not.toContain(t.slice(0, 16));
    expect(out).not.toContain(CIPHERTEXT.slice(0, 16));
    expect(out).not.toMatch(/bearer|eyJ/i);
  });
});
