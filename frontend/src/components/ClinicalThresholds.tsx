"use client";

import React, { useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { canManageMedications } from "@/lib/permissions";
import { setBPLowThresholds, setOxygenFeverThresholds } from "@/lib/clinical";

// The doctor's limits, admins only: low blood pressure (issue #13, default below
// 90/60) and oxygen / fever (issue #22, default SpO2 92% re-check, 88% alert,
// fever from 38.0 °C).
interface Settings {
  bp_low_systolic: number;
  bp_low_diastolic: number;
  spo2_warn: number;
  spo2_critical: number;
  fever_c: number;
}

export function ClinicalThresholds() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const [v, setV] = useState<Record<keyof Settings, string>>({ bp_low_systolic: "", bp_low_diastolic: "", spo2_warn: "", spo2_critical: "", fever_c: "" });
  const [msg, setMsg] = useState("");
  const admin = canManageMedications(currentCaregiver?.role);

  useEffect(() => {
    if (!admin) return;
    fetchApi<Settings>("/settings/clinical")
      .then((t) => {
        if (typeof t?.bp_low_systolic !== "number") return;
        setV({
          bp_low_systolic: String(t.bp_low_systolic),
          bp_low_diastolic: String(t.bp_low_diastolic),
          spo2_warn: String(t.spo2_warn ?? 92),
          spo2_critical: String(t.spo2_critical ?? 88),
          fever_c: String(t.fever_c ?? 38),
        });
      })
      .catch(() => {});
  }, [admin]);

  if (!admin) return null;

  const save = async () => {
    setMsg("");
    try {
      const t = await fetchApi<Settings>("/settings/clinical", {
        method: "PUT",
        body: JSON.stringify(Object.fromEntries(Object.entries(v).map(([k, x]) => [k, Number(x)]))),
      });
      setBPLowThresholds(t.bp_low_systolic, t.bp_low_diastolic);
      setOxygenFeverThresholds(t.spo2_warn, t.spo2_critical, t.fever_c);
      setMsg(ar ? "تم الحفظ" : "Saved");
    } catch {
      setMsg(
        ar
          ? "القيم المسموحة: الانقباضي 70–120، الانبساطي 40–80، أكسجين الخطر 80–94 والتنبيه أعلى منه حتى 96، الحمّى 37.5–39"
          : "Allowed: systolic 70–120, diastolic 40–80, oxygen alert 80–94 with re-check above it up to 96, fever 37.5–39"
      );
    }
  };

  const field = (key: keyof Settings, label: string, step = "1") => (
    <label className="text-xs font-semibold text-slate-600 dark:text-slate-300">
      {label}
      <input
        type="number"
        inputMode="decimal"
        step={step}
        value={v[key]}
        onChange={(e) => setV((p) => ({ ...p, [key]: e.target.value }))}
        className="mt-1 block w-24 rounded-xl border border-slate-200 px-3 py-2 text-base dark:border-slate-700 dark:bg-slate-800"
      />
    </label>
  );

  return (
    <details className="mb-4 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <summary className="min-h-12 cursor-pointer text-sm font-bold text-slate-800 dark:text-slate-100">
        {ar ? "حدود الطبيب: الضغط المنخفض والأكسجين والحمّى (للمسؤول)" : "Doctor's limits: low BP, oxygen, fever (admin)"}
      </summary>
      <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">
        {ar ? "الضغط منخفض إذا كان الانقباضي أقل من أو الانبساطي أقل من:" : "BP is low when systolic is below, or diastolic is below:"}
      </p>
      <div className="mt-2 flex flex-wrap items-end gap-2">
        {field("bp_low_systolic", ar ? "الانقباضي" : "Systolic")}
        {field("bp_low_diastolic", ar ? "الانبساطي" : "Diastolic")}
      </div>
      <p className="mt-3 text-sm text-slate-600 dark:text-slate-300">
        {ar ? "الأكسجين (SpO₂ ٪): إعادة القياس والاتصال بالطبيب عند أو تحت، وتنبيه العائلة عند أو تحت:" : "Oxygen (SpO₂ %): re-check and call the doctor at or below, alert the family at or below:"}
      </p>
      <div className="mt-2 flex flex-wrap items-end gap-2">
        {field("spo2_warn", ar ? "أعد القياس" : "Re-check")}
        {field("spo2_critical", ar ? "تنبيه" : "Alert")}
        {field("fever_c", ar ? "حمّى من (°م)" : "Fever from (°C)", "0.1")}
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button type="button" onClick={save} className="min-h-12 rounded-xl bg-emerald-700 px-4 text-base font-bold text-white">
          {ar ? "حفظ" : "Save"}
        </button>
        {msg && <span className="text-sm font-semibold text-slate-700 dark:text-slate-200">{msg}</span>}
      </div>
    </details>
  );
}
