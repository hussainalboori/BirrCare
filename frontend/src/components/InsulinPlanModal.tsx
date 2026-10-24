"use client";

import React, { useState } from "react";
import { InsulinPlan } from "@/lib/types";
import { useLanguage } from "@/context/LanguageContext";
import { fetchApi } from "@/lib/api";
import { Ltr } from "./Ltr";
import { X, Plus, Trash2, Loader2, AlertTriangle, Pencil } from "lucide-react";

interface RowDraft {
  min: string;
  max: string;
  units: string;
}

interface Draft {
  id?: string;
  name_ar: string;
  name_en: string;
  kind: "scale" | "fixed";
  fixed_units: string;
  min_hours_between: string;
  prescribed_by: string;
  notes_ar: string;
  notes_en: string;
  is_active: boolean;
  rows: RowDraft[];
}

// Deliberately blank: any numbers pre-filled here could be mistaken for a recommendation.
const emptyDraft = (): Draft => ({
  name_ar: "",
  name_en: "",
  kind: "scale",
  fixed_units: "",
  min_hours_between: "4",
  prescribed_by: "",
  notes_ar: "",
  notes_en: "",
  is_active: true,
  rows: [{ min: "0", max: "", units: "" }],
});

const fromPlan = (p: InsulinPlan): Draft => ({
  id: p.id,
  name_ar: p.name_ar,
  name_en: p.name_en,
  kind: p.kind,
  fixed_units: p.fixed_units != null ? String(p.fixed_units) : "",
  min_hours_between: String(p.min_hours_between),
  prescribed_by: p.prescribed_by || "",
  notes_ar: p.notes_ar || "",
  notes_en: p.notes_en || "",
  is_active: p.is_active,
  rows: p.scale.length ? p.scale.map((r) => ({ min: String(r.min), max: String(r.max), units: String(r.units) })) : emptyDraft().rows,
});

const num = (s: string) => (s.trim() === "" ? NaN : Number(s.replace(",", ".")));

interface Props {
  plans: InsulinPlan[];
  onClose: () => void;
  onSaved: () => void;
}

