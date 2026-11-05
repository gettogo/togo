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
const production = config.env.production;

describe("wrangler production environment", () => {
  it("production env has no workers.dev", () => {
    expect(production.workers_dev).toBe(false);
    expect(production.routes).toEqual([{ pattern: "relay.togo.app", custom_domain: true }]);
  });

  it("production env keeps mailbox binding and migrations", () => {
    expect(production.durable_objects).toEqual(config.durable_objects);
    expect(production.vars).toEqual(config.vars);
    expect(production.observability).toEqual(config.observability);
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
