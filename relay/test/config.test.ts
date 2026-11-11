/// <reference types="vite/client" />
import { describe, expect, it } from "vitest";
import raw from "../wrangler.jsonc?raw";

const files = import.meta.glob(
  ["../**/*", "../../scripts/relay-deploy.sh", "!../node_modules/**", "!../.wrangler/**", "!../test/vectors/**", "!../package-lock.json"],
  { query: "?raw", import: "default", eager: true },
) as Record<string, string>;

// Strips // comments and trailing commas so the JSONC file parses.
function parse(text: string): any {
  const noComments = text.replace(/^\s*\/\/.*$/gm, "");
  return JSON.parse(noComments.replace(/,(\s*[}\]])/g, "$1"));
}

const config = parse(raw);

describe("wrangler config", () => {
  it("one worker owns the custom domain", () => {
    expect(config.env).toBeUndefined();
    expect(config.routes).toEqual([{ pattern: "relay.togoapp.dev", custom_domain: true }]);
  });

  it("keeps the mailbox binding and migrations", () => {
    expect(config.durable_objects.bindings).toEqual([
      { name: "RATE_LIMITER", class_name: "RateLimiter" },
      { name: "MAILBOX", class_name: "Mailbox" },
    ]);
    expect(config.migrations).toEqual([
      { tag: "v1", new_sqlite_classes: ["RateLimiter", "Mailbox"] },
    ]);
  });

  it("no account subdomain anywhere in relay/", () => {
    expect(Object.keys(files).length).toBeGreaterThan(5);
    const placeholder = "your-relay.example.workers.dev";
    for (const [path, text] of Object.entries(files)) {
      const stripped = text.split(placeholder).join("");
      const hit = /\.workers\.dev/.test(stripped);
      expect(hit, path).toBe(false);
    }
  });
});
