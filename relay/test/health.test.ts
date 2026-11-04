import { SELF } from "cloudflare:test";
import { describe, expect, it } from "vitest";

describe("router", () => {
  it("health returns 200 ok", async () => {
    const res = await SELF.fetch("https://relay.test/v1/health");
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toContain("text/plain");
    expect(await res.text()).toBe("ok");
  });

  it("unknown route returns 404 json", async () => {
    const res = await SELF.fetch("https://relay.test/v1/nope");
    expect(res.status).toBe(404);
    expect(res.headers.get("content-type")).toContain("application/json");
    expect(await res.json()).toEqual({ error: "not_found" });
  });

  it("wrong method returns 405", async () => {
    const res = await SELF.fetch("https://relay.test/v1/health", { method: "POST" });
    expect(res.status).toBe(405);
    expect(res.headers.get("allow")).toBe("GET");
  });
});
