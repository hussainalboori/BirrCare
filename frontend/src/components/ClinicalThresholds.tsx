"use client";

import React, { useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { canManageMedications } from "@/lib/permissions";
import { setBPLowThresholds } from "@/lib/clinical";

// Issue #13: the doctor's "low blood pressure" limit (default below 90/60). Admins only.
export function ClinicalThresholds() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const [sys, setSys] = useState("");
  const [dia, setDia] = useState("");
  const [msg, setMsg] = useState("");
  const admin = canManageMedications(currentCaregiver?.role);

  useEffect(() => {
    if (!admin) return;
    fetchApi<{ bp_low_systolic: number; bp_low_diastolic: number }>("/settings/clinical")
      .then((t) => {
        if (typeof t?.bp_low_systolic !== "number" || typeof t?.bp_low_diastolic !== "number") return;
        setSys(String(t.bp_low_systolic));
        setDia(String(t.bp_low_diastolic));
      })
      .catch(() => {});
  }, [admin]);

  if (!admin) return null;

  const save = async () => {
    setMsg("");
    try {
      const t = await fetchApi<{ bp_low_systolic: number; bp_low_diastolic: number }>("/settings/clinical", {
        method: "PUT",
        body: JSON.stringify({ bp_low_systolic: Number(sys), bp_low_diastolic: Number(dia) }),
      });
      setBPLowThresholds(t.bp_low_systolic, t.bp_low_diastolic);
      setMsg(ar ? "تم الحفظ" : "Saved");
    } catch {
      setMsg(ar ? "القيم المسموحة: الانقباضي 70–120، الانبساطي 40–80" : "Allowed: systolic 70–120, diastolic 40–80");
    }
  };

  return (
    <details className="mb-4 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <summary className="min-h-12 cursor-pointer text-sm font-bold text-slate-800 dark:text-slate-100">
        {ar ? "حد الضغط المنخفض حسب الطبيب (للمسؤول)" : "Doctor's low blood pressure limit (admin)"}
      </summary>
      <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">
        {ar ? "تُعد القراءة منخفضة إذا كان الانقباضي أقل من أو الانبساطي أقل من:" : "A reading is low when systolic is below, or diastolic is below:"}
      </p>
      <div className="mt-2 flex flex-wrap items-end gap-2">
        <label className="text-xs font-semibold text-slate-600 dark:text-slate-300">
          {ar ? "الانقباضي" : "Systolic"}
          <input type="number" inputMode="numeric" value={sys} onChange={(e) => setSys(e.target.value)} className="mt-1 block w-24 rounded-xl border border-slate-200 px-3 py-2 text-base dark:border-slate-700 dark:bg-slate-800" />
        </label>
        <label className="text-xs font-semibold text-slate-600 dark:text-slate-300">
          {ar ? "الانبساطي" : "Diastolic"}
          <input type="number" inputMode="numeric" value={dia} onChange={(e) => setDia(e.target.value)} className="mt-1 block w-24 rounded-xl border border-slate-200 px-3 py-2 text-base dark:border-slate-700 dark:bg-slate-800" />
        </label>
        <button type="button" onClick={save} className="min-h-12 rounded-xl bg-emerald-700 px-4 text-base font-bold text-white">
          {ar ? "حفظ" : "Save"}
        </button>
        {msg && <span className="text-sm font-semibold text-slate-700 dark:text-slate-200">{msg}</span>}
      </div>
    </details>
  );
}
