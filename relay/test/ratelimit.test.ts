import { env, runDurableObjectAlarm, runInDurableObject } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import { RateLimiter, UNREGISTERED_TTL_MS } from "../src/ratelimit";

function stub(name: string) {
  return env.RATE_LIMITER.get(env.RATE_LIMITER.idFromName(name));
}

describe("RateLimiter", () => {
  it("admits up to the limit then reports seconds left in the window", async () => {
    const rl = stub(`test:${crypto.randomUUID()}`);
    for (let i = 0; i < 3; i++) expect(await rl.admit(3, 60_000)).toEqual({ status: "ok" });
    const res = await rl.admit(3, 60_000);
    expect(res.status).toBe("limited");
    if (res.status === "limited") {
      expect(res.retryAfter).toBeGreaterThanOrEqual(1);
      expect(res.retryAfter).toBeLessThanOrEqual(60);
    }
  });

  it("opens a fresh window once the old one has elapsed", async () => {
    const rl = stub(`test:${crypto.randomUUID()}`);
    await rl.admit(1, 60_000);
    await runInDurableObject(rl, async (_obj: RateLimiter, state) => {
      await state.storage.put("window", { start: Date.now() - 60_001, count: 1 });
    });
    expect(await rl.admit(1, 60_000)).toEqual({ status: "ok" });
  });

  it("answers unregistered for 24 hours after a mark, then forgets it", async () => {
    const rl = stub(`test:${crypto.randomUUID()}`);
    await rl.markUnregistered();
    expect(await rl.admit(10, 60_000)).toEqual({ status: "unregistered" });
    await runInDurableObject(rl, async (_obj: RateLimiter, state) => {
      const until = await state.storage.get<number>("unregisteredUntil");
      expect(until! - Date.now()).toBeGreaterThan(UNREGISTERED_TTL_MS - 5_000);
      await state.storage.put("unregisteredUntil", Date.now() - 1);
    });
    expect(await rl.admit(10, 60_000)).toEqual({ status: "ok" });
  });

  // One object exists per token and per address; Live Activity tokens are new
  // for every activity, so a key's state must not outlive its use.
  it("clears its storage once the window has passed", async () => {
    const rl = stub(`test:${crypto.randomUUID()}`);
    await rl.admit(3, 60_000);
    await runInDurableObject(rl, async (_obj: RateLimiter, state) => {
      expect(await state.storage.getAlarm()).not.toBeNull();
      const win = await state.storage.get<{ start: number }>("window");
      await state.storage.put("window", { ...win, start: Date.now() - 60_001, end: Date.now() - 1 });
    });
    expect(await runDurableObjectAlarm(rl)).toBe(true);
    await runInDurableObject(rl, async (_obj: RateLimiter, state) => {
      expect((await state.storage.list()).size).toBe(0);
    });
  });

  it("keeps an unregistered mark until it lapses, then clears it", async () => {
    const rl = stub(`test:${crypto.randomUUID()}`);
    await rl.admit(3, 60_000);
    await rl.markUnregistered();
    await runInDurableObject(rl, async (_obj: RateLimiter, state) => {
      await state.storage.put("window", { start: Date.now() - 60_001, count: 1, end: Date.now() - 1 });
      await state.storage.setAlarm(Date.now());
    });
    expect(await runDurableObjectAlarm(rl)).toBe(true);
    expect(await rl.admit(10, 60_000)).toEqual({ status: "unregistered" });
    await runInDurableObject(rl, async (_obj: RateLimiter, state) => {
      const until = await state.storage.get<number>("unregisteredUntil");
      expect(await state.storage.getAlarm()).toBe(until);
      await state.storage.put("unregisteredUntil", Date.now() - 1);
      await state.storage.setAlarm(Date.now());
    });
    expect(await runDurableObjectAlarm(rl)).toBe(true);
    await runInDurableObject(rl, async (_obj: RateLimiter, state) => {
      expect((await state.storage.list()).size).toBe(0);
    });
  });
});
