"use client";

import React, { useCallback, useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { canTakeActions } from "@/lib/permissions";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import type { SSEEvent, StockStatus } from "@/lib/types";
import { Ltr } from "./Ltr";
import { daysLeftText } from "@/lib/stockText";
import { PackageOpen, Delete, X, Loader2 } from "lucide-react";

/**
 * Issue #20: an amber card on Home for each medicine about to run out, with a
 * big "Refilled" button that asks how many pills are in the new box.
 */
export function LowStockCards() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const [stock, setStock] = useState<StockStatus[]>([]);
  const [refilling, setRefilling] = useState<StockStatus | null>(null);

  const load = useCallback(async () => {
    const list = await fetchApi<StockStatus[]>("/medications/stock").catch(() => null);
    if (Array.isArray(list)) setStock(list);
  }, []);

  useEffect(() => {
    if (!currentCaregiver) return;
    load();
    const onEvent = (e: Event) => {
      const type = (e as CustomEvent<SSEEvent>).detail?.type;
      if (type === "STOCK_UPDATED" || type === "MEDICATIONS_UPDATED") load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    window.addEventListener(RESYNC_EVENT, load);
    return () => {
      window.removeEventListener("birrcare:sse_event", onEvent);
      window.removeEventListener(RESYNC_EVENT, load);
    };
  }, [currentCaregiver, load]);

  const low = stock.filter((s) => s.low);
  if (!low.length && !refilling) return null;
  const canRefill = canTakeActions(currentCaregiver?.role);

  return (
    <>
      <div className="mb-4 space-y-2">
        {low.map((s) => {
          const name = ar ? s.name_ar : s.name_en;
          return (
            <div
              key={s.medication_id}
              data-testid="low-stock"
              role="status"
              className="flex flex-wrap items-center gap-3 rounded-2xl border-2 border-amber-400 bg-amber-50 p-3 dark:border-amber-700 dark:bg-amber-950/40"
            >
              <PackageOpen className="h-7 w-7 shrink-0 text-amber-700 dark:text-amber-300" />
              <div className="min-w-0 flex-1">
                <p className="text-base font-black text-amber-950 dark:text-amber-100">
                  <bdi>{name}</bdi>: {daysLeftText(s.days_left, ar)}
                </p>
                <p className="text-sm font-semibold text-amber-900 dark:text-amber-200">
                  {ar ? "المتبقي" : "Left"}: <Ltr>{s.stock_units}</Ltr> {ar ? "حبة" : "pills"}
                  {s.blood_thinner && <span className="ms-2 rounded-md bg-rose-700 px-1.5 py-0.5 text-xs font-black text-white">{ar ? "مميّع للدم" : "Blood thinner"}</span>}
                </p>
              </div>
              {canRefill && (
                <button
                  type="button"
                  onClick={() => setRefilling(s)}
                  className="min-h-14 w-full rounded-xl bg-amber-600 px-5 text-base font-black text-white active:scale-95 sm:w-auto"
                >
                  {ar ? "اشتريت علبة جديدة" : "Refilled"}
                </button>
              )}
            </div>
          );
        })}
      </div>
      {refilling && <RefillPad med={refilling} onClose={() => setRefilling(null)} onSaved={load} />}
    </>
  );
}

function RefillPad({ med, onClose, onSaved }: { med: StockStatus; onClose: () => void; onSaved: () => void }) {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const [units, setUnits] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const name = ar ? med.name_ar : med.name_en;

  const press = (d: string) => setUnits((u) => (u.length >= 4 ? u : u === "0" ? d : u + d));
  const save = async () => {
    const n = Number(units);
    if (!n) return;
    setSaving(true);
    setError("");
    try {
      await fetchApi(`/medications/${encodeURIComponent(med.medication_id)}/stock`, { method: "POST", body: JSON.stringify({ action: "refill", units: n }) });
      onSaved();
      onClose();
    } catch (err) {
      setError((err as Error).message || (ar ? "تعذر الحفظ" : "Could not save"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-900/60 sm:items-center" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={ar ? "علبة جديدة" : "New box"}
        className="w-full max-w-sm rounded-t-3xl bg-white p-5 pb-[calc(1.25rem+env(safe-area-inset-bottom))] shadow-2xl dark:bg-slate-900 sm:rounded-3xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-start justify-between gap-2">
          <p className="text-lg font-black text-slate-900 dark:text-white">
            {ar ? "كم حبة في العلبة الجديدة؟" : "How many pills in the new box?"}
            <span className="block text-sm font-semibold text-slate-500">
              <bdi>{name}</bdi> · {ar ? "تُضاف إلى" : "added to the"} <Ltr>{med.stock_units}</Ltr> {ar ? "المتبقية" : "left"}
            </span>
          </p>
          <button type="button" onClick={onClose} aria-label={ar ? "إلغاء" : "Cancel"} className="flex min-h-12 min-w-12 items-center justify-center rounded-full text-slate-400">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p data-testid="refill-units" className="mb-3 min-h-16 rounded-2xl bg-slate-100 py-3 text-center text-4xl font-black tabular-nums text-slate-900 dark:bg-slate-800 dark:text-white" dir="ltr">
          {units || "0"}
        </p>
        <div className="grid grid-cols-3 gap-2" dir="ltr">
          {["1", "2", "3", "4", "5", "6", "7", "8", "9"].map((d) => (
            <button key={d} type="button" onClick={() => press(d)} className="min-h-16 rounded-2xl bg-slate-100 text-2xl font-black text-slate-900 active:scale-95 dark:bg-slate-800 dark:text-white">
              {d}
            </button>
          ))}
          <button type="button" onClick={() => setUnits((u) => u.slice(0, -1))} className="flex min-h-16 flex-col items-center justify-center gap-0.5 rounded-2xl bg-slate-100 text-sm font-bold text-slate-700 dark:bg-slate-800 dark:text-slate-200">
            <Delete className="h-6 w-6" />
            {ar ? "حذف" : "Delete"}
          </button>
          <button type="button" onClick={() => press("0")} className="min-h-16 rounded-2xl bg-slate-100 text-2xl font-black text-slate-900 active:scale-95 dark:bg-slate-800 dark:text-white">
            0
          </button>
          <button
            type="button"
            onClick={save}
            disabled={!Number(units) || saving}
            className="flex min-h-16 items-center justify-center rounded-2xl bg-emerald-600 text-lg font-black text-white disabled:opacity-40"
          >
            {saving ? <Loader2 className="h-6 w-6 animate-spin" /> : ar ? "حفظ" : "Save"}
          </button>
        </div>
        {error && <p role="alert" className="mt-3 text-sm font-bold text-rose-700">{error}</p>}
      </div>
    </div>
  );
}
