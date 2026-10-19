"use client";

import React, { useCallback, useEffect, useState } from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { InsulinPlanCard } from "@/components/InsulinPlanCard";
import { InsulinPlanModal } from "@/components/InsulinPlanModal";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLiveSync } from "@/hooks/useLiveSync";
import { fetchApi } from "@/lib/api";
import { canManageMedications } from "@/lib/permissions";
import { InsulinPlan, InsulinPlanStatus } from "@/lib/types";
import { Syringe, Settings2 } from "lucide-react";

export default function InsulinPage() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const [statuses, setStatuses] = useState<InsulinPlanStatus[]>([]);
  const [allPlans, setAllPlans] = useState<InsulinPlan[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(false);
  const isAdmin = canManageMedications(currentCaregiver?.role);

  const load = useCallback(async () => {
    try {
      setStatuses((await fetchApi<InsulinPlanStatus[]>("/insulin/status")) || []);
    } catch (err) {
      console.warn("Failed fetching insulin status:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  const openEditor = async () => {
    try {
      setAllPlans((await fetchApi<InsulinPlan[]>("/insulin/plans?all=1")) || []);
      setEditing(true);
    } catch (err) {
      console.warn("Failed fetching insulin plans:", err);
    }
  };

  useEffect(() => {
    load();
  }, [load]);

  const { status: connectionStatus } = useLiveSync({
    onEvent: (e) => {
      if (e.type === "INSULIN_LOGGED" || e.type === "INSULIN_PLANS_UPDATED") load();
    },
    onResync: load,
  });

  return (
    <div className="min-h-screen">
      <CaregiverHeader connectionStatus={connectionStatus} />

      <main className="mx-auto max-w-3xl px-4 py-5 sm:px-6 lg:max-w-none lg:px-8">
        <div className="mb-5 flex items-center justify-between gap-3">
          <div>
            <h1 className="flex items-center gap-2 text-xl font-black text-slate-900 dark:text-white sm:text-2xl">
              {ar ? "الإنسولين" : "Insulin"}
              <Syringe className="h-5 w-5 text-blue-600" />
            </h1>
            <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
              {ar ? "قِس السكر، اتبع جدول الطبيب، وسجّل الإبرة" : "Measure sugar, follow the doctor's scale, record the injection"}
            </p>
          </div>
          {isAdmin && (
            <button
              type="button"
              onClick={openEditor}
              className="flex min-h-12 shrink-0 items-center gap-1.5 rounded-xl border border-slate-200 px-3 text-xs font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200"
            >
              <Settings2 className="h-4 w-4" />
              {ar ? "خطة الطبيب" : "Doctor's plan"}
            </button>
          )}
        </div>

        {loading ? (
          <div className="h-64 w-full animate-pulse rounded-3xl bg-slate-200 dark:bg-slate-800" />
        ) : statuses.length === 0 ? (
          <div className="rounded-3xl border border-dashed border-slate-300 p-8 text-center dark:border-slate-700">
            <Syringe className="mx-auto mb-2 h-8 w-8 text-slate-400" />
            <p className="mb-4 text-sm font-semibold text-slate-600 dark:text-slate-300">
              {isAdmin
                ? ar ? "أدخل جدول الإنسولين كما كتبه الطبيب" : "Enter the insulin scale exactly as the doctor wrote it"
                : ar ? "اطلب من المشرف إدخال جدول الإنسولين من الطبيب" : "Ask an admin to enter the doctor's insulin scale"}
            </p>
            {isAdmin && (
              <button type="button" onClick={openEditor} className="min-h-12 rounded-xl bg-emerald-600 px-5 text-sm font-bold text-white">
                {ar ? "إدخال خطة الطبيب" : "Enter doctor's plan"}
              </button>
            )}
          </div>
        ) : (
          <div className="grid items-start gap-4 lg:grid-cols-2">
            {statuses.map((s) => (
              <InsulinPlanCard key={s.plan.id} status={s} onLogged={load} />
            ))}
          </div>
        )}
      </main>

      {editing && <InsulinPlanModal plans={allPlans} onClose={() => setEditing(false)} onSaved={() => { load(); openEditor(); }} />}
    </div>
  );
}
