import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { HANDOVER_TAGS } from "@/lib/handoverTags";

// Issue #18: the server rejects unknown tags, so the two lists must not drift.
describe("handover tags", () => {
  const go = readFileSync(join(__dirname, "../../../backend/internal/handlers/handover_tags.go"), "utf8");
  const block = go.slice(go.indexOf("var handoverTags"), go.indexOf("}", go.indexOf("var handoverTags")));
  const server = new Map([...block.matchAll(/"([a-z_]+)":\s*(true|false)/g)].map((m) => [m[1], m[2] === "true"]));

  it("match the backend list, including which are urgent", () => {
    expect(server.size).toBe(12);
    expect(new Set(HANDOVER_TAGS.map((t) => t.key))).toEqual(new Set(server.keys()));
    for (const t of HANDOVER_TAGS) expect(!!t.urgent, t.key).toBe(server.get(t.key));
  });

  it("have Arabic and English labels", () => {
    for (const t of HANDOVER_TAGS) {
      expect(t.ar).toMatch(/[؀-ۿ]/);
      expect(t.en).toMatch(/[A-Za-z]/);
    }
  });
});
