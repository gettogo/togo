import { env, runDurableObjectAlarm, runInDurableObject } from "cloudflare:test";
import { afterEach, describe, expect, it, vi } from "vitest";
import worker from "../src/index";
import { hostIdFromSecret } from "../src/hostid";
import { MESSAGE_TTL_MS, type Mailbox } from "../src/mailbox";
import {
  due,
  nextCheck,
  parseWill,
  WILL_MAX_AFTER_MS,
  WILL_MAX_PUSHES,
  WILL_MIN_AFTER_MS,
  WILL_SETS_PER_MINUTE,
  WILL_TTL_MS,
  type StoredWill,
} from "../src/will";
import { CIPHERTEXT, mockApns, randomToken } from "./helpers";

const AFTER = 300_000;
const MIN = 60_000;

interface Host {
  hostId: string;
  bearer: string;
}

async function newHost(): Promise<Host> {
  const secret = crypto.getRandomValues(new Uint8Array(32));
  const bearer = btoa(String.fromCharCode(...secret)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return { hostId: await hostIdFromSecret(secret), bearer };
}

function call(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("cf-connecting-ip", crypto.randomUUID());
  const req = new Request(`https://relay.test${path}`, { ...init, headers });
  return worker.fetch(req as Request<unknown, IncomingRequestCfProperties>, env);
}

class Client {
  closed: { code: number; reason: string } | null = null;

  constructor(readonly ws: WebSocket) {
    ws.accept();
    ws.addEventListener("close", (e) => {
      this.closed = { code: e.code, reason: e.reason };
    });
  }

  send(frame: unknown) {
    this.ws.send(typeof frame === "string" ? frame : JSON.stringify(frame));
  }

  async waitClosed(): Promise<{ code: number; reason: string }> {
    await vi.waitUntil(() => this.closed !== null, { timeout: 2000 });
    return this.closed!;
  }
}

async function open(host: Host): Promise<Client> {
  const res = await call(`/v1/mailbox/${host.hostId}/ws`, {
    headers: { upgrade: "websocket", authorization: `Bearer ${host.bearer}` },
  });
  expect(res.status).toBe(101);
  return new Client(res.webSocket!);
}

function stub(hostId: string) {
  return env.MAILBOX.get(env.MAILBOX.idFromName(hostId));
}

function inObject<T>(hostId: string, fn: (state: DurableObjectState) => Promise<T>): Promise<T> {
  return runInDurableObject(stub(hostId), (_o: Mailbox, state) => fn(state));
}

function storedWill(hostId: string): Promise<StoredWill | undefined> {
  return inObject(hostId, (state) => state.storage.get<StoredWill>("will"));
}

async function waitForWill(hostId: string): Promise<StoredWill> {
  await vi.waitUntil(async () => (await storedWill(hostId)) !== undefined, { timeout: 2000 });
  return (await storedWill(hostId))!;
}

// Moves every time the object remembers back by `ms`, as if that much time
// had passed since.
async function age(hostId: string, ms: number): Promise<void> {
  await inObject(hostId, async (state) => {
    for (const key of ["connectedAt", "disconnectedAt"]) {
      const t = await state.storage.get<number>(key);
      if (t !== undefined) await state.storage.put(key, t - ms);
    }
    const will = await state.storage.get<StoredWill>("will");
    if (will) await state.storage.put("will", { ...will, setAt: will.setAt - ms });
    for (const ws of state.getWebSockets()) {
      const a = ws.deserializeAttachment() as { seenAt?: number } | null;
      if (a?.seenAt !== undefined) ws.serializeAttachment({ ...a, seenAt: a.seenAt - ms });
    }
  });
}

function alertPush(token = randomToken()) {
  return { token, env: "sandbox", ciphertext: CIPHERTEXT, kind: "alert" };
}

function offlineActivity(token = randomToken()) {
  return {
    token,
    env: "sandbox",
    kind: "liveactivity",
    liveActivity: {
      event: "update",
      contentState: { bib: null, agent: "claude", status: "offline", waiting: 0, since: Math.floor(Date.now() / 1000) },
      staleDate: Math.floor(Date.now() / 1000) + 3600,
    },
  };
}

function willFrame(pushes: unknown[], afterMs = AFTER) {
  return { type: "will", v: 1, afterMs, pushes };
}

async function armed(pushes: unknown[] = [alertPush(), offlineActivity()]) {
  const host = await newHost();
  const client = await open(host);
  client.send(willFrame(pushes));
  await waitForWill(host.hostId);
  return { host, client };
}

async function closeAndWait(host: Host, client: Client) {
  client.ws.close(1000);
  await vi.waitUntil(async () => (await inObject(host.hostId, (s) => s.storage.get("disconnectedAt"))) !== undefined, {
    timeout: 2000,
  });
}

function tokensSent(calls: { url: string }[]): string[] {
  return calls.map((c) => c.url.split("/").pop()!);
}

afterEach(() => vi.restoreAllMocks());

describe("will frame", () => {
  it("parses a valid will and a disarm", () => {
    const parsed = parseWill(willFrame([alertPush()]));
    expect(parsed).not.toBeNull();
    expect(parsed).not.toBe("disarm");
    const will = parsed as StoredWill;
    expect(will.afterMs).toBe(AFTER);
    expect(will.pushes).toHaveLength(1);
    expect(Math.abs(will.setAt - Date.now())).toBeLessThan(5000);
    expect(parseWill({ type: "will", v: 1, pushes: [] })).toBe("disarm");
    expect(parseWill(willFrame([]))).toBe("disarm");
    expect(parseWill(willFrame([], 1000))).toBeNull();
  });

  it("refuses out-of-range and malformed frames", () => {
    const bad: unknown[] = [
      null,
      [],
      "will",
      { ...willFrame([alertPush()]), v: 2 },
      { ...willFrame([alertPush()]), type: "ack" },
      willFrame([alertPush()], WILL_MIN_AFTER_MS - 1),
      willFrame([alertPush()], WILL_MAX_AFTER_MS + 1),
      willFrame([alertPush()], 300_000.5),
      { type: "will", v: 1, pushes: [alertPush()] },
      willFrame(Array.from({ length: WILL_MAX_PUSHES + 1 }, () => alertPush())),
      willFrame([{ ...alertPush(), token: "nothex" }]),
      willFrame([{ ...alertPush(), ciphertext: "plain text!" }]),
      { ...willFrame([alertPush()]), extra: 1 },
      { type: "will", v: 1, afterMs: AFTER, pushes: "x" },
    ];
    for (const frame of bad) expect(parseWill(frame), JSON.stringify(frame)?.slice(0, 80)).toBeNull();
  });

  it("decides fire, wait or expired", () => {
    const will: StoredWill = { pushes: [], afterMs: AFTER, setAt: 1_000_000 };
    expect(due(will, 1_000_000, 1_000_000 + AFTER - 1)).toBe("wait");
    expect(due(will, 1_000_000, 1_000_000 + AFTER)).toBe("fire");
    expect(due(will, 1_000_000 + WILL_TTL_MS, 1_000_000 + WILL_TTL_MS)).toBe("expired");
    expect(nextCheck(will, 1_000_000 + 10)).toBe(1_000_000 + 10 + AFTER);
    expect(nextCheck(will, 1_000_000 + WILL_TTL_MS)).toBe(1_000_000 + WILL_TTL_MS);
  });
});

describe("mailbox will", () => {
  it("acceptsWillFromAuthenticatedSocket", async () => {
    const before = Date.now();
    const { host } = await armed();
    const will = (await storedWill(host.hostId))!;
    expect(will.afterMs).toBe(AFTER);
    expect(will.pushes).toHaveLength(2);
    expect(will.pushes[1].liveActivity?.contentState.status).toBe("offline");
    const alarm = await inObject(host.hostId, (s) => s.storage.getAlarm());
    expect(alarm).not.toBeNull();
    expect(alarm!).toBeLessThanOrEqual(Date.now() + AFTER);
    expect(alarm!).toBeGreaterThanOrEqual(before);
  });

  it("refusesOversizeOrMalformedWill", async () => {
    const big = { ...willFrame([alertPush()]), pad: "x".repeat(40 * 1024) };
    const cases: unknown[] = [
      willFrame(Array.from({ length: 9 }, () => alertPush())),
      willFrame([alertPush()], 1000),
      willFrame([{ ...alertPush(), token: "zz" + randomToken().slice(2) }]),
      big,
      // A large frame that is not a will is refused before it is parsed.
      JSON.stringify({ type: "ack", id: "x".repeat(2000) }),
      "x".repeat(2000),
    ];
    for (const frame of cases) {
      const host = await newHost();
      const client = await open(host);
      client.send(frame);
      expect((await client.waitClosed()).code).toBe(1003);
      expect(await storedWill(host.hostId)).toBeUndefined();
    }
  });

  it("keeps the previous will when a bad one arrives", async () => {
    const { host, client } = await armed();
    client.send(willFrame([alertPush()], 1000));
    await client.waitClosed();
    expect((await storedWill(host.hostId))?.pushes).toHaveLength(2);
  });

  it("alarmFiresAfterSilence", async () => {
    const { calls } = mockApns();
    const pushes = [alertPush(), alertPush(), offlineActivity()];
    const { host, client } = await armed(pushes);
    await closeAndWait(host, client);
    await age(host.hostId, 5 * MIN);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(tokensSent(calls).sort()).toEqual(pushes.map((p) => p.token).sort());
    const la = calls.find((c) => c.headers.get("apns-push-type") === "liveactivity")!;
    expect((la.body as { aps: { "content-state": { status: string } } }).aps["content-state"].status).toBe("offline");
    expect(await storedWill(host.hostId)).toBeUndefined();
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(3);
  });

  it("reconnectWithinGraceSendsNothing", async () => {
    const { calls } = mockApns();
    const { host, client } = await armed();
    await closeAndWait(host, client);
    await age(host.hostId, 4 * MIN);
    const again = await open(host);
    await age(host.hostId, 2 * MIN);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(0);
    expect(again.closed).toBeNull();
    expect(await storedWill(host.hostId)).toBeDefined();
    const alarm = await inObject(host.hostId, (s) => s.storage.getAlarm());
    expect(alarm).not.toBeNull();
    expect(alarm!).toBeGreaterThan(Date.now() + 2 * MIN);
  });

  it("halfOpenSocketFires", async () => {
    const { calls } = mockApns();
    const { host, client } = await armed();
    client.send('{"type":"ping"}');
    await age(host.hostId, 6 * MIN);
    await inObject(host.hostId, async (state) => {
      vi.spyOn(state, "getWebSocketAutoResponseTimestamp").mockReturnValue(new Date(Date.now() - 6 * MIN));
    });
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(2);
    expect(await client.waitClosed()).toEqual({ code: 4001, reason: "silent" });
    expect(await storedWill(host.hostId)).toBeUndefined();
  });

  it("recent auto-response keeps an open socket alive", async () => {
    const { calls } = mockApns();
    const { host } = await armed();
    await age(host.hostId, 6 * MIN);
    await inObject(host.hostId, async (state) => {
      vi.spyOn(state, "getWebSocketAutoResponseTimestamp").mockReturnValue(new Date(Date.now() - 1000));
    });
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(0);
    expect(await storedWill(host.hostId)).toBeDefined();
  });

  // A deploy or reset drops the socket without a close callback.
  async function dropSilently(hostId: string) {
    return inObject(hostId, async (state) => vi.spyOn(state, "getWebSockets").mockReturnValue([]));
  }

  async function storedTimes(hostId: string) {
    return inObject(hostId, async (s) => ({
      gone: await s.storage.get<number>("disconnectedAt"),
      alarm: await s.storage.getAlarm(),
    }));
  }

  it("deployDropDoesNotFireImmediately", async () => {
    const { calls } = mockApns();
    const { host } = await armed();
    await age(host.hostId, 3 * 60 * MIN);
    await dropSilently(host.hostId);
    const before = Date.now();
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(0);
    expect(await storedWill(host.hostId)).toBeDefined();
    const t = await storedTimes(host.hostId);
    expect(t.gone!).toBeGreaterThanOrEqual(before);
    expect(t.alarm!).toBeGreaterThanOrEqual(before + AFTER);
  });

  it("firesAfterGraceFromObservation", async () => {
    const { calls } = mockApns();
    const { host } = await armed();
    await age(host.hostId, 3 * 60 * MIN);
    await dropSilently(host.hostId);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(0);
    await age(host.hostId, 4 * MIN);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(0);
    await age(host.hostId, 1 * MIN);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(2);
    expect(await storedWill(host.hostId)).toBeUndefined();
  });

  it("reconnectAfterDeployCancels", async () => {
    const { calls } = mockApns();
    const { host } = await armed();
    await age(host.hostId, 3 * 60 * MIN);
    const drop = await dropSilently(host.hostId);
    await runDurableObjectAlarm(stub(host.hostId));
    await age(host.hostId, 4 * MIN);
    drop.mockRestore();
    const again = await open(host);
    await age(host.hostId, 2 * MIN);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(0);
    expect(again.closed).toBeNull();
    expect(await storedWill(host.hostId)).toBeDefined();
  });

  it("newSocketThenOldCloseDoesNotFire", async () => {
    const { calls } = mockApns();
    const { host, client } = await armed();
    const newer = await open(host);
    expect((await client.waitClosed()).code).toBe(4000);
    // Let the old socket's close reach the object.
    await new Promise((r) => setTimeout(r, 200));
    expect((await storedTimes(host.hostId)).gone).toBeUndefined();
    expect(newer.closed).toBeNull();
    // Hours later the newer socket is dropped without a close.
    await age(host.hostId, 3 * 60 * MIN);
    await dropSilently(host.hostId);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(0);
    await age(host.hostId, 4 * MIN);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(0);
    await age(host.hostId, 1 * MIN);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(2);
  });

  it("staleWillIsDropped", async () => {
    const { calls } = mockApns();
    const { host, client } = await armed();
    await closeAndWait(host, client);
    await age(host.hostId, 25 * 60 * MIN);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(0);
    expect(await storedWill(host.hostId)).toBeUndefined();
  });

  it("disarmDeletes", async () => {
    const { calls } = mockApns();
    const { host, client } = await armed();
    client.send({ type: "will", v: 1, pushes: [] });
    await vi.waitUntil(async () => (await storedWill(host.hostId)) === undefined, { timeout: 2000 });
    client.send(willFrame([alertPush()]));
    await waitForWill(host.hostId);
    client.send(willFrame([]));
    await vi.waitUntil(async () => (await storedWill(host.hostId)) === undefined, { timeout: 2000 });
    await closeAndWait(host, client);
    await age(host.hostId, 10 * MIN);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(calls).toHaveLength(0);
  });

  it("sweepStillExpiresAnswers", async () => {
    const { host } = await armed();
    const res = await call(`/v1/mailbox/${host.hostId}`, { method: "POST", body: JSON.stringify({ ciphertext: "AQold" }) });
    expect(res.status).toBe(202);
    await inObject(host.hostId, async (state) => {
      for (const [key, msg] of await state.storage.list<{ receivedAt: number }>({ prefix: "msg:" })) {
        await state.storage.put(key, { ...msg, receivedAt: Date.now() - MESSAGE_TTL_MS - 1 });
      }
    });
    await runDurableObjectAlarm(stub(host.hostId));
    const left = await inObject(host.hostId, async (s) => ({
      msgs: (await s.storage.list({ prefix: "msg:" })).size,
      alarm: await s.storage.getAlarm(),
    }));
    expect(left.msgs).toBe(0);
    expect(await storedWill(host.hostId)).toBeDefined();
    expect(left.alarm).not.toBeNull();
  });

  it("limits how often one mailbox can set its will", async () => {
    const host = await newHost();
    const client = await open(host);
    for (let i = 0; i < WILL_SETS_PER_MINUTE; i++) client.send(willFrame([alertPush()]));
    await waitForWill(host.hostId);
    client.send(willFrame([alertPush()]));
    expect((await client.waitClosed()).code).toBe(1008);
  });

  it("willNeverLogged", async () => {
    const lines: string[] = [];
    for (const m of ["log", "info", "warn", "error", "debug"] as const) {
      vi.spyOn(console, m).mockImplementation((...a: unknown[]) => void lines.push(a.map(String).join(" ")));
    }
    let n = 0;
    mockApns(() =>
      n++ === 0
        ? Response.json({ reason: "Unregistered" }, { status: 410 })
        : Response.json({ reason: "InternalServerError" }, { status: 500 }),
    );
    const pushes = [alertPush(), alertPush(), offlineActivity()];
    const { host, client } = await armed(pushes);
    await closeAndWait(host, client);
    await age(host.hostId, 5 * MIN);
    await runDurableObjectAlarm(stub(host.hostId));
    expect(lines.length).toBeGreaterThan(0);
    const out = lines.join("\n");
    for (const p of pushes) expect(out).not.toContain(p.token.slice(0, 16));
    expect(out).not.toContain(CIPHERTEXT.slice(0, 16));
    expect(out).not.toContain(host.hostId);
    expect(out).not.toContain(host.bearer);
  });
});
