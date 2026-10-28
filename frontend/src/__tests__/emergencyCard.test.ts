import { describe, expect, it } from "vitest";
import { renderEmergencyCard, renderEmergencyMessage, type EmergencyCard } from "@/lib/emergencyCard";

// Issue #19: the paramedics' card is plain HTML in both languages, with every value escaped.
const card: EmergencyCard = {
  patient: {
    name: "Amina <script>alert(1)</script>",
    dob: "1950-03-02",
    diagnosis_ar: "جلطة دماغية إقفارية",
    diagnosis_en: "Ischaemic stroke",
    stroke_date: "2026-09-01",
    allergies_ar: "البنسلين",
    allergies_en: "Penicillin",
    blood_type: "O+",
    baseline_bp: "130/80",
    notes: "",
  },
  medications: [{ name_ar: "إليكويس", name_en: "Apixaban", dosage: "5 mg", slots: ["morning", "evening"], times: ["08:00", "20:00"], blood_thinner: true }],
  last_bp: [{ systolic: 182, diastolic: 104, pulse: 88, recorded_at: "2026-10-21T10:00:00Z" }],
  last_alertness: { value: "drowsy", recorded_at: "2026-10-21T10:00:00Z" },
  last_spo2: { value: "87%", recorded_at: "2026-10-21T10:00:00Z" },
  last_temperature: { value: "38.4 °C", recorded_at: "2026-10-21T10:00:00Z" },
  last_fast: { face: true, arm: false, speech: true, recorded_at: "2026-10-21T10:05:00Z", last_normal_at: "2026-10-21T08:30:00Z" },
  contacts: { emergency_number: "997", doctor_name: "Dr. Saleh", doctor_phone: "+966500000000" },
  family: [{ name: "Fatima", phone: "+966500000001" }],
  generated_at: "2026-10-21T10:10:00Z",
  time_zone: "Asia/Riyadh",
};

describe("emergency card HTML", () => {
  const html = renderEmergencyCard(card, new Date("2026-10-21T12:00:00Z"));

  it("escapes values", () => {
    expect(html).not.toContain("<script>");
    expect(html).toContain("Amina &lt;script&gt;");
  });

  it("has both languages side by side", () => {
    for (const s of ["EMERGENCY MEDICAL CARD", "بطاقة طبية للطوارئ", "Medicines", "الأدوية", "Penicillin", "البنسلين", "Apixaban", "إليكويس"]) expect(html).toContain(s);
    expect(html).toContain('dir="rtl" lang="ar"');
  });

  it("flags blood thinners, crisis-level facts and last seen normal", () => {
    expect(html).toContain("BLOOD THINNER / ANTIPLATELET");
    expect(html).toContain("مميّع للدم");
    expect(html).toContain("182/104");
    expect(html).toContain("POSITIVE: face droop, speech");
    expect(html).toContain("Hard to wake");
    expect(html).toContain("87%");
    expect(html).toContain("38.4 °C");
    expect(html).toContain("Last seen normal");
    expect(html).toContain("Age 76");
  });

  it("shows times in the household time zone, with an as-of time", () => {
    expect(html).toContain("13:10"); // 10:10Z in Riyadh
    expect(html).toContain("As of");
  });

  it("has call links and no scripts at all", () => {
    expect(html).toContain('href="tel:997"');
    expect(html).toContain('href="tel:+966500000001"');
    expect(html).not.toMatch(/<script/i);
  });

  it("a bad link says so in both languages", () => {
    const m = renderEmergencyMessage("not_found");
    expect(m).toContain("not valid");
    expect(m).toContain("غير صالح");
  });
});

