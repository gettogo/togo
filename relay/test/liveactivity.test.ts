import { afterEach, describe, expect, it, vi } from "vitest";
import { mockApns, push, randomToken } from "./helpers";

afterEach(() => vi.restoreAllMocks());

const HOST = "h_" + "a".repeat(26);
const state = { bib: "a1", agent: "claude", status: "needs_you", waiting: 1, since: 1762300000 };

function live(liveActivity: Record<string, unknown>, extra: Record<string, unknown> = {}) {
  return { token: randomToken(), env: "sandbox", kind: "liveactivity", liveActivity, ...extra };
}

async function expectField(body: unknown, field: string) {
  const res = await push(body);
  expect(res.status, field).toBe(400);
  expect(await res.json()).toEqual({ error: "bad_request", field });
}

describe("watch target", () => {
  const alert = (extra: Record<string, unknown> = {}) => ({
    token: randomToken(),
    env: "sandbox",
    kind: "alert",
    ciphertext: "AQ" + "x".repeat(100),
    ...extra,
  });

  it("watch target uses watchkitapp topic", async () => {
    const { calls } = mockApns();
    expect((await push(alert({ target: "watch" }))).status).toBe(202);
    expect(calls[0].headers.get("apns-topic")).toBe("app.togo.Togo.watchkitapp");
  });

  it("app target and no target use the app topic", async () => {
    const { calls } = mockApns();
    await push(alert({ target: "app" }));
    await push(alert());
    expect(calls.map((c) => c.headers.get("apns-topic"))).toEqual(["app.togo.Togo", "app.togo.Togo"]);
  });

  it("unknown target is 400 target", async () => {
    await expectField(alert({ target: "tv" }), "target");
  });

  it("liveactivity with watch target is 400 target", async () => {
    mockApns();
    await expectField(live({ event: "update", contentState: state }, { target: "watch" }), "target");
  });
});

describe("live activity start", () => {
  it("start requires attributes with hostId", async () => {
    mockApns();
    await expectField(live({ event: "start", contentState: state }), "liveActivity.attributes");
    await expectField(live({ event: "start", contentState: state, attributes: { hostId: "h_short" } }), "liveActivity.attributes");
    await expectField(live({ event: "start", contentState: state, attributes: {} }), "liveActivity.attributes");
  });

  it("start payload carries attributes-type, attributes and alert", async () => {
    const { calls } = mockApns();
    const res = await push(live({ event: "start", contentState: state, attributes: { hostId: HOST } }));
    expect(res.status).toBe(202);
    const aps = (calls[0].body as { aps: Record<string, unknown> }).aps;
    expect(aps.event).toBe("start");
    expect(aps["attributes-type"]).toBe("TogoActivityAttributes");
    expect(aps.attributes).toEqual({ hostId: HOST });
    expect(aps.alert).toEqual({ title: "Togo", body: "Needs you" });
    expect(aps["content-state"]).toEqual(state);
    expect(typeof aps.timestamp).toBe("number");
  });

  it("update carries no attributes or alert", async () => {
    const { calls } = mockApns();
    await push(live({ event: "update", contentState: state }));
    const aps = (calls[0].body as { aps: Record<string, unknown> }).aps;
    expect(aps.alert).toBeUndefined();
    expect(aps["attributes-type"]).toBeUndefined();
  });
});

describe("end, stale and content state", () => {
  const now = () => Math.floor(Date.now() / 1000);
  const soon = now() + 300;

  it("end with dismissalDate sets dismissal-date", async () => {
    const { calls } = mockApns();
    await push(live({ event: "end", contentState: { ...state, status: "done" }, dismissalDate: soon }));
    expect((calls[0].body as { aps: Record<string, unknown> }).aps["dismissal-date"]).toBe(soon);
  });

  it("staleDate sets stale-date", async () => {
    const { calls } = mockApns();
    await push(live({ event: "update", contentState: state, staleDate: soon }));
    expect((calls[0].body as { aps: Record<string, unknown> }).aps["stale-date"]).toBe(soon);
  });

  it("requestId and command pass through, null allowed", async () => {
    const { calls } = mockApns();
    const full = { ...state, requestId: "r_" + "b".repeat(10), command: "npm test" };
    await push(live({ event: "update", contentState: full }));
    await push(live({ event: "update", contentState: { ...state, requestId: null, command: null } }));
    const states = calls.map((c) => (c.body as { aps: { "content-state": unknown } }).aps["content-state"]);
    expect(states).toEqual([full, { ...state, requestId: null, command: null }]);
  });

  it("contentState command over 160 chars is 400 liveActivity.contentState.command", async () => {
    mockApns();
    await expectField(
      live({ event: "update", contentState: { ...state, command: "x".repeat(161) } }),
      "liveActivity.contentState.command",
    );
    await expectField(
      live({ event: "update", contentState: { ...state, command: "" } }),
      "liveActivity.contentState.command",
    );
  });

  it("status outside the three words is 400", async () => {
    mockApns();
    await expectField(
      live({ event: "update", contentState: { ...state, status: "idle" } }),
      "liveActivity.contentState.status",
    );
  });

  it("requestId must match id pattern", async () => {
    mockApns();
    for (const requestId of ["r_short", "x_" + "b".repeat(10), "r_" + "B".repeat(10), 7]) {
      await expectField(
        live({ event: "update", contentState: { ...state, requestId } }),
        "liveActivity.contentState.requestId",
      );
    }
  });

  it("staleDate and dismissalDate outside now-60s to now+8h are 400", async () => {
    mockApns();
    const bad = [now() - 3600, now() + 8 * 3600 + 120];
    for (const staleDate of bad) {
      await expectField(live({ event: "update", contentState: state, staleDate }), "liveActivity.staleDate");
    }
    for (const dismissalDate of bad) {
      await expectField(
        live({ event: "end", contentState: state, dismissalDate }),
        "liveActivity.dismissalDate",
      );
    }
  });

  it("dates at the window edges are accepted", async () => {
    mockApns();
    const res1 = await push(live({ event: "update", contentState: state, staleDate: now() - 30 }));
    const res2 = await push(live({ event: "end", contentState: state, dismissalDate: now() + 8 * 3600 - 30 }));
    expect([res1.status, res2.status]).toEqual([202, 202]);
  });

  it("dismissalDate is only valid on end", async () => {
    mockApns();
    await expectField(
      live({ event: "update", contentState: state, dismissalDate: 5 }),
      "liveActivity.dismissalDate",
    );
  });
});
