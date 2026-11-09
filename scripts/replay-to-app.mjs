#!/usr/bin/env node
// Turns a recorded replay fixture (protocol/v1/replays/*.jsonl: a header, then {t, dir, method,
// frame} lines) into the app's UI-test replay format (apps/ios/Togo/Debug/Resources/*.jsonl: one
// JSON-RPC message per line, notifications carrying "t", see ReplayScript).
//
//   node scripts/replay-to-app.mjs protocol/v1/replays/claude.demo.jsonl apps/ios/Togo/Debug/Resources/replay-demo.jsonl "comment"
import { readFileSync, writeFileSync } from "node:fs";

const [src, dst, ...comment] = process.argv.slice(2);
if (!src || !dst) {
  console.error("usage: replay-to-app.mjs <fixture> <out> [comment]");
  process.exit(2);
}
const [header, ...lines] = readFileSync(src, "utf8").split("\n").filter((l) => l.trim());
const h = JSON.parse(header);
const out = [
  `// ${comment.join(" ") || `${h.agent} ${h.scenario}`}`,
  `// Converted from ${src.replace(/^.*protocol\//, "protocol/")} (recorded ${h.recorded}) by scripts/replay-to-app.mjs.`,
];
for (const raw of lines) {
  const l = JSON.parse(raw);
  const f = l.frame;
  if (!f) continue;
  if (f.method && f.id === undefined) out.push(JSON.stringify({ jsonrpc: "2.0", t: l.t, method: f.method, params: f.params }));
  else out.push(JSON.stringify(f));
}
writeFileSync(dst, out.join("\n") + "\n");
console.log(`wrote ${dst} (${out.length - 2} messages)`);
