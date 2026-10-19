import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

/**
 * Static RTL lint: the app is bidirectional (Arabic RTL / English LTR), so
 * physical directional utilities (ml-/pr-/left-/text-right/rounded-l/...) are
 * forbidden — use logical ones (ms-/pe-/start-/text-end/rounded-s/...).
 *
 * `translate-x` has no logical equivalent; it is allowed only when the same
 * line also provides an `rtl:` counterpart.
 */
const SRC = join(__dirname, "..");

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return name === "__tests__" ? [] : walk(p);
    return p.endsWith(".tsx") ? [p] : [];
  });
}

// Optional variant prefixes (hover:, sm:, dark:, group-hover: ...), optional negative.
const PREFIX = String.raw`(?<![\w-])(?:[a-z0-9-]+:)*-?`;
const PHYSICAL = new RegExp(
  PREFIX +
    String.raw`(?:` +
    [
      String.raw`(?:m|p|scroll-m|scroll-p)[lr]-[\w.\[\]/%-]+`, // ml-4, pr-2, scroll-ml-1
      String.raw`(?:left|right)-[\w.\[\]/%-]+`, // left-0, right-1/2
      String.raw`inset-[lr]-[\w.\[\]/%-]+`,
      String.raw`rounded-(?:l|r|tl|tr|bl|br)(?:-[\w.\[\]-]+)?`, // rounded-l, rounded-tr-xl
      String.raw`border-[lr](?:-[\w.\[\]/-]+)?`, // border-l, border-r-2
      String.raw`text-(?:left|right)`,
      String.raw`(?:float|clear)-(?:left|right)`,
      String.raw`(?:space|divide)-x-[\w.\[\]-]+`, // space-x-* flips incorrectly without rtl:space-x-reverse
    ].join("|") +
    String.raw`)(?![\w-])`,
  "g"
);
const TRANSLATE_X = new RegExp(PREFIX + String.raw`translate-x-[\w.\[\]/%-]+(?![\w-])`, "g");
const INLINE_PHYSICAL_STYLE = /style=\{\{[^}]*\b(left|right|marginLeft|marginRight|paddingLeft|paddingRight)\s*:/;

describe("RTL lint: no physical directional classes in src/**/*.tsx", () => {
  const files = walk(SRC);

  it("self-check: the patterns catch physical classes and ignore logical ones", () => {
    const bad = 'className="ml-2 pr-4 left-0 sm:right-1/2 text-right rounded-l-xl border-r-2 space-x-2 hover:-mr-1"';
    expect(bad.match(PHYSICAL)).toEqual(["ml-2", "pr-4", "left-0", "sm:right-1/2", "text-right", "rounded-l-xl", "border-r-2", "space-x-2", "hover:-mr-1"]);
    const good = 'className="ms-2 pe-4 start-0 end-0 text-start rounded-s-xl border-e-2 gap-2 inset-x-0 min-h-12 px-3 py-1 mx-auto"';
    expect(good.match(PHYSICAL)).toBeNull();
    expect("group-hover:translate-x-0.5".match(TRANSLATE_X)).toHaveLength(1);
  });

  it("finds source files to scan", () => {
    expect(files.length).toBeGreaterThan(5);
  });

  it("uses logical Tailwind utilities only", () => {
    const violations: string[] = [];
    for (const file of files) {
      readFileSync(file, "utf8")
        .split("\n")
        .forEach((line, i) => {
          if (/^\s*(\/\/|\*|\{\/\*)/.test(line)) return; // comments
          const where = `${relative(SRC, file)}:${i + 1}`;
          for (const m of line.match(PHYSICAL) ?? []) violations.push(`${where}  ${m}`);
          const tx = line.match(TRANSLATE_X) ?? [];
          if (tx.length && !/\brtl:/.test(line)) {
            for (const m of tx) violations.push(`${where}  ${m} (no rtl: counterpart)`);
          }
          if (INLINE_PHYSICAL_STYLE.test(line)) violations.push(`${where}  inline physical style`);
        });
    }
    expect(violations, `Physical directional classes found:\n${violations.join("\n")}`).toEqual([]);
  });

  it("form controls use >= 16px text (prevents iOS focus zoom)", () => {
    const violations: string[] = [];
    for (const file of files) {
      const src = readFileSync(file, "utf8");
      for (const m of src.matchAll(/<(input|textarea|select)\b/g)) {
        const rest = src.slice(m.index! + m[0].length);
        const nextTag = rest.indexOf("<");
        const ci = rest.indexOf("className=");
        if (ci === -1 || (nextTag !== -1 && ci > nextTag)) continue;
        const head = rest.slice(0, ci);
        if (/type="(checkbox|radio|file|color)"/.test(head)) continue;
        const cls = rest.slice(ci, rest.indexOf(rest[ci + 10] === '"' ? '"' : "`", ci + 12));
        if (/(?<![\w:-])text-(xs|sm|\[1[0-5]px\])(?![\w-])/.test(cls)) {
          const line = src.slice(0, m.index).split("\n").length;
          violations.push(`${relative(SRC, file)}:${line} <${m[1]}>`);
        }
      }
    }
    expect(violations, `Form controls below 16px:\n${violations.join("\n")}`).toEqual([]);
  });
});
