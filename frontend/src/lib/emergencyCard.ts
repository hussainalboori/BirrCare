/**
 * Issue #19: renders the paramedics' emergency card as one self-contained HTML
 * page, English and Arabic side by side (paramedics may not read Arabic).
 * Pure function: no browser APIs, every value escaped.
 */

export interface EmergencyCard {
  patient: {
    name: string;
    dob: string;
    diagnosis_ar: string;
    diagnosis_en: string;
    stroke_date: string;
    allergies_ar: string;
    allergies_en: string;
    blood_type: string;
    baseline_bp: string;
    notes: string;
  };
  medications: {
    name_ar: string;
    name_en: string;
    dosage: string;
    slots: string[];
    times?: string[];
    blood_thinner: boolean;
    /** Issue #64: "do_not_crush" for slow-release / coated tablets. */
    crush?: "ok" | "do_not_crush" | "ask_pharmacist";
    /** Issue #56: the latest taken/partial dose that wasn't cancelled. */
    last_dose?: { at: string; status: "taken" | "partial"; hours_ago: number; vomited: boolean };
  }[];
  last_bp: { systolic: number; diastolic: number; pulse?: number; recorded_at: string }[];
  last_sugar?: { value: string; recorded_at: string };
  last_spo2?: { value: string; recorded_at: string };
  last_temperature?: { value: string; recorded_at: string };
  last_alertness?: { value: string; recorded_at: string };
  last_fast?: { face: boolean; arm: boolean; speech: boolean; recorded_at: string; last_normal_at?: string };
  last_insulin?: { at: string; units: number; plan_name_ar: string; plan_name_en: string; blood_sugar?: number };
  prn_24h?: { name_ar: string; name_en: string; dosage: string; count: number; total_dose?: string; last_at: string }[];
  /** Issue #60: the latest fall or seizure in the last 7 days. */
  last_incident?: { kind: "fall" | "seizure"; at: string; hit_head?: boolean; awake_normal?: boolean; pain_or_bleeding?: boolean; duration_seconds?: number; repeat?: boolean; escalate: boolean };
  /** Issue #64: a swallowing difficulty noted in a handover in the last 7 days. */
  swallowing_difficulty?: { at: string };
  contacts: { emergency_number?: string; doctor_name?: string; doctor_phone?: string };
  family: { name: string; phone: string }[];
  generated_at: string;
  time_zone: string;
}

const esc = (s: unknown) =>
  String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);

const SLOTS: Record<string, [string, string]> = {
  night: ["night", "ليلاً"],
  morning: ["morning", "صباحاً"],
  afternoon: ["afternoon", "ظهراً"],
  evening: ["evening", "مساءً"],
  bedtime: ["bedtime", "قبل النوم"],
};

const ALERTNESS: Record<string, [string, string]> = {
  alert: ["Alert, responsive", "واعية ومتجاوبة"],
  confused: ["Confused / slow to respond", "مشوشة / بطيئة الاستجابة"],
  restless: ["Restless", "مضطربة"],
  drowsy: ["Hard to wake", "صعب إيقاظها"],
};

function when(iso: string | undefined, tz: string, ar: boolean): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const opts: Intl.DateTimeFormatOptions = { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hour12: false };
  try {
    return d.toLocaleString(ar ? "ar-SA-u-nu-latn-ca-gregory" : "en-GB", { ...opts, timeZone: tz || "UTC" });
  } catch {
    return d.toLocaleString(ar ? "ar-SA-u-nu-latn-ca-gregory" : "en-GB", opts);
  }
}

/** "UTC+3" for the household zone at that moment, so a hospital elsewhere isn't misled. */
function offset(iso: string, tz: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  try {
    const name = new Intl.DateTimeFormat("en-US", { timeZone: tz || "UTC", timeZoneName: "shortOffset" }).formatToParts(d).find((x) => x.type === "timeZoneName")?.value || "";
    return name.replace(/^GMT/, "UTC") || "UTC";
  } catch {
    return "";
  }
}

