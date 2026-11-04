import { generateKeyPairSync } from "node:crypto";
import { cloudflareTest } from "@cloudflare/vitest-plugin";
import { defineConfig } from "vitest/config";

// A throwaway P-256 key minted per run stands in for the APNs .p8 secret, so
// no real key ever sits in the repo or on disk.
const { privateKey, publicKey } = generateKeyPairSync("ec", { namedCurve: "P-256" });

export default defineConfig({
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
