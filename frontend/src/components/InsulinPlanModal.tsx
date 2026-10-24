"use client";

import React, { useState } from "react";
import { InsulinPlan } from "@/lib/types";
import { useLanguage } from "@/context/LanguageContext";
import { fetchApi } from "@/lib/api";
import { Ltr } from "./Ltr";
import { X, Plus, Trash2, Loader2, AlertTriangle, Pencil, Clock, Check } from "lucide-react";
import { autoIntervalHours } from "@/lib/clinical";
import { parseNumberInput } from "@/lib/numbers";

interface RowDraft {
  min: string;
  max: string;
  units: string;
}

const ALL_SLOTS = [
  { id: "morning", labelAr: "الصباح", labelEn: "Morning", defaultTime: "08:00" },
  { id: "afternoon", labelAr: "الظهيرة", labelEn: "Afternoon", defaultTime: "14:00" },
  { id: "evening", labelAr: "المساء", labelEn: "Evening", defaultTime: "20:00" },
  { id: "bedtime", labelAr: "قبل النوم", labelEn: "Bedtime", defaultTime: "22:30" },
  { id: "night", labelAr: "الليل", labelEn: "Night", defaultTime: "02:00" },
];

interface Draft {
  id?: string;
  name_ar: string;
  name_en: string;
  kind: "scale" | "fixed";
  fixed_units: string;
  min_hours_between: string;
  frequency_slots: string[];
  slot_times: Record<string, string>;
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
  min_hours_between: "",
  frequency_slots: ["morning"],
  slot_times: { morning: "08:00", afternoon: "14:00", evening: "20:00", bedtime: "22:30", night: "02:00" },
  prescribed_by: "",
  notes_ar: "",
  notes_en: "",
  is_active: true,
  rows: [{ min: "0", max: "", units: "" }],
});

const fromPlan = (p: InsulinPlan): Draft => {
  const times: Record<string, string> = {};
  ALL_SLOTS.forEach((s) => {
    times[s.id] = p.slot_times?.[s.id] || s.defaultTime;
  });
  return {
    id: p.id,
    name_ar: p.name_ar,
    name_en: p.name_en,
    kind: p.kind,
    fixed_units: p.fixed_units != null ? String(p.fixed_units) : "",
    min_hours_between: p.min_hours_between != null ? String(p.min_hours_between) : "",
    frequency_slots: p.frequency_slots && p.frequency_slots.length > 0 ? p.frequency_slots : [],
    slot_times: times,
    prescribed_by: p.prescribed_by || "",
    notes_ar: p.notes_ar || "",
    notes_en: p.notes_en || "",
    is_active: p.is_active,
    rows: p.scale.length ? p.scale.map((r) => ({ min: String(r.min), max: String(r.max), units: String(r.units) })) : emptyDraft().rows,
  };
};

