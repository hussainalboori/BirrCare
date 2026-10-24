"use client";

import { haptic } from "@/lib/theme";
import React, { useEffect, useRef, useState } from "react";
import { InsulinPlanStatus, InsulinLog } from "@/lib/types";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { canManageMedications, isViewer } from "@/lib/permissions";
import { HYPO_THRESHOLD, lookupInsulinScale } from "@/lib/clinical";
import { formatTime, formatShortDate, parseServerTime } from "@/lib/datetime";
import { BloodSugarBadge } from "./BloodSugarBadge";
import { CrisisActions } from "./CrisisActions";
import { Ltr } from "./Ltr";
import { firstName } from "@/lib/actor";
import { Syringe, Lock, AlertTriangle, Check, Minus, Plus, Loader2, Ban, Clock } from "lucide-react";

type Mode = "normal" | "adjust" | "hold";

const fmtUnits = (u: number) => String(Number(u.toFixed(1)));

const ADJUST_REASONS = {
  ar: ["الطبيب قال بالهاتف", "أكلت أقل من المعتاد", "أكلت أكثر من المعتاد"],
  en: ["Doctor said by phone", "Ate less than usual", "Ate more than usual"],
};
const HOLD_REASONS = {
  ar: ["لم تأكل", "السكر منخفض", "الطبيب قال أوقفها"],
  en: ["She did not eat", "Sugar is low", "Doctor said hold"],
};

interface Props {
  status: InsulinPlanStatus;
  onLogged: () => void;
}

