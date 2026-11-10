import { DurableObject } from "cloudflare:workers";

export type Admission =
  | { status: "ok" }
  | { status: "limited"; retryAfter: number }
  | { status: "unregistered" };

interface Window {
  start: number;
  count: number;
  /** When the window closes; absent on windows stored before it was kept. */
  end?: number;
}

export const UNREGISTERED_TTL_MS = 24 * 60 * 60 * 1000;

// One instance per key (`tok:<hash prefix>` or `ip:<address>`), so a single
// object's input gate serializes its counter updates without locks.
export class RateLimiter extends DurableObject<Env> {
  async admit(limit: number, windowMs: number): Promise<Admission> {
    const now = Date.now();
    const until = await this.ctx.storage.get<number>("unregisteredUntil");
    if (until !== undefined) {
      if (now < until) return { status: "unregistered" };
      await this.ctx.storage.delete("unregisteredUntil");
    }
    let win = await this.ctx.storage.get<Window>("window");
    if (!win || now - win.start >= windowMs) {
      win = { start: now, count: 0, end: now + windowMs };
      // Every token and address gets its own object; the alarm clears it
      // once the window is over so idle keys do not pile up in storage.
      await this.ctx.storage.setAlarm(win.end!);
    }
    if (win.count >= limit) {
      const retryAfter = Math.max(1, Math.ceil((win.start + windowMs - now) / 1000));
      return { status: "limited", retryAfter };
    }
    win.count += 1;
    await this.ctx.storage.put("window", win);
    return { status: "ok" };
  }

  async markUnregistered(): Promise<void> {
    const until = Date.now() + UNREGISTERED_TTL_MS;
    await this.ctx.storage.put("unregisteredUntil", until);
    await this.ctx.storage.setAlarm(until);
  }

  async alarm(): Promise<void> {
    const now = Date.now();
    const until = await this.ctx.storage.get<number>("unregisteredUntil");
    if (until !== undefined && now < until) {
      await this.ctx.storage.setAlarm(until);
      return;
    }
    const win = await this.ctx.storage.get<Window>("window");
    if (win?.end !== undefined && now < win.end) {
      await this.ctx.storage.setAlarm(win.end);
      return;
    }
    await this.ctx.storage.deleteAll();
  }
}
