import { env, runDurableObjectAlarm, runInDurableObject } from "cloudflare:test";
import { afterEach, describe, expect, it, vi } from "vitest";
import worker from "../src/index";
import { MAX_PENDING, MESSAGE_TTL_MS, POST_IP_LIMIT, REPLACED_CODE, type Mailbox } from "../src/mailbox";
import { hostIdFromSecret } from "../src/hostid";

interface Host {
  hostId: string;
  bearer: string;
}

async function newHost(): Promise<Host> {
  const secret = crypto.getRandomValues(new Uint8Array(32));
  const bearer = btoa(String.fromCharCode(...secret)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return { hostId: await hostIdFromSecret(secret), bearer };
}

function call(path: string, init: RequestInit = {}, ip: string = crypto.randomUUID()): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("cf-connecting-ip", ip);
  const req = new Request(`https://relay.test${path}`, { ...init, headers });
  return worker.fetch(req as Request<unknown, IncomingRequestCfProperties>, env);
}

function post(hostId: string, ciphertext = "AQ" + crypto.randomUUID().replace(/-/g, ""), ip?: string) {
  return call(`/v1/mailbox/${hostId}`, { method: "POST", body: JSON.stringify({ ciphertext }) }, ip);
}

interface Frame {
  type: string;
  id: string;
  ciphertext: string;
  receivedAt: number;
}

// Client side of the helper's socket: queues frames and close events.
class Client {
  frames: Frame[] = [];
  closed: { code: number } | null = null;
  private waiters: (() => void)[] = [];

  constructor(readonly ws: WebSocket) {
    ws.accept();
    ws.addEventListener("message", (e) => {
      this.frames.push(JSON.parse(e.data as string));
      this.wake();
    });
    ws.addEventListener("close", (e) => {
      this.closed = { code: e.code };
      this.wake();
    });
  }

  private wake() {
    for (const w of this.waiters.splice(0)) w();
  }

  async until(cond: () => boolean, ms = 2000): Promise<void> {
    const deadline = Date.now() + ms;
    while (!cond()) {
      if (Date.now() > deadline) throw new Error("timed out waiting for socket");
      await new Promise<void>((r) => {
        this.waiters.push(r);
        setTimeout(r, 50);
      });
    }
  }

  async next(n: number): Promise<Frame[]> {
    await this.until(() => this.frames.length >= n);
    return this.frames.slice(0, n);
  }

  ack(id: string) {
    this.ws.send(JSON.stringify({ type: "ack", id }));
  }
}

async function connect(host: Host, bearer = host.bearer): Promise<Response> {
  return call(`/v1/mailbox/${host.hostId}/ws`, {
    headers: { upgrade: "websocket", authorization: `Bearer ${bearer}` },
  });
}

async function open(host: Host): Promise<Client> {
  const res = await connect(host);
  expect(res.status).toBe(101);
  return new Client(res.webSocket!);
}

function stub(hostId: string) {
  return env.MAILBOX.get(env.MAILBOX.idFromName(hostId));
}

async function pendingCount(hostId: string): Promise<number> {
  return runInDurableObject(stub(hostId), async (_o: Mailbox, state) => (await state.storage.list({ prefix: "msg:" })).size);
}

afterEach(() => vi.restoreAllMocks());