const num = (s: string) => parseNumberInput(s);

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
  const [planToDelete, setPlanToDelete] = useState<InsulinPlan | Draft | null>(null);
  const [deleting, setDeleting] = useState(false);

  const handleConfirmDelete = async () => {
    if (!planToDelete || !planToDelete.id) return;
    setDeleting(true);
    setError(null);
    try {
      await fetchApi(`/insulin/plans/${planToDelete.id}`, {
        method: "DELETE",
      });
      setPlanToDelete(null);
      setDraft(null);
      onSaved();
    } catch (err: any) {
      setError(err?.data?.message || err?.message || (ar ? "تعذر حذف الخطة" : "Failed to delete plan"));
      setPlanToDelete(null);
    } finally {
      setDeleting(false);
    }
  };

  const set = (patch: Partial<Draft>) => setDraft((d) => (d ? { ...d, ...patch } : d));

  const setRow = (i: number, patch: Partial<RowDraft>) =>
    setDraft((d) => {
      if (!d) return d;
      const rows = d.rows.map((r, j) => (j === i ? { ...r, ...patch } : r));
      return { ...d, rows };
    });

  const addRow = () =>
    setDraft((d) => (d ? { ...d, rows: [...d.rows, { min: d.rows[d.rows.length - 1]?.max || "", max: "", units: "" }] } : d));

  const removeRow = () => setDraft((d) => (d && d.rows.length > 1 ? { ...d, rows: d.rows.slice(0, -1) } : d));

  const handleToggleSlot = (slotId: string) => {
    setDraft((d) => {
      if (!d) return d;
      const current = d.frequency_slots || [];
      const updated = current.includes(slotId) ? current.filter((s) => s !== slotId) : [...current, slotId];
      return { ...d, frequency_slots: updated };
    });
  };

  const handleSlotTimeChange = (slotId: string, time: string) => {
    setDraft((d) => {
      if (!d) return d;
      return { ...d, slot_times: { ...d.slot_times, [slotId]: time } };
    });
  };

  const save = async () => {
    if (!draft) return;
    setError(null);
    const autoGap = draft.frequency_slots.length > 0
      ? autoIntervalHours(draft.frequency_slots, draft.slot_times)
      : 4;
    const gap = draft.min_hours_between.trim() !== "" ? num(draft.min_hours_between) : autoGap;
    const body = {
      name_ar: draft.name_ar,
      name_en: draft.name_en,
      kind: draft.kind,
      fixed_units: draft.kind === "fixed" ? num(draft.fixed_units) : undefined,
      scale: draft.kind === "scale" ? draft.rows.map((r) => ({ min: num(r.min), max: num(r.max), units: num(r.units) })) : [],
      frequency_slots: draft.frequency_slots,
      slot_times: draft.slot_times,
      min_hours_between: gap,
      prescribed_by: draft.prescribed_by.trim(),
      notes_ar: draft.notes_ar.trim(),
      notes_en: draft.notes_en.trim(),
      is_active: draft.is_active,
    };
    if (body.scale.some((r) => Number.isNaN(r.min) || Number.isNaN(r.max) || Number.isNaN(r.units))) {
      setError(ar ? "أكمل كل خانات الجدول" : "Fill in every cell of the scale");
      return;
    }
    if (draft.kind === "scale") {
      for (let i = 0; i < body.scale.length; i++) {
        const r = body.scale[i];
        if (r.min < 0 || r.max > 45 || r.min >= r.max) {
          setError(
            ar
              ? `الصف ${i + 1}: قيمة "من" (${r.min}) يجب أن تكون أقل من "إلى" (${r.max})`
              : `Row ${i + 1}: 'from' (${r.min}) must be less than 'to' (${r.max})`
          );
          return;
        }
        if (i > 0 && Math.abs(body.scale[i - 1].max - r.min) > 1e-9) {
          setError(
            ar
              ? `الصف ${i + 1}: يجب أن يبدأ من نهاية الصف السابق (${body.scale[i - 1].max}) لضمان عدم وجود فجوات في الجدول`
              : `Row ${i + 1}: must start where row ${i} ends (${body.scale[i - 1].max}) to avoid gaps in the scale`
          );
          return;
        }
      }
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
              <div
                key={p.id}
                className="flex min-h-14 w-full items-center justify-between rounded-2xl border border-slate-200 p-3 text-start dark:border-slate-700 hover:border-slate-300 dark:hover:border-slate-600 transition-colors"
              >
                <button
                  type="button"
                  onClick={() => setDraft(fromPlan(p))}
                  className="flex flex-col flex-1 min-w-0 text-start"
                >
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className={`font-bold text-sm ${p.is_active ? "text-slate-900 dark:text-white" : "text-slate-400 line-through"}`}>
                      <bdi>{ar ? p.name_ar : p.name_en}</bdi>
                    </span>
                    <span className="rounded-md bg-slate-100 dark:bg-slate-800 px-2 py-0.5 text-xs font-semibold text-slate-600 dark:text-slate-300">
                      {p.kind === "scale" ? (ar ? "جدول" : "Scale") : (ar ? "ثابتة" : "Fixed")}
                    </span>
                    {!p.is_active && (
                      <span className="rounded-md bg-rose-50 dark:bg-rose-950/50 px-2 py-0.5 text-xs font-bold text-rose-700 dark:text-rose-300 border border-rose-200 dark:border-rose-800">
                        {ar ? "معطل" : "Inactive"}
                      </span>
                    )}
                  </div>
                  {p.prescribed_by && (
                    <span className="text-xs text-slate-400 mt-0.5">
                      {ar ? "الطبيب: " : "Doctor: "}{p.prescribed_by}
                    </span>
                  )}
                </button>

                <div className="flex items-center gap-1.5 ms-2">
                  <button
                    type="button"
                    onClick={() => setDraft(fromPlan(p))}
                    aria-label={ar ? `تعديل ${p.name_ar}` : `Edit ${p.name_en}`}
                    className="flex min-h-11 min-w-11 items-center justify-center rounded-xl border border-slate-200 text-slate-600 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800 transition-colors"
                  >
                    <Pencil className="h-4 w-4" />
                  </button>
                  <button
                    type="button"
                    onClick={() => setPlanToDelete(p)}
                    aria-label={ar ? `حذف ${p.name_ar}` : `Delete ${p.name_en}`}
                    className="flex min-h-11 min-w-11 items-center justify-center rounded-xl border border-rose-200 text-rose-600 hover:bg-rose-50 dark:border-rose-900/60 dark:text-rose-400 dark:hover:bg-rose-950/40 transition-colors"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
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
                          className="min-h-12 w-full rounded-lg border border-slate-200 bg-white px-2 text-center text-base font-bold text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                          inputMode="decimal"
                          value={r[k]}
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

            {/* Scheduled daily dose times */}
            <div>
              <label className="mb-1.5 block text-xs font-semibold text-slate-700 dark:text-slate-300">
                {ar ? "أوقات الجرعات المقررة خلال اليوم" : "Scheduled daily dose times"}
              </label>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {ALL_SLOTS.map((slot) => {
                  const isChecked = draft.frequency_slots.includes(slot.id);
                  const currentSlotTime = draft.slot_times[slot.id] || slot.defaultTime;
                  return (
                    <div
                      key={slot.id}
                      className={`flex items-center justify-between rounded-xl border p-2.5 text-xs transition-all ${
                        isChecked
                          ? "border-blue-500/80 bg-blue-50/80 text-blue-950 dark:bg-blue-950/40 dark:text-blue-200"
                          : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 dark:border-slate-800 dark:bg-slate-800 dark:text-slate-400"
                      }`}
                    >
                      <button
                        type="button"
                        onClick={() => handleToggleSlot(slot.id)}
                        className="flex flex-1 items-center gap-2 text-start"
                      >
                        <div
                          className={`flex h-4 w-4 items-center justify-center rounded-md border ${
                            isChecked
                              ? "border-blue-600 bg-blue-600 text-white"
                              : "border-slate-300 dark:border-slate-600"
                          }`}
                        >
                          {isChecked && <Check className="h-3 w-3 stroke-[3]" />}
                        </div>
                        <Clock className="h-3.5 w-3.5 text-blue-600 dark:text-blue-400" />
                        <span className="font-bold">{ar ? slot.labelAr : slot.labelEn}</span>
                      </button>

                      {isChecked && (
                        <div className="flex items-center gap-1 rounded-lg border border-blue-200 bg-white px-2 py-1 dark:border-blue-800 dark:bg-slate-900">
                          <input
                            type="time"
                            value={currentSlotTime}
                            onChange={(e) => handleSlotTimeChange(slot.id, e.target.value)}
                            className="bg-transparent text-base font-mono font-bold text-slate-900 focus:outline-none dark:text-white"
                          />
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>

            <label className="block space-y-1 text-sm font-semibold text-slate-700 dark:text-slate-300">
              {ar ? "أقل مدة بين إبرتين (ساعات)" : "Minimum hours between injections"}
              <Field
                inputMode="decimal"
                dir="ltr"
                value={draft.min_hours_between}
                placeholder={draft.frequency_slots.length > 0 ? String(autoIntervalHours(draft.frequency_slots, draft.slot_times)) : "4"}
                onChange={(e) => set({ min_hours_between: e.target.value })}
              />
              <p className="text-xs text-slate-500">
                {ar
                  ? draft.frequency_slots.length > 0
                    ? `اتركه فارغاً للحساب التلقائي (${autoIntervalHours(draft.frequency_slots, draft.slot_times)} ساعة حسب الأوقات المحددة).`
                    : "اتركه فارغاً للحساب التلقائي (4 ساعات افتراضياً)."
                  : draft.frequency_slots.length > 0
                    ? `Leave empty for automatic (${autoIntervalHours(draft.frequency_slots, draft.slot_times)} h based on scheduled times).`
                    : "Leave empty for automatic (4 h default)."}
              </p>
            </label>

            <Field value={draft.prescribed_by} onChange={(e) => set({ prescribed_by: e.target.value })} placeholder={ar ? "اسم الطبيب" : "Doctor's name"} aria-label={ar ? "اسم الطبيب" : "Doctor's name"} />
            <Field value={draft.notes_ar} onChange={(e) => set({ notes_ar: e.target.value })} placeholder="تعليمات (مثل: قبل الأكل بـ 15 دقيقة)" aria-label="تعليمات" dir="rtl" />
            <Field value={draft.notes_en} onChange={(e) => set({ notes_en: e.target.value })} placeholder="Instructions (e.g. 15 min before meals)" aria-label="Instructions" dir="ltr" />

            {draft.id && (
              <div className="flex items-center justify-between pt-2 border-t border-slate-200 dark:border-slate-800">
                <label className="flex min-h-12 items-center gap-2 text-sm font-semibold text-slate-700 dark:text-slate-300 cursor-pointer">
                  <input type="checkbox" className="h-5 w-5" checked={draft.is_active} onChange={(e) => set({ is_active: e.target.checked })} />
                  {ar ? "مستخدم حالياً" : "Currently in use"}
                </label>

                <button
                  type="button"
                  onClick={() => setPlanToDelete(draft)}
                  className="flex min-h-11 items-center gap-1.5 rounded-xl border border-rose-200 px-3 py-1.5 text-xs font-bold text-rose-600 hover:bg-rose-50 dark:border-rose-900/60 dark:text-rose-400 dark:hover:bg-rose-950/30 transition-colors"
                >
                  <Trash2 className="h-4 w-4" />
                  <span>{ar ? "حذف الإبرة" : "Delete plan"}</span>
                </button>
              </div>
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

      {planToDelete && (
        <div className="fixed inset-0 z-60 flex items-center justify-center bg-black/50 backdrop-blur-2xs p-4 animate-in fade-in duration-150">
          <div className="w-full max-w-sm rounded-3xl bg-white p-5 shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800">
            <div className="flex items-center gap-3 mb-3">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl bg-rose-100 text-rose-600 dark:bg-rose-950/60 dark:text-rose-400">
                <Trash2 className="h-5 w-5" />
              </div>
              <div>
                <h3 className="text-base font-bold text-slate-900 dark:text-white">
                  {ar ? "تأكيد حذف الإبرة" : "Delete Insulin Plan"}
                </h3>
                <p className="text-xs text-slate-500 dark:text-slate-400">
                  <bdi>{ar ? planToDelete.name_ar : planToDelete.name_en}</bdi>
                </p>
              </div>
            </div>

            <p className="text-xs text-slate-600 dark:text-slate-300 mb-4 leading-relaxed">
              {ar
                ? "هل تريد بالتأكيد حذف هذه الإبرة؟ إذا لم تُسجل لها أي جرعات سابقة فستُحذف نهائياً، وإن وُجدت لها سجلات فستؤرشف وتُخفى من الشاشة للحفاظ على السجل الطبي."
                : "Are you sure you want to delete this insulin plan? If no doses were recorded, it will be deleted permanently. Otherwise it will be archived to preserve medical records."}
            </p>

            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                disabled={deleting}
                onClick={() => setPlanToDelete(null)}
                className="min-h-12 rounded-xl border border-slate-200 text-xs font-bold text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800 transition-colors"
              >
                {ar ? "إلغاء" : "Cancel"}
              </button>
              <button
                type="button"
                disabled={deleting}
                onClick={handleConfirmDelete}
                className="flex min-h-12 items-center justify-center gap-1.5 rounded-xl bg-rose-600 hover:bg-rose-700 text-xs font-bold text-white shadow-xs transition-all active:scale-95 disabled:opacity-50"
              >
                {deleting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
                <span>{ar ? "نعم، احذف" : "Yes, delete"}</span>
              </button>
            </div>
          </div>
        </div>
      )}
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
