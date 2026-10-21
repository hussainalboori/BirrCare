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