describe("emergency card: recent doses first (issue #56)", () => {
  const withDoses: EmergencyCard = {
    ...card,
    medications: [
      { ...card.medications[0], last_dose: { at: "2026-10-20T20:30:00Z", status: "taken", hours_ago: 8.25, vomited: false } },
      { name_ar: "أتورفاستاتين", name_en: "Atorvastatin", dosage: "20 mg", slots: ["bedtime"], blood_thinner: false },
    ],
    last_insulin: { at: "2026-10-21T08:10:00Z", units: 4, plan_name_ar: "نوفورابيد", plan_name_en: "NovoRapid", blood_sugar: 12.5 },
    prn_24h: [{ name_ar: "باراسيتامول", name_en: "Paracetamol", dosage: "1 g", count: 3, total_dose: "3 g", last_at: "2026-10-21T07:10:00Z" }],
    generated_at: "2026-10-21T04:45:00Z",
  };
  const html = renderEmergencyCard(withDoses, new Date("2026-10-21T05:00:00Z"));

  it("puts the blood thinner's last dose before anything else, with the household offset", () => {
    const recent = html.indexOf('data-testid="recent-doses"');
    expect(recent).toBeGreaterThan(-1);
    expect(recent).toBeLessThan(html.indexOf("<h1>"));
    expect(html).toContain("Blood thinner");
    expect(html).toContain("مميّع للدم");
    // 20:30 UTC is 23:30 in Riyadh.
    expect(html).toMatch(/last dose <bdi class="num">20 Oct, 23:30 UTC\+3<\/bdi> \(8 h 15 min ago\)/);
    expect(html).toContain("(قبل 8 س 15 د)");
  });

  it("shows the last insulin and as-needed doses of the last 24 h", () => {
    expect(html).toContain("Last insulin");
    expect(html).toContain("4 u</span> NovoRapid");
    expect(html).toMatch(/Paracetamol <span class="num">1 g<\/span> ×<span class="num">3<\/span> = <span class="num">3 g<\/span>/);
  });

  it("flags a partial or vomited dose, and says when no dose is recorded", () => {
    const partial = renderEmergencyCard({
      ...withDoses,
      medications: [{ ...card.medications[0], last_dose: { at: "2026-10-21T04:00:00Z", status: "partial", hours_ago: 0.75, vomited: true } }],
    });
    expect(partial).toContain('<span class="flag">partial</span>');
    expect(partial).toContain('<span class="flag">vomited</span>');
    expect(partial).toContain('<span class="flag">استفرغتها</span>');
    const none = renderEmergencyCard({ ...withDoses, medications: [card.medications[0]], last_insulin: undefined, prn_24h: [] });
    expect(none).toContain("no dose recorded in this app");
  });

  it("issue #64: do-not-crush medicines and a recent swallowing difficulty", () => {
    const html = renderEmergencyCard({
      ...card,
      medications: [{ ...withDoses.medications[1], crush: "do_not_crush" }],
      swallowing_difficulty: { at: "2026-10-20T08:00:00Z" },
    });
    expect(html).toContain('<span class="nocrush">DO NOT CRUSH</span>');
    expect(html).toContain('<span class="nocrush">لا تُطحن</span>');
    expect(html).toContain("Swallowing difficulty");
    expect(html).toContain("صعوبة في البلع");
    expect(html).toContain("recent-doses");
    const plain = renderEmergencyCard({ ...card, medications: [{ ...withDoses.medications[1], crush: "ok" }] });
    expect(plain).not.toContain('class="nocrush"');
    expect(plain).not.toContain("Swallowing difficulty");
  });

  it("no blood thinner, insulin or as-needed doses: no block", () => {
    const plain = renderEmergencyCard({ ...card, medications: [withDoses.medications[1]] });
    expect(plain).not.toContain("recent-doses");
  });
});

describe("emergency card: latest fall or seizure (issue #60)", () => {
  it("shows a recent fall with the head knock, in red, in both languages", () => {
    const html = renderEmergencyCard({
      ...card,
      generated_at: "2026-10-21T12:00:00Z",
      last_incident: { kind: "fall", at: "2026-10-21T10:00:00Z", hit_head: true, awake_normal: true, pain_or_bleeding: false, escalate: true },
    });
    expect(html).toMatch(/<div class="row alarm"><div class="en"><span class="k">Fall<\/span>/);
    expect(html).toContain("(2 h 0 min ago) — hit her head");
    expect(html).toContain("ضربت رأسها");
  });
  it("shows a seizure with how long it lasted", () => {
    const html = renderEmergencyCard({ ...card, last_incident: { kind: "seizure", at: "2026-10-21T09:10:00Z", duration_seconds: 125, escalate: false } });
    expect(html).toContain('lasted <span class="num">2:05</span> min');
  });
});
