import { generateKeyPairSync } from "node:crypto";
import { cloudflareTest } from "@cloudflare/vitest-plugin";
import { defineConfig } from "vitest/config";

// A throwaway P-256 key minted per run stands in for the APNs .p8 secret, so
// no real key ever sits in the repo or on disk.
const { privateKey, publicKey } = generateKeyPairSync("ec", { namedCurve: "P-256" });

export default defineConfig({
  // Rate-limit tests send 30 to 60 requests through the Workers runtime, which
  // can pass 5 s on a busy CI runner.
  test: { testTimeout: 20_000 },
  plugins: [
    cloudflareTest({
      wrangler: { configPath: "./wrangler.jsonc" },
      miniflare: {
        bindings: {
          APNS_KEY: privateKey.export({ type: "pkcs8", format: "pem" }).toString(),
          APNS_KEY_ID: "TESTKEY123",
          APNS_TEAM_ID: "TESTTEAM12",
          ALLOW_MISSING_CLIENT_IP: "1",
          TEST_APNS_PUBLIC_JWK: JSON.stringify(publicKey.export({ format: "jwk" })),
        },
      },
    }),
  ],
});
