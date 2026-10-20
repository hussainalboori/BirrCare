import { describe, expect, it } from "vitest";
import { DOSE_COPY, VOMIT_MINUTES } from "@/lib/doseWarnings";

// Issue #9: no string may contain a dosing instruction; warnings route to the doctor.
function allStrings(locale: "ar" | "en"): string[] {
  const out: string[] = [];
  for (const v of Object.values(DOSE_COPY[locale])) {
    if (typeof v === "string") out.push(v);
    else if (Array.isArray(v)) out.push(...v);
    else if (typeof v === "function") {
      out.push((v as (n: number | null) => string)(10), (v as (n: number | null) => string)(null));
    }
  }
  return out;
}

const DOSING = {
  en: [/\bgive (it |the dose )?again\b/i, /\brepeat\b/i, /\bre-?dose\b/i, /\bextra dose\b/i, /\bhalf (a|the) (pill|dose)\b/i, /\d+\s*(mg|ml|tablets?|pills?)\b/i, /\bgive (another|one more)\b/i],
  ar: [/أعد(ي)? (إعطاء|الجرعة)/, /كرر/, /جرعة إضافية/, /نصف (حبة|الجرعة)/, /\d+\s*(ملغ|مل|حبة|حبات)/, /أعطِ(ها)? (جرعة|حبة) (أخرى|ثانية)/],
};

describe("dose warnings never instruct a re-dose", () => {
  for (const locale of ["ar", "en"] as const) {
    it(`${locale}: no dosing instructions in any string`, () => {
      for (const s of allStrings(locale)) {
        for (const re of DOSING[locale]) expect(s, `"${s}" matches ${re}`).not.toMatch(re);
      }
    });

    it(`${locale}: partial and vomited warnings send the family to the doctor or pharmacist`, () => {
      const c = DOSE_COPY[locale];
      for (const w of [c.warnPartial, c.warnVomited(10), c.warnVomited(null)]) {
        expect(w).toMatch(locale === "ar" ? /الطبيب أو الصيدلي/ : /doctor or pharmacist/);
      }
    });
  }

  it("states minutes only when known", () => {
    expect(DOSE_COPY.en.warnVomited(10)).toContain("10 min later");
    expect(DOSE_COPY.en.warnVomited(null)).not.toMatch(/\d/);
    expect(DOSE_COPY.ar.warnVomited(10)).toContain("بعد 10 دقائق");
    expect(DOSE_COPY.ar.warnVomited(15)).toContain("بعد 15 دقيقة");
    expect(DOSE_COPY.ar.minutes(60)).toBe("ساعة");
    expect(VOMIT_MINUTES).toContain(10);
  });
});
