import { describe, expect, it } from "vitest";
import { readBodyCapped } from "../src/http";

function chunked(total: number, chunk = 1024): Request {
  let sent = 0;
  const body = new ReadableStream<Uint8Array>({
    pull(c) {
      if (sent >= total) return c.close();
      const n = Math.min(chunk, total - sent);
      sent += n;
      c.enqueue(new Uint8Array(n).fill(120));
    },
  });
  // No content-length: the declared-size check cannot see it.
  return new Request("https://relay/x", { method: "POST", body, duplex: "half" } as RequestInit);
}

describe("readBodyCapped", () => {
  it("stops a chunked body past the cap without buffering it", async () => {
    expect(await readBodyCapped(chunked(64 * 1024), 8192)).toBeNull();
  });
  it("returns a body within the cap", async () => {
    const got = await readBodyCapped(chunked(3000, 700), 8192);
    expect(got?.byteLength).toBe(3000);
  });
  it("refuses a declared length over the cap", async () => {
    const r = new Request("https://relay/x", { method: "POST", body: "x".repeat(10), headers: { "content-length": "99999" } });
    expect(await readBodyCapped(r, 8192)).toBeNull();
  });
});