export function InsulinPlanCard({ status, onLogged }: Props) {
  const { locale } = useLanguage();
  const { currentCaregiver, openPinModal } = useCaregiver();
  const { plan, last_given, next_allowed_at, recent } = status;
  const ar = locale === "ar";

  const [sugarStr, setSugarStr] = useState("");
  const [mode, setMode] = useState<Mode>("normal");
  const [units, setUnits] = useState(0);
  const [reason, setReason] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const inFlightRef = useRef(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

  // Re-evaluate the interval lock as time passes, without a refetch.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 30000);
    return () => clearInterval(id);
  }, []);

  const name = ar ? plan.name_ar : plan.name_en;
  const notes = ar ? plan.notes_ar : plan.notes_en;
  const sugar = sugarStr.trim() === "" ? NaN : Number(sugarStr.replace(",", "."));
  const hasSugar = Number.isFinite(sugar) && sugar >= 1 && sugar <= 45;
  const isHypo = hasSugar && sugar < HYPO_THRESHOLD;
  const expected: number | null =
    plan.kind === "fixed" ? plan.fixed_units ?? null : hasSugar ? lookupInsulinScale(plan.scale, sugar) : null;
  const nextAllowed = parseServerTime(next_allowed_at);
  const locked = !!nextAllowed && nextAllowed.getTime() > now;
  const canLog = !!currentCaregiver && !isViewer(currentCaregiver.role);
  const isAdmin = canManageMedications(currentCaregiver?.role);
  // A scale plan cannot say anything until there is a reading.
  const ready = plan.kind === "fixed" || hasSugar;

  const reset = () => {
    setMode("normal");
    setReason("");
    setSugarStr("");
  };

  const startAdjust = () => {
    setUnits(expected && expected > 0 ? expected : 1);
    setReason("");
    setMode("adjust");
  };

  const submit = async (body: { status: "given" | "held"; units?: number; reason?: string; override_interval?: boolean }) => {
    if (!currentCaregiver) {
      openPinModal();
      return;
    }
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    setSubmitting(true);
    setError(null);
    try {
      await fetchApi("/insulin/log", {
        method: "POST",
        body: JSON.stringify({
          plan_id: plan.id,
          blood_sugar: hasSugar ? sugar : undefined,
          ...body,
        }),
      });
      haptic(40);
      setDone(
        body.status === "given"
          ? ar ? `تم تسجيل ${fmtUnits(body.units || 0)} وحدة` : `Recorded ${fmtUnits(body.units || 0)} units`
          : ar ? "تم تسجيل القراءة — لم تُعطَ إبرة" : "Reading recorded — no injection given"
      );
      setTimeout(() => setDone(null), 6000);
      reset();
      onLogged();
    } catch (err: any) {
      setError(describeError(err, ar));
      if (err?.status === 409) onLogged(); // show the injection that beat us
    } finally {
      inFlightRef.current = false;
      setSubmitting(false);
    }
  };

  const reasonChips = (list: string[]) => (
    <div className="flex flex-wrap gap-2">
      {list.map((r) => (
        <button
          key={r}
          type="button"
          onClick={() => setReason(r)}
          aria-pressed={reason === r}
          className={`min-h-12 rounded-full border px-4 text-sm font-semibold active:scale-95 ${
            reason === r
              ? "border-emerald-600 bg-emerald-50 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300"
              : "border-slate-200 bg-slate-50 text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
          }`}
        >
          {r}
        </button>
      ))}
    </div>
  );

  const reasonInput = (
    <input
      type="text"
      value={reason}
      onChange={(e) => setReason(e.target.value)}
      placeholder={ar ? "السبب (مطلوب)" : "Reason (required)"}
      aria-label={ar ? "السبب" : "Reason"}
      className="min-h-12 w-full rounded-xl border border-slate-200 bg-white px-3 text-base text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
    />
  );

  return (
    <section className="rounded-3xl border border-slate-200 bg-white p-4 shadow-xs dark:border-slate-800 dark:bg-slate-900">
      {/* Title */}
      <div className="mb-3 flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 className="flex items-center gap-2 text-lg font-black text-slate-900 dark:text-white">
            <Syringe className="h-5 w-5 shrink-0 text-blue-600" />
            <bdi className="truncate">{name}</bdi>
          </h2>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            {plan.kind === "scale"
              ? ar ? "جرعة حسب جدول الطبيب" : "Dose from the doctor's scale"
              : <>{ar ? "جرعة ثابتة: " : "Fixed dose: "}<Ltr>{fmtUnits(plan.fixed_units || 0)}</Ltr>{ar ? " وحدة" : " units"}</>}
            {plan.prescribed_by && <> · <bdi>{plan.prescribed_by}</bdi></>}
          </p>
          {notes && <p className="mt-1 text-xs font-semibold text-slate-600 dark:text-slate-300">{notes}</p>}
          {plan.frequency_slots && plan.frequency_slots.length > 0 && (
            <div className="mt-2 flex flex-wrap items-center gap-1.5">
              {plan.frequency_slots.map((s) => {
                const timeStr = plan.slot_times?.[s];
                const slotLabel =
                  s === "morning"
                    ? ar ? "الصباح" : "Morning"
                    : s === "afternoon"
                    ? ar ? "الظهيرة" : "Afternoon"
                    : s === "evening"
                    ? ar ? "المساء" : "Evening"
                    : s === "bedtime"
                    ? ar ? "قبل النوم" : "Bedtime"
                    : s === "night"
                    ? ar ? "الليل" : "Night"
                    : s;
                return (
                  <span
                    key={s}
                    className="inline-flex items-center gap-1 rounded-lg border border-blue-200 bg-blue-50/80 px-2.5 py-1 text-xs font-bold text-blue-900 dark:border-blue-900/60 dark:bg-blue-950/40 dark:text-blue-300"
                  >
                    <Clock className="h-3 w-3 text-blue-600 dark:text-blue-400" />
                    <span>{slotLabel}</span>
                    {timeStr && <Ltr className="font-mono text-xs font-black">{timeStr}</Ltr>}
                  </span>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* Last injection: the 3 AM question, answered first */}
      <div className="mb-3 rounded-2xl bg-slate-50 p-3 text-sm dark:bg-slate-800/60">
        {last_given ? (
          <p className="font-semibold text-slate-800 dark:text-slate-100">
            {ar ? "آخر إبرة: " : "Last injection: "}
            <Ltr className="font-black">{fmtUnits(last_given.units)}</Ltr>
            {ar ? " وحدة بواسطة " : " units by "}
            <bdi>{last_given.caregiver_name}</bdi>
            {ar ? " " : " "}
            {formatShortDate(last_given.given_at, locale)} <Ltr>{formatTime(last_given.given_at, locale)}</Ltr>
            {last_given.blood_sugar != null && (
              <span className="text-slate-500 dark:text-slate-400">
                {" "}({ar ? "السكر " : "sugar "}<Ltr>{last_given.blood_sugar.toFixed(1)}</Ltr>)
              </span>
            )}
          </p>
        ) : (
          <p className="text-slate-500 dark:text-slate-400">{ar ? "لا توجد إبرة مسجلة بعد" : "No injection recorded yet"}</p>
        )}
      </div>

      {locked && (
        <div role="status" className="mb-3 flex items-start gap-2 rounded-2xl border-2 border-amber-400 bg-amber-50 p-3 text-sm font-bold text-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
          <Lock className="mt-0.5 h-5 w-5 shrink-0" />
          <span>
            {ar ? "مقفلة: لا تُعطَ إبرة أخرى قبل " : "Locked: no other injection before "}
            <Ltr>{formatTime(next_allowed_at, locale)}</Ltr>
            {ar ? " (لتجنب تراكم الإنسولين)" : " (prevents insulin stacking)"}
          </span>
        </div>
      )}

      {done && (
        <div role="status" className="mb-3 flex items-center gap-2 rounded-2xl bg-emerald-600 p-3 text-sm font-bold text-white">
          <Check className="h-5 w-5" /> {done}
        </div>
      )}

      {error && (
        <div role="alert" className="mb-3 flex items-start gap-2 rounded-2xl border border-rose-300 bg-rose-50 p-3 text-sm font-semibold text-rose-800 dark:bg-rose-950/40 dark:text-rose-200">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" /> <span>{error}</span>
        </div>
      )}

      {canLog || !currentCaregiver ? (
        <>
          {/* Step 1: the reading, taken now */}
          <label className="mb-1 block text-sm font-bold text-slate-700 dark:text-slate-300">
            {ar ? "السكر الآن" : "Blood sugar now"} <Ltr className="font-normal text-slate-400">(mmol/L)</Ltr>
            {plan.kind === "fixed" && <span className="font-normal text-slate-400">{ar ? " — يُفضّل" : " — recommended"}</span>}
          </label>
          <input
            type="text"
            inputMode="decimal"
            dir="ltr"
            value={sugarStr}
            onChange={(e) => {
              setSugarStr(e.target.value);
              setError(null);
              setMode("normal");
            }}
            placeholder="0.0"
            aria-label={ar ? "قراءة السكر الآن" : "Blood sugar reading now"}
            className="mb-3 min-h-14 w-full rounded-2xl border-2 border-slate-200 bg-white px-4 text-center text-3xl font-black text-slate-900 focus:border-blue-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
          />

          {hasSugar && <div className="mb-3"><BloodSugarBadge sugar={sugar} /></div>}

          {/* The doctor's table, with this reading's row highlighted */}
          {plan.kind === "scale" && plan.scale.length > 0 && (
            <table className="mb-3 w-full overflow-hidden rounded-2xl text-sm">
              <thead>
                <tr className="bg-slate-100 text-xs text-slate-500 dark:bg-slate-800 dark:text-slate-400">
                  <th className="p-2 text-start">{ar ? "السكر" : "Sugar"} <Ltr>(mmol/L)</Ltr></th>
                  <th className="p-2 text-end">{ar ? "الوحدات" : "Units"}</th>
                </tr>
              </thead>
              <tbody>
                {plan.scale.map((row) => {
                  const hit = hasSugar && sugar >= row.min && sugar < row.max;
                  return (
                    <tr
                      key={`${row.min}-${row.max}`}
                      className={hit ? "bg-blue-600 font-black text-white" : "border-t border-slate-100 text-slate-700 dark:border-slate-800 dark:text-slate-300"}
                    >
                      <td className="p-2"><Ltr>{row.min} – {row.max}</Ltr></td>
                      <td className="p-2 text-end"><Ltr>{fmtUnits(row.units)}</Ltr></td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}

          {/* Step 2: what to do */}
          {ready && mode === "normal" && (
            <div className="space-y-2">
              {isHypo ? (
                <div className="rounded-2xl border-2 border-rose-500 bg-rose-50 p-3 dark:bg-rose-950/40">
                  <p className="mb-2 text-base font-black text-rose-800 dark:text-rose-200">
                    {ar ? "السكر منخفض — لا تُعطِ إنسولين" : "Sugar is LOW — do NOT give insulin"}
                  </p>
                  <p className="mb-3 text-sm font-semibold text-rose-800 dark:text-rose-200">
                    {ar
                      ? "أعطِها عصيراً أو 3 مكعبات سكر الآن، وأعد القياس بعد 15 دقيقة. إذا لم تستطع البلع أو لم تستجب، اتصل بالإسعاف."
                      : "Give juice or 3 sugar cubes now and measure again in 15 minutes. If she cannot swallow or does not respond, call emergency."}
                  </p>
                  <CrisisActions compact />
                  <button
                    type="button"
                    disabled={submitting}
                    onClick={() => submit({ status: "held", reason: ar ? "سكر منخفض" : "Low sugar" })}
                    className="mt-2 min-h-12 w-full rounded-xl bg-white text-sm font-bold text-rose-700 dark:bg-slate-900 dark:text-rose-300"
                  >
                    {ar ? "سجّل القراءة — لم تُعطَ إبرة" : "Record reading — no injection"}
                  </button>
                  {plan.kind === "fixed" && (
                    <button type="button" onClick={startAdjust} className="mt-2 min-h-12 w-full text-sm font-semibold text-rose-700 underline dark:text-rose-300">
                      {ar ? "الطبيب قال أعطِها رغم ذلك" : "The doctor said give it anyway"}
                    </button>
                  )}
                </div>
              ) : expected === null ? (
                <div className="rounded-2xl border-2 border-amber-500 bg-amber-50 p-3 dark:bg-amber-950/40">
                  <p className="mb-3 text-base font-black text-amber-900 dark:text-amber-200">
                    {ar ? "القراءة خارج جدول الطبيب — اتصل بالطبيب قبل إعطاء أي إنسولين" : "This reading is outside the doctor's scale — call the doctor before giving any insulin"}
                  </p>
                  <CrisisActions compact />
                  {!locked && (
                    <button type="button" onClick={startAdjust} className="mt-2 min-h-12 w-full rounded-xl bg-white text-sm font-bold text-amber-800 dark:bg-slate-900 dark:text-amber-200">
                      {ar ? "سجّل الجرعة التي حددها الطبيب" : "Record the dose the doctor gave"}
                    </button>
                  )}
                </div>
              ) : expected === 0 ? (
                <button
                  type="button"
                  disabled={submitting}
                  onClick={() => submit({ status: "held", reason: ar ? "الجدول: 0 وحدة" : "Scale: 0 units" })}
                  className="flex min-h-16 w-full items-center justify-center gap-2 rounded-2xl bg-emerald-600 text-lg font-black text-white active:scale-95 disabled:opacity-60"
                >
                  <Check className="h-6 w-6" />
                  {ar ? "لا تحتاج إنسولين — سجّل القراءة" : "No insulin needed — record reading"}
                </button>
              ) : locked ? null : (
                <button
                  type="button"
                  disabled={submitting}
                  onClick={() => submit({ status: "given", units: expected })}
                  className="flex min-h-16 w-full flex-wrap items-center justify-center gap-x-2 rounded-2xl bg-blue-600 px-3 py-2 text-xl font-black text-white shadow-lg shadow-blue-500/30 active:scale-95 disabled:opacity-60"
                >
                  {submitting ? <Loader2 className="h-6 w-6 animate-spin" /> : <Syringe className="h-6 w-6" />}
                  {ar ? "أعطيتها " : "Gave "}
                  <Ltr>{fmtUnits(expected)}</Ltr>
                  {ar ? " وحدة" : " units"}
                  {currentCaregiver && (
                    <span className="text-sm font-bold opacity-90">
                      {ar ? `— باسم ${firstName(currentCaregiver.name)}` : `— as ${firstName(currentCaregiver.name)}`}
                    </span>
                  )}
                </button>
              )}

              {!isHypo && expected !== null && expected > 0 && (
                <div className="grid grid-cols-2 gap-2">
                  {(!locked || isAdmin) && (
                    <button type="button" onClick={startAdjust} className="min-h-12 rounded-xl border border-slate-200 text-sm font-semibold text-slate-700 dark:border-slate-700 dark:text-slate-200">
                      {locked
                        ? ar ? "تجاوز القفل (مشرف)" : "Override lock (admin)"
                        : ar ? "جرعة مختلفة" : "Different dose"}
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => {
                      setReason("");
                      setMode("hold");
                    }}
                    className="min-h-12 rounded-xl border border-slate-200 text-sm font-semibold text-slate-700 dark:border-slate-700 dark:text-slate-200"
                  >
                    {ar ? "لن أعطيها الآن" : "Not giving now"}
                  </button>
                </div>
              )}
            </div>
          )}

          {/* Different dose: always with a reason, which the doctor will see */}
          {mode === "adjust" && (
            <div className="space-y-3 rounded-2xl border border-slate-200 p-3 dark:border-slate-700">
              <div className="flex items-center justify-center gap-4">
                <button type="button" aria-label={ar ? "أقل" : "Less"} onClick={() => setUnits((u) => Math.max(0.5, u - 0.5))} className="flex h-14 w-14 items-center justify-center rounded-full bg-slate-100 dark:bg-slate-800">
                  <Minus className="h-6 w-6" />
                </button>
                <span className="min-w-24 text-center text-3xl font-black text-slate-900 dark:text-white">
                  <Ltr>{fmtUnits(units)}</Ltr>
                  <span className="block text-xs font-semibold text-slate-500">{ar ? "وحدة" : "units"}</span>
                </span>
                <button type="button" aria-label={ar ? "أكثر" : "More"} onClick={() => setUnits((u) => Math.min(100, u + 0.5))} className="flex h-14 w-14 items-center justify-center rounded-full bg-slate-100 dark:bg-slate-800">
                  <Plus className="h-6 w-6" />
                </button>
              </div>
              {expected !== null && (
                <p className="text-center text-xs text-slate-500">
                  {ar ? "جدول الطبيب يقول: " : "Doctor's plan says: "}<Ltr>{fmtUnits(expected)}</Ltr>
                </p>
              )}
              {reasonChips(ADJUST_REASONS[locale])}
              {reasonInput}
              <div className="grid grid-cols-2 gap-2">
                <button type="button" onClick={() => setMode("normal")} className="min-h-12 rounded-xl text-sm font-semibold text-slate-600 dark:text-slate-300">
                  {ar ? "إلغاء" : "Cancel"}
                </button>
                <button
                  type="button"
                  disabled={submitting || !reason.trim()}
                  onClick={() => submit({ status: "given", units, reason: reason.trim(), override_interval: locked || undefined })}
                  className="min-h-12 rounded-xl bg-blue-600 text-sm font-bold text-white disabled:opacity-50"
                >
                  {ar ? "سجّل " : "Record "}<Ltr>{fmtUnits(units)}</Ltr>{ar ? " وحدة" : " units"}
                </button>
              </div>
            </div>
          )}

          {mode === "hold" && (
            <div className="space-y-3 rounded-2xl border border-slate-200 p-3 dark:border-slate-700">
              {reasonChips(HOLD_REASONS[locale])}
              {reasonInput}
              <div className="grid grid-cols-2 gap-2">
                <button type="button" onClick={() => setMode("normal")} className="min-h-12 rounded-xl text-sm font-semibold text-slate-600 dark:text-slate-300">
                  {ar ? "إلغاء" : "Cancel"}
                </button>
                <button
                  type="button"
                  disabled={submitting || !reason.trim()}
                  onClick={() => submit({ status: "held", reason: reason.trim() })}
                  className="flex min-h-12 items-center justify-center gap-1 rounded-xl bg-slate-700 text-sm font-bold text-white disabled:opacity-50"
                >
                  <Ban className="h-4 w-4" /> {ar ? "سجّل: لم تُعطَ" : "Record: not given"}
                </button>
              </div>
            </div>
          )}
        </>
      ) : null}

      {recent.length > 0 && <History logs={recent} />}
    </section>
  );
}

function History({ logs }: { logs: InsulinLog[] }) {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  return (
    <ul className="mt-4 divide-y divide-slate-100 border-t border-slate-100 text-xs dark:divide-slate-800 dark:border-slate-800">
      {logs.map((l) => (
        <li key={l.id} className="flex flex-wrap items-center gap-x-2 gap-y-0.5 py-2 text-slate-600 dark:text-slate-300">
          <Ltr className="font-mono text-slate-400">{formatShortDate(l.given_at, locale)} {formatTime(l.given_at, locale)}</Ltr>
          {l.blood_sugar != null && <Ltr className="font-semibold text-blue-700 dark:text-blue-300">{l.blood_sugar.toFixed(1)} mmol/L</Ltr>}
          <span className={`font-bold ${l.status === "given" ? "text-slate-900 dark:text-white" : "text-slate-400"}`}>
            {l.status === "given" ? <><Ltr>{fmtUnits(l.units)}</Ltr>{ar ? " وحدة" : " u"}</> : ar ? "لم تُعطَ" : "not given"}
          </span>
          <bdi className="text-slate-400">{l.caregiver_name}</bdi>
          {l.reason && <span className="w-full text-amber-700 dark:text-amber-400">↳ <bdi>{l.reason}</bdi></span>}
        </li>
      ))}
    </ul>
  );
}

function describeError(err: any, ar: boolean): string {
  const d = err?.data || {};
  switch (d.error) {
    case "INSULIN_TOO_SOON": {
      const l = d.last || {};
      const at = formatTime(l.given_at, ar ? "ar" : "en");
      return ar
        ? `أُعطيت إبرة بالفعل: ${fmtUnits(l.units || 0)} وحدة بواسطة ${l.caregiver_name || "أحد الأخوة"} الساعة ${at}. لا تُعطِ مرة أخرى.`
        : `Already given: ${fmtUnits(l.units || 0)} units by ${l.caregiver_name || "a sibling"} at ${at}. Do not inject again.`;
    }
    case "HYPO_NO_INSULIN":
      return ar ? "السكر أقل من 3.9 — لا يُعطى إنسولين التصحيح." : "Sugar is below 3.9 — correction insulin is not allowed.";
    case "HYPO_REASON_REQUIRED":
    case "REASON_REQUIRED":
      return ar ? "اكتب السبب، مثل توجيه الطبيب." : "Write the reason, e.g. the doctor's instruction.";
    case "SUGAR_REQUIRED":
      return ar ? "قِس السكر الآن وأدخل القراءة." : "Measure the sugar now and enter the reading.";
    default:
      return err?.message || (ar ? "تعذر الحفظ" : "Could not save");
  }
}