/** An absolute time with its UTC offset: "26 Oct, 23:30 UTC+3". */
function whenAbs(iso: string | undefined, tz: string, ar: boolean): string {
  if (!iso) return "";
  return `${when(iso, tz, ar)} ${offset(iso, tz)}`.trim();
}

/** 8.25 → "8 h 15 min" / "8 س 15 د". */
function ago(hours: number, ar: boolean): string {
  const total = Math.max(0, Math.round(hours * 60));
  const h = Math.floor(total / 60);
  const m = total % 60;
  if (ar) return h ? `${h} س ${m} د` : `${m} د`;
  return h ? `${h} h ${m} min` : `${m} min`;
}

function hoursSince(iso: string, generated: string): number {
  const t = new Date(iso).getTime();
  const g = new Date(generated).getTime();
  return Number.isNaN(t) || Number.isNaN(g) ? 0 : (g - t) / 3_600_000;
}

function age(dob: string, now: Date): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(dob);
  if (!m) return "";
  let a = now.getUTCFullYear() - Number(m[1]);
  if (now.getUTCMonth() + 1 < Number(m[2]) || (now.getUTCMonth() + 1 === Number(m[2]) && now.getUTCDate() < Number(m[3]))) a--;
  return a >= 0 && a < 130 ? String(a) : "";
}

/** One row: English label and value | Arabic label and value. */
function row(en: string, enVal: string, ar: string, arVal: string, cls = ""): string {
  if (!enVal && !arVal) return "";
  return `<div class="row ${cls}"><div class="en"><span class="k">${esc(en)}</span><span class="v">${enVal || "—"}</span></div><div class="ar" dir="rtl" lang="ar"><span class="k">${esc(ar)}</span><span class="v">${arVal || enVal || "—"}</span></div></div>`;
}

const STYLE = `
*{box-sizing:border-box}html{-webkit-text-size-adjust:100%}
body{margin:0;background:#fff;color:#0f172a;font:18px/1.35 system-ui,-apple-system,"Segoe UI",Roboto,"Noto Sans Arabic",Tahoma,sans-serif}
.top{background:#b91c1c;color:#fff;padding:12px 16px;display:flex;justify-content:space-between;gap:12px;font-weight:800;font-size:18px}
main{padding:12px 16px 32px;max-width:960px;margin:0 auto}
h1{font-size:30px;margin:6px 0 2px} .sub{color:#334155;font-size:18px;margin:0 0 4px}
.asof{display:inline-block;background:#fef3c7;color:#78350f;font-weight:700;border-radius:8px;padding:4px 10px;margin:6px 0 10px;font-size:16px}
h2{font-size:20px;margin:22px 0 8px;padding-bottom:4px;border-bottom:3px solid #0f172a;display:flex;justify-content:space-between;gap:8px}
.row{display:grid;grid-template-columns:1fr 1fr;gap:12px;padding:8px 0;border-bottom:1px solid #e2e8f0}
.k{display:block;font-size:14px;color:#475569;font-weight:700;text-transform:uppercase;letter-spacing:.02em}
.ar .k{text-transform:none;letter-spacing:0}
.v{display:block;font-size:20px;font-weight:700}
.ar{text-align:right}
.alarm .v{color:#b91c1c}
.med{display:grid;grid-template-columns:1fr 1fr;gap:12px;padding:10px 0;border-bottom:1px solid #e2e8f0}
.med b{font-size:21px} .med .d{font-size:18px;font-weight:700} .med .s{color:#334155;font-size:16px}
.nocrush{display:inline-block;background:#fff;color:#b91c1c;border:2px solid #b91c1c;font-weight:800;font-size:14px;border-radius:6px;padding:1px 7px;margin-top:4px;margin-inline-start:4px}
.thin{display:inline-block;background:#b91c1c;color:#fff;font-weight:800;font-size:14px;border-radius:6px;padding:2px 8px;margin-top:4px}
table{width:100%;border-collapse:collapse;font-size:20px} td,th{padding:8px 6px;border-bottom:1px solid #e2e8f0;text-align:left}
th{font-size:14px;color:#475569} .num{font-weight:800;font-variant-numeric:tabular-nums;direction:ltr;unicode-bidi:isolate}
a.tel{display:inline-block;background:#0f172a;color:#fff;text-decoration:none;font-weight:800;border-radius:10px;padding:10px 14px;margin:4px 0;font-size:20px}
a.sos{background:#b91c1c}
.empty{color:#64748b;font-size:16px}
.now{border:3px solid #b91c1c;border-radius:12px;padding:4px 12px 8px;margin:12px 0 4px}
.now .row:last-of-type{border-bottom:0}
.now .thinrow .v{color:#b91c1c;font-size:22px;font-weight:900}
.flag{display:inline-block;background:#fef3c7;color:#78350f;font-weight:800;border-radius:6px;padding:0 6px;font-size:16px}
@media print{.top{background:#fff;color:#b91c1c;border-bottom:3px solid #b91c1c}a.tel{background:#fff;color:#0f172a;border:2px solid #0f172a}body{font-size:14px}h1{font-size:24px}}
`;

