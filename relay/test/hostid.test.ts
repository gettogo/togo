import { describe, expect, it } from "vitest";
import { constantTimeEqual, decodeSecret, hostIdFromSecret } from "../src/hostid";
import vectors from "./vectors/hostid.json";

function fromHex(hex: string): Uint8Array {
  return Uint8Array.from(hex.match(/../g)!, (h) => parseInt(h, 16));
}

describe("hostIdFromSecret", () => {
  it("matches shared vector", async () => {
    for (const v of vectors.vectors) {
      expect(await hostIdFromSecret(fromHex(v.secretHex))).toBe(v.hostId);
    }
  });
});

describe("decodeSecret", () => {
  it("accepts unpadded base64url of 32 bytes", () => {
    expect(decodeSecret("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")).toEqual(fromHex(vectors.vectors[0].secretHex));
  });

  it("rejects padding, wrong length and non-url alphabet", () => {
    expect(decodeSecret("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")).toBeNull();
    expect(decodeSecret("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh")).toBeNull();
    expect(decodeSecret("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdH+8")).toBeNull();
  });
});

describe("constantTimeEqual", () => {
  it("compares content and length", () => {
    expect(constantTimeEqual("h_abc", "h_abc")).toBe(true);
    expect(constantTimeEqual("h_abc", "h_abd")).toBe(false);
    expect(constantTimeEqual("h_abc", "h_ab")).toBe(false);
    expect(constantTimeEqual("", "h_abc")).toBe(false);
  });
});