export function InsulinPlanModal({ plans, onClose, onSaved }: Props) {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const [draft, setDraft] = useState<Draft | null>(plans.length === 0 ? emptyDraft() : null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const set = (patch: Partial<Draft>) => setDraft((d) => (d ? { ...d, ...patch } : d));

  // Each row starts where the previous one ends, so the scale can never have gaps.
  const setRow = (i: number, patch: Partial<RowDraft>) =>
    setDraft((d) => {
      if (!d) return d;
      const rows = d.rows.map((r, j) => (j === i ? { ...r, ...patch } : r));
      for (let j = 1; j < rows.length; j++) rows[j] = { ...rows[j], min: rows[j - 1].max };
      return { ...d, rows };
    });

  const addRow = () =>
    setDraft((d) => (d ? { ...d, rows: [...d.rows, { min: d.rows[d.rows.length - 1]?.max || "", max: "", units: "" }] } : d));

  const removeRow = () => setDraft((d) => (d && d.rows.length > 1 ? { ...d, rows: d.rows.slice(0, -1) } : d));

  const save = async () => {
    if (!draft) return;
    setError(null);
    const body = {
      name_ar: draft.name_ar,
      name_en: draft.name_en,
      kind: draft.kind,
      fixed_units: draft.kind === "fixed" ? num(draft.fixed_units) : undefined,
      scale: draft.kind === "scale" ? draft.rows.map((r) => ({ min: num(r.min), max: num(r.max), units: num(r.units) })) : [],
      min_hours_between: num(draft.min_hours_between),
      prescribed_by: draft.prescribed_by.trim(),
      notes_ar: draft.notes_ar.trim(),
      notes_en: draft.notes_en.trim(),
      is_active: draft.is_active,
    };
    if (body.scale.some((r) => Number.isNaN(r.min) || Number.isNaN(r.max) || Number.isNaN(r.units))) {
      setError(ar ? "أكمل كل خانات الجدول" : "Fill in every cell of the scale");
      return;
    }
    setSaving(true);
    try {
      await fetchApi(draft.id ? `/insulin/plans/${draft.id}` : "/insulin/plans", {
        method: draft.id ? "PUT" : "POST",
        body: JSON.stringify(body),
      });
      onSaved();
      setDraft(null);
      if (!draft.id && plans.length === 0) onClose();
    } catch (err: any) {
      setError(err?.data?.message || err?.message || (ar ? "تعذر الحفظ" : "Could not save"));
    } finally {
      setSaving(false);
    }
  };


  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-900/60 p-0 backdrop-blur-xs sm:items-center sm:p-4" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        className="max-h-[92dvh] w-full max-w-lg overflow-y-auto rounded-t-3xl bg-white p-5 shadow-2xl dark:bg-slate-900 sm:rounded-3xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-black text-slate-900 dark:text-white">{ar ? "خطة الإنسولين من الطبيب" : "Doctor's insulin plan"}</h2>
          <button type="button" onClick={onClose} aria-label={ar ? "إغلاق" : "Close"} className="flex min-h-12 min-w-12 items-center justify-center rounded-full text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="mb-4 flex items-start gap-2 rounded-2xl border border-amber-300 bg-amber-50 p-3 text-xs font-semibold text-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          <span>
            {ar
              ? "انسخ الأرقام كما كتبها الطبيب تماماً. التطبيق لا يقرر الجرعة، بل يعرض جدول الطبيب فقط. عند تغيير الطبيب للجرعات عدّل الجدول هنا."
              : "Copy the numbers exactly as the doctor wrote them. The app never decides a dose; it only shows the doctor's table. When the doctor changes the doses, update it here."}
          </span>
        </div>

        {!draft ? (
          <div className="space-y-2">
            {plans.map((p) => (
              <button
                key={p.id}
                type="button"
                onClick={() => setDraft(fromPlan(p))}
                className="flex min-h-14 w-full items-center justify-between rounded-2xl border border-slate-200 px-4 text-start dark:border-slate-700"
              >
                <span className={`font-bold ${p.is_active ? "text-slate-900 dark:text-white" : "text-slate-400 line-through"}`}>
                  <bdi>{ar ? p.name_ar : p.name_en}</bdi>
                </span>
                <Pencil className="h-4 w-4 text-slate-400" />
              </button>
            ))}
            <button
              type="button"
              onClick={() => setDraft(emptyDraft())}
              className="flex min-h-14 w-full items-center justify-center gap-2 rounded-2xl border-2 border-dashed border-emerald-400 font-bold text-emerald-700 dark:text-emerald-300"
            >
              <Plus className="h-5 w-5" /> {ar ? "إضافة إنسولين" : "Add insulin"}
            </button>
          </div>
        ) : (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-2">
              <Field value={draft.name_ar} onChange={(e) => set({ name_ar: e.target.value })} placeholder="الاسم (مثل نوفورابيد)" aria-label="الاسم بالعربية" dir="rtl" />
              <Field value={draft.name_en} onChange={(e) => set({ name_en: e.target.value })} placeholder="Name (e.g. NovoRapid)" aria-label="English name" dir="ltr" />
            </div>

            <div className="grid grid-cols-2 gap-2" role="radiogroup">
              {(["scale", "fixed"] as const).map((k) => (
                <button
                  key={k}
                  type="button"
                  role="radio"
                  aria-checked={draft.kind === k}
                  onClick={() => set({ kind: k })}
                  className={`min-h-14 rounded-xl border px-2 text-sm font-bold ${
                    draft.kind === k ? "border-emerald-600 bg-emerald-50 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300" : "border-slate-200 text-slate-600 dark:border-slate-700 dark:text-slate-300"
                  }`}
                >
                  {k === "scale" ? (ar ? "حسب قراءة السكر (جدول)" : "By sugar reading (scale)") : ar ? "جرعة ثابتة" : "Fixed dose"}
                </button>
              ))}
            </div>

            {draft.kind === "scale" ? (
              <div>
                <div className="mb-1 grid grid-cols-3 gap-2 text-center text-xs font-semibold text-slate-500">
                  <span>{ar ? "من" : "From"} <Ltr>mmol/L</Ltr></span>
                  <span>{ar ? "إلى أقل من" : "Up to (not incl.)"}</span>
                  <span>{ar ? "الوحدات" : "Units"}</span>
                </div>
                <div className="space-y-2">
                  {draft.rows.map((r, i) => (
                    <div key={i} className="grid grid-cols-3 gap-2">
                      {(["min", "max", "units"] as const).map((k) => (
                        <input
                          key={k}
                          dir="ltr"
                          className="min-h-12 w-full rounded-lg border border-slate-200 bg-white px-2 text-center text-base font-bold text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white disabled:bg-slate-100 disabled:text-slate-500 dark:disabled:bg-slate-900"
                          inputMode="decimal"
                          value={r[k]}
                          disabled={k === "min" && i > 0}
                          onChange={(e) => setRow(i, { [k]: e.target.value })}
                          aria-label={`row ${i + 1} ${k}`}
                        />
                      ))}
                    </div>
                  ))}
                </div>
                <div className="mt-2 grid grid-cols-2 gap-2">
                  <button type="button" onClick={addRow} className="flex min-h-12 items-center justify-center gap-1 rounded-xl border border-slate-200 text-sm font-semibold dark:border-slate-700">
                    <Plus className="h-4 w-4" /> {ar ? "صف" : "Row"}
                  </button>
                  <button type="button" onClick={removeRow} disabled={draft.rows.length < 2} className="flex min-h-12 items-center justify-center gap-1 rounded-xl border border-slate-200 text-sm font-semibold disabled:opacity-40 dark:border-slate-700">
                    <Trash2 className="h-4 w-4" /> {ar ? "حذف آخر صف" : "Remove last"}
                  </button>
                </div>
                <p className="mt-2 text-xs text-slate-500">
                  {ar
                    ? "أي قراءة خارج الجدول ستطلب الاتصال بالطبيب. القراءات تحت 3.9 تمنع الإنسولين دائماً."
                    : "Any reading outside the table tells the caregiver to call the doctor. Readings under 3.9 always block insulin."}
                </p>
              </div>
            ) : (
              <label className="block space-y-1 text-sm font-semibold text-slate-700 dark:text-slate-300">
                {ar ? "عدد الوحدات" : "Units"}
                <Field inputMode="decimal" dir="ltr" value={draft.fixed_units} onChange={(e) => set({ fixed_units: e.target.value })} />
              </label>
            )}

            <label className="block space-y-1 text-sm font-semibold text-slate-700 dark:text-slate-300">
              {ar ? "أقل مدة بين إبرتين (ساعات)" : "Minimum hours between injections"}
              <Field inputMode="decimal" dir="ltr" value={draft.min_hours_between} onChange={(e) => set({ min_hours_between: e.target.value })} />
            </label>

            <Field value={draft.prescribed_by} onChange={(e) => set({ prescribed_by: e.target.value })} placeholder={ar ? "اسم الطبيب" : "Doctor's name"} aria-label={ar ? "اسم الطبيب" : "Doctor's name"} />
            <Field value={draft.notes_ar} onChange={(e) => set({ notes_ar: e.target.value })} placeholder="تعليمات (مثل: قبل الأكل بـ 15 دقيقة)" aria-label="تعليمات" dir="rtl" />
            <Field value={draft.notes_en} onChange={(e) => set({ notes_en: e.target.value })} placeholder="Instructions (e.g. 15 min before meals)" aria-label="Instructions" dir="ltr" />

            {draft.id && (
              <label className="flex min-h-12 items-center gap-2 text-sm font-semibold text-slate-700 dark:text-slate-300">
                <input type="checkbox" className="h-5 w-5" checked={draft.is_active} onChange={(e) => set({ is_active: e.target.checked })} />
                {ar ? "مستخدم حالياً" : "Currently in use"}
              </label>
            )}

            {error && (
              <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm font-semibold text-rose-700 dark:bg-rose-950/40 dark:text-rose-300">
                {error}
              </p>
            )}

            <div className="grid grid-cols-2 gap-2">
              <button type="button" onClick={() => (plans.length ? setDraft(null) : onClose())} className="min-h-12 rounded-xl text-sm font-semibold text-slate-600 dark:text-slate-300">
                {ar ? "إلغاء" : "Cancel"}
              </button>
              <button type="button" onClick={save} disabled={saving} className="flex min-h-12 items-center justify-center gap-2 rounded-xl bg-emerald-600 text-sm font-bold text-white disabled:opacity-50">
                {saving && <Loader2 className="h-4 w-4 animate-spin" />}
                {ar ? "حفظ" : "Save"}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function Field(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className="min-h-12 w-full rounded-xl border border-slate-200 bg-white px-3 text-base text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
    />
  );
}