export function renderEmergencyCard(c: EmergencyCard, now = new Date()): string {
  const tz = c.time_zone;
  const p = c.patient;
  const years = age(p.dob, now);
  const asOfEn = when(c.generated_at, tz, false);
  const asOfAr = when(c.generated_at, tz, true);

  const meds = c.medications.length
    ? c.medications
        .map((m) => {
          const slotsEn = m.slots.map((s) => SLOTS[s]?.[0] || s).join(", ");
          const slotsAr = m.slots.map((s) => SLOTS[s]?.[1] || s).join("، ");
          const times = m.times?.length ? ` (${m.times.map(esc).join(", ")})` : "";
          return `<div class="med"><div><b>${esc(m.name_en)}</b> <span class="d num">${esc(m.dosage)}</span><div class="s">${esc(slotsEn)}${times}</div>${
            m.blood_thinner ? '<span class="thin">BLOOD THINNER / ANTIPLATELET</span>' : ""
          }${m.crush === "do_not_crush" ? '<span class="nocrush">DO NOT CRUSH</span>' : ""}</div><div class="ar" dir="rtl" lang="ar"><b>${esc(m.name_ar)}</b> <span class="d num">${esc(m.dosage)}</span><div class="s">${esc(slotsAr)}${times}</div>${
            m.blood_thinner ? '<span class="thin">مميّع للدم / مضاد للصفائح</span>' : ""
          }${m.crush === "do_not_crush" ? '<span class="nocrush">لا تُطحن</span>' : ""}</div></div>`;
        })
        .join("")
    : '<p class="empty">No medicines recorded · لا توجد أدوية مسجلة</p>';

  // Issue #56: "When did she last take her blood thinner?" first, before anything else.
  const flags = (d: { status: string; vomited: boolean }, ar: boolean) =>
    [d.status === "partial" && (ar ? "جزئية" : "partial"), d.vomited && (ar ? "استفرغتها" : "vomited")].filter(Boolean).map((x) => ` <span class="flag">${esc(x)}</span>`).join("");
  const thinners = c.medications
    .filter((m) => m.blood_thinner)
    .map((m) => {
      const d = m.last_dose;
      const en = d
        ? `${esc(m.name_en)} <span class="num">${esc(m.dosage)}</span> — last dose <bdi class="num">${esc(whenAbs(d.at, tz, false))}</bdi> (${esc(ago(d.hours_ago, false))} ago)${flags(d, false)}`
        : `${esc(m.name_en)} <span class="num">${esc(m.dosage)}</span> — no dose recorded in this app`;
      const ar = d
        ? `${esc(m.name_ar)} <span class="num">${esc(m.dosage)}</span> — آخر جرعة <bdi class="num">${esc(whenAbs(d.at, tz, true))}</bdi> (قبل ${esc(ago(d.hours_ago, true))})${flags(d, true)}`
        : `${esc(m.name_ar)} <span class="num">${esc(m.dosage)}</span> — لا توجد جرعة مسجلة في التطبيق`;
      return row("Blood thinner", en, "مميّع للدم", ar, "thinrow");
    })
    .join("");
  const li = c.last_insulin;
  const insulin = li
    ? row(
        "Last insulin",
        `<span class="num">${esc(li.units)} u</span> ${esc(li.plan_name_en)} — <bdi class="num">${esc(whenAbs(li.at, tz, false))}</bdi> (${esc(ago(hoursSince(li.at, c.generated_at), false))} ago)${li.blood_sugar != null ? ` · sugar <span class="num">${esc(li.blood_sugar)} mmol/L</span>` : ""}`,
        "آخر إنسولين",
        `<span class="num">${esc(li.units)} u</span> ${esc(li.plan_name_ar)} — <bdi class="num">${esc(whenAbs(li.at, tz, true))}</bdi> (قبل ${esc(ago(hoursSince(li.at, c.generated_at), true))})${li.blood_sugar != null ? ` · السكر <span class="num">${esc(li.blood_sugar)} mmol/L</span>` : ""}`,
      )
    : "";
  const prn = (c.prn_24h || [])
    .map((x) =>
      row(
        "As-needed, last 24 h",
        `${esc(x.name_en)} <span class="num">${esc(x.dosage)}</span> ×<span class="num">${esc(x.count)}</span>${x.total_dose ? ` = <span class="num">${esc(x.total_dose)}</span>` : ""} — last <bdi class="num">${esc(whenAbs(x.last_at, tz, false))}</bdi>`,
        "عند الحاجة، آخر 24 ساعة",
        `${esc(x.name_ar)} <span class="num">${esc(x.dosage)}</span> ×<span class="num">${esc(x.count)}</span>${x.total_dose ? ` = <span class="num">${esc(x.total_dose)}</span>` : ""} — آخرها <bdi class="num">${esc(whenAbs(x.last_at, tz, true))}</bdi>`,
      ),
    )
    .join("");
  const inc = c.last_incident;
  const incident = inc
    ? (() => {
        const agoEn = esc(ago(hoursSince(inc.at, c.generated_at), false));
        const agoAr = esc(ago(hoursSince(inc.at, c.generated_at), true));
        const atEn = `<bdi class="num">${esc(whenAbs(inc.at, tz, false))}</bdi>`;
        const atAr = `<bdi class="num">${esc(whenAbs(inc.at, tz, true))}</bdi>`;
        if (inc.kind === "fall") {
          const en = [inc.hit_head && "hit her head", inc.awake_normal === false && "not awake / talking normally", inc.pain_or_bleeding && "pain or bleeding"].filter(Boolean).join(", ");
          const arx = [inc.hit_head && "ضربت رأسها", inc.awake_normal === false && "ليست واعية / كلامها غير طبيعي", inc.pain_or_bleeding && "ألم أو نزيف"].filter(Boolean).join("، ");
          return row("Fall", `${atEn} (${agoEn} ago)${en ? ` — ${esc(en)}` : ""}`, "سقوط", `${atAr} (قبل ${agoAr})${arx ? ` — ${esc(arx)}` : ""}`, inc.escalate ? "alarm" : "");
        }
        const secs = inc.duration_seconds ?? 0;
        const dur = `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, "0")}`;
        return row(
          "Seizure",
          `${atEn} (${agoEn} ago) — lasted <span class="num">${esc(dur)}</span> min${inc.repeat ? ", second within 24 h" : ""}`,
          "نوبة تشنج",
          `${atAr} (قبل ${agoAr}) — استمرت <span class="num">${esc(dur)}</span> د${inc.repeat ? "، الثانية خلال 24 ساعة" : ""}`,
          inc.escalate ? "alarm" : "",
        );
      })()
    : "";
  // Issue #64: aspiration risk; tablets may have been crushed.
  const sw = c.swallowing_difficulty;
  const swallowing = sw
    ? row(
        "Swallowing difficulty",
        `noted <bdi class="num">${esc(whenAbs(sw.at, tz, false))}</bdi> — aspiration risk`,
        "صعوبة في البلع",
        `سُجّلت <bdi class="num">${esc(whenAbs(sw.at, tz, true))}</bdi> — خطر دخول الطعام أو الشراب إلى الرئة`,
        "alarm",
      )
    : "";
  const recent =
    thinners || insulin || prn || incident || swallowing
      ? `<section class="now" data-testid="recent-doses">${thinners}${incident}${swallowing}${insulin}${prn}<p class="empty">As of · حتى <bdi class="num">${esc(whenAbs(c.generated_at, tz, false))}</bdi></p></section>`
      : "";

  const bp = c.last_bp.length
    ? `<table><tr><th>BP mmHg · الضغط</th><th>Pulse · النبض</th><th>Time · الوقت</th></tr>${c.last_bp
        .map(
          (b) =>
            `<tr><td class="num">${esc(b.systolic)}/${esc(b.diastolic)}</td><td class="num">${b.pulse != null ? esc(b.pulse) : "—"}</td><td class="num">${esc(when(b.recorded_at, tz, false))}</td></tr>`,
        )
        .join("")}</table>`
    : '<p class="empty">No readings · لا توجد قراءات</p>';

  const f = c.last_fast;
  const fastSigns = f ? [f.face && "face droop", f.arm && "arm weakness", f.speech && "speech"].filter(Boolean).join(", ") : "";
  const fastSignsAr = f ? [f.face && "ميلان الوجه", f.arm && "ضعف الذراع", f.speech && "الكلام"].filter(Boolean).join("، ") : "";
  const fastEn = f ? `${esc(fastSigns ? `POSITIVE: ${fastSigns}` : "Negative (no signs)")} — <bdi>${esc(when(f.recorded_at, tz, false))}</bdi>` : "";
  const fastAr = f ? `${esc(fastSignsAr ? `إيجابي: ${fastSignsAr}` : "سلبي (لا علامات)")} — <bdi>${esc(when(f.recorded_at, tz, true))}</bdi>` : "";
  const al = c.last_alertness ? ALERTNESS[c.last_alertness.value] || [c.last_alertness.value, c.last_alertness.value] : null;

  const contacts = [
    c.contacts.emergency_number &&
      `<a class="tel sos" href="tel:${esc(c.contacts.emergency_number)}">Emergency · الإسعاف <span class="num">${esc(c.contacts.emergency_number)}</span></a>`,
    c.contacts.doctor_phone &&
      `<a class="tel" href="tel:${esc(c.contacts.doctor_phone)}">${esc(c.contacts.doctor_name || "Doctor · الطبيب")} <span class="num">${esc(c.contacts.doctor_phone)}</span></a>`,
    ...c.family.map((m) => `<a class="tel" href="tel:${esc(m.phone)}">${esc(m.name)} <span class="num">${esc(m.phone)}</span></a>`),
  ]
    .filter(Boolean)
    .join("<br>");

  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><title>Emergency medical card</title><style>${STYLE}</style></head><body>
<div class="top"><span>EMERGENCY MEDICAL CARD</span><span dir="rtl" lang="ar">بطاقة طبية للطوارئ</span></div>
<main>
${recent}
<h1>${esc(p.name || "Patient · المريضة")}</h1>
<p class="sub">${years ? `Age ${esc(years)} · العمر ${esc(years)}` : ""}${p.dob ? ` · DOB <span class="num">${esc(p.dob)}</span>` : ""}${p.blood_type ? ` · Blood type · فصيلة الدم <span class="num">${esc(p.blood_type)}</span>` : ""}</p>
<span class="asof">As of · حتى <span class="num">${esc(asOfEn)}</span></span>
<h2><span>Condition</span><span dir="rtl" lang="ar">الحالة</span></h2>
${row("Diagnosis", esc(p.diagnosis_en), "التشخيص", esc(p.diagnosis_ar))}
${row("Stroke date", p.stroke_date ? `<span class="num">${esc(p.stroke_date)}</span>` : "", "تاريخ الجلطة", p.stroke_date ? `<span class="num">${esc(p.stroke_date)}</span>` : "")}
${row("Allergies", esc(p.allergies_en || (p.allergies_ar ? "" : "None recorded")), "الحساسية", esc(p.allergies_ar || (p.allergies_en ? "" : "لا توجد مسجلة")), "alarm")}
${row("Usual BP", p.baseline_bp ? `<span class="num">${esc(p.baseline_bp)}</span>` : "", "الضغط المعتاد", p.baseline_bp ? `<span class="num">${esc(p.baseline_bp)}</span>` : "")}
${row("Notes", esc(p.notes), "ملاحظات", esc(p.notes))}
<h2><span>Medicines</span><span dir="rtl" lang="ar">الأدوية</span></h2>
${meds}
<h2><span>Last blood pressure</span><span dir="rtl" lang="ar">آخر قراءات الضغط</span></h2>
${bp}
<h2><span>Latest</span><span dir="rtl" lang="ar">آخر الملاحظات</span></h2>
${row("Blood sugar", c.last_sugar ? `<span class="num">${esc(c.last_sugar.value)}</span> — <bdi>${esc(when(c.last_sugar.recorded_at, tz, false))}</bdi>` : "", "السكر", c.last_sugar ? `<span class="num">${esc(c.last_sugar.value)}</span> — <bdi>${esc(when(c.last_sugar.recorded_at, tz, true))}</bdi>` : "")}
${row("Oxygen (SpO₂)", c.last_spo2 ? `<span class="num">${esc(c.last_spo2.value)}</span> — <bdi>${esc(when(c.last_spo2.recorded_at, tz, false))}</bdi>` : "", "الأكسجين", c.last_spo2 ? `<span class="num">${esc(c.last_spo2.value)}</span> — <bdi>${esc(when(c.last_spo2.recorded_at, tz, true))}</bdi>` : "")}
${row("Temperature", c.last_temperature ? `<span class="num">${esc(c.last_temperature.value)}</span> — <bdi>${esc(when(c.last_temperature.recorded_at, tz, false))}</bdi>` : "", "الحرارة", c.last_temperature ? `<span class="num">${esc(c.last_temperature.value)}</span> — <bdi>${esc(when(c.last_temperature.recorded_at, tz, true))}</bdi>` : "")}
${row("Alertness", al ? `${esc(al[0])} — <bdi>${esc(when(c.last_alertness!.recorded_at, tz, false))}</bdi>` : "", "مستوى الوعي", al ? `${esc(al[1])} — <bdi>${esc(when(c.last_alertness!.recorded_at, tz, true))}</bdi>` : "")}
${row("F.A.S.T. stroke check", fastEn, "فحص الجلطة F.A.S.T.", fastAr, f && fastSigns ? "alarm" : "")}
${row("Last seen normal", f?.last_normal_at ? `<bdi>${esc(when(f.last_normal_at, tz, false))}</bdi>` : "", "آخر مرة كانت طبيعية", f?.last_normal_at ? `<bdi>${esc(when(f.last_normal_at, tz, true))}</bdi>` : "", "alarm")}
<h2><span>Call</span><span dir="rtl" lang="ar">اتصال</span></h2>
${contacts || '<p class="empty">No numbers recorded · لا توجد أرقام</p>'}
<p class="empty">As of <bdi>${esc(asOfEn)}</bdi> · حتى <bdi>${esc(asOfAr)}</bdi></p>
</main></body></html>`;
}

export function renderEmergencyMessage(kind: "not_found" | "too_many" | "unavailable"): string {
  const text = {
    not_found: ["This emergency card link is not valid. Ask the family for the current QR code.", "رابط بطاقة الطوارئ غير صالح. اطلب رمز QR الحالي من العائلة."],
    too_many: ["Too many requests. Wait a minute and try again.", "طلبات كثيرة. انتظر دقيقة وحاول مجدداً."],
    unavailable: ["The card can't be loaded right now. Try again.", "تعذر تحميل البطاقة الآن. حاول مجدداً."],
  }[kind];
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><title>Emergency medical card</title><style>${STYLE}</style></head><body><div class="top"><span>EMERGENCY MEDICAL CARD</span><span dir="rtl" lang="ar">بطاقة طبية للطوارئ</span></div><main><p class="v">${esc(text[0])}</p><p class="v" dir="rtl" lang="ar">${esc(text[1])}</p></main></body></html>`;
}