describe("mailbox", () => {
  it("post then connect delivers pending message", async () => {
    const host = await newHost();
    const res = await post(host.hostId, "AQsealed_bytes-1");
    expect(res.status).toBe(202);
    const { id } = await res.json<{ id: string }>();
    const client = await open(host);
    const [frame] = await client.next(1);
    expect(frame).toMatchObject({ type: "msg", id, ciphertext: "AQsealed_bytes-1" });
    expect(Math.abs(frame.receivedAt - Date.now())).toBeLessThan(10_000);
  });

  it("connected socket receives new posts live", async () => {
    const host = await newHost();
    const client = await open(host);
    const res = await post(host.hostId, "AQlive");
    const { id } = await res.json<{ id: string }>();
    const [frame] = await client.next(1);
    expect(frame).toMatchObject({ type: "msg", id, ciphertext: "AQlive" });
  });

  it("ack deletes and reconnect does not redeliver", async () => {
    const host = await newHost();
    await post(host.hostId, "AQone");
    const first = await open(host);
    const [frame] = await first.next(1);
    first.ack(frame.id);
    await vi.waitUntil(async () => (await pendingCount(host.hostId)) === 0, { timeout: 2000 });
    first.ws.close(1000);
    const second = await open(host);
    await post(host.hostId, "AQtwo");
    const [next] = await second.next(1);
    expect(next.ciphertext).toBe("AQtwo");
  });

  it("unacked message redelivered on reconnect", async () => {
    const host = await newHost();
    await post(host.hostId, "AQkeep");
    const first = await open(host);
    const [frame] = await first.next(1);
    first.ws.close(1000);
    const second = await open(host);
    const [again] = await second.next(1);
    expect(again).toMatchObject({ id: frame.id, ciphertext: "AQkeep" });
  });

  it("ping is answered with pong", async () => {
    const host = await newHost();
    const client = await open(host);
    client.ws.send('{"type":"ping"}');
    const [frame] = await client.next(1);
    expect(frame.type).toBe("pong");
  });

  it("message older than 600 s is gone after alarm", async () => {
    const host = await newHost();
    await post(host.hostId, "AQold");
    await post(host.hostId, "AQnew");
    await runInDurableObject(stub(host.hostId), async (_o: Mailbox, state) => {
      const all = await state.storage.list<{ ciphertext: string; receivedAt: number }>({ prefix: "msg:" });
      for (const [key, msg] of all) {
        if (msg.ciphertext === "AQold") await state.storage.put(key, { ...msg, receivedAt: Date.now() - MESSAGE_TTL_MS - 1 });
      }
    });
    expect(await runDurableObjectAlarm(stub(host.hostId))).toBe(true);
    const left = await runInDurableObject(stub(host.hostId), async (_o: Mailbox, state) => {
      const all = await state.storage.list<{ ciphertext: string }>({ prefix: "msg:" });
      return { texts: [...all.values()].map((m) => m.ciphertext), alarm: await state.storage.getAlarm() };
    });
    expect(left.texts).toEqual(["AQnew"]);
    expect(left.alarm).not.toBeNull();
  });

  it("wrong secret gets 401", async () => {
    const host = await newHost();
    const other = await newHost();
    expect((await connect(host, other.bearer)).status).toBe(401);
    expect((await connect(host, "not-a-secret")).status).toBe(401);
    const res = await call(`/v1/mailbox/${host.hostId}/ws`, { headers: { upgrade: "websocket" } });
    expect(res.status).toBe(401);
  });

  it("malformed hostId 404", async () => {
    expect((await post("h_UPPERCASEUPPERCASEUPPERCA")).status).toBe(404);
    expect((await post("h_short")).status).toBe(404);
    const host = await newHost();
    expect((await call(`/v1/mailbox/${host.hostId}/other`, { method: "POST" })).status).toBe(404);
  });

  it("51st pending message 507", async () => {
    const host = await newHost();
    for (let i = 0; i < MAX_PENDING; i++) expect((await post(host.hostId)).status).toBe(202);
    expect((await post(host.hostId)).status).toBe(507);
  });

  it("second connection replaces first with code 4000", async () => {
    const host = await newHost();
    const first = await open(host);
    const second = await open(host);
    await first.until(() => first.closed !== null);
    expect(first.closed?.code).toBe(REPLACED_CODE);
    await post(host.hostId, "AQafter");
    const [frame] = await second.next(1);
    expect(frame.ciphertext).toBe("AQafter");
  });

  it("rejects missing, oversized and non-base64url ciphertext", async () => {
    const host = await newHost();
    const bad = [{}, { ciphertext: "" }, { ciphertext: "a".repeat(4097) }, { ciphertext: "a+b" }, "not json"];
    for (const body of bad) {
      const res = await call(`/v1/mailbox/${host.hostId}`, {
        method: "POST",
        body: typeof body === "string" ? body : JSON.stringify(body),
      });
      expect(res.status).toBe(400);
      expect(await res.json()).toEqual({ error: "bad_request", field: "ciphertext" });
    }
  });

  it("61st post from one IP in a minute returns 429", async () => {
    const ip = crypto.randomUUID();
    for (let i = 0; i < POST_IP_LIMIT; i++) {
      const host = await newHost();
      expect((await post(host.hostId, undefined, ip)).status).toBe(202);
    }
    const res = await post((await newHost()).hostId, undefined, ip);
    expect(res.status).toBe(429);
    expect(res.headers.get("retry-after")).not.toBeNull();
  });

  it("wrong method returns 405", async () => {
    const host = await newHost();
    expect((await call(`/v1/mailbox/${host.hostId}`)).status).toBe(405);
    expect((await call(`/v1/mailbox/${host.hostId}/ws`, { method: "POST" })).status).toBe(405);
  });

  it("logs contain no ciphertext or secret", async () => {
    const lines: string[] = [];
    for (const m of ["log", "info", "warn", "error", "debug"] as const) {
      vi.spyOn(console, m).mockImplementation((...a: unknown[]) => void lines.push(a.map(String).join(" ")));
    }
    const host = await newHost();
    await post(host.hostId, "AQsecretciphertext");
    const client = await open(host);
    await client.next(1);
    await connect(host, (await newHost()).bearer);
    const out = lines.join("\n");
    expect(out).not.toContain("AQsecretciphertext");
    expect(out).not.toContain(host.bearer);
  });
});
