"use client";

import { haptic } from "@/lib/theme";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi, type ApiError } from "@/lib/api";
import { formatTime } from "@/lib/datetime";
import { clockDiffersFromHome, homeCity } from "@/lib/householdTime";
import { useHouseholdZone } from "@/context/HouseholdTimeContext";
import { playAlert } from "@/lib/alertSound";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import { setBPLowThresholds, setOxygenFeverThresholds } from "@/lib/clinical";
import Link from "next/link";
import type { SSEEvent } from "@/lib/types";
import { useContacts } from "./CrisisActions";
import { Ltr } from "./Ltr";
import { tagLabel } from "@/lib/handoverTags";
import { bleedingAdvice, bleedingLabel } from "@/lib/bruises";
import { daysLeftText } from "@/lib/stockText";
import { Phone, Siren, Check, AlertTriangle, ArrowRightLeft } from "lucide-react";

// Duty handovers (issue #18) are addressed to one sibling and aren't emergencies.
const DUTY_KINDS = new Set(["duty_handover", "duty_unaccepted"]);

// Alerts every sibling must see (issues #11, #12): a BP crisis, a dose given inside
// its minimum interval, vomiting after a dose. They come from the server
// (/alerts/unacked), so one missed while the phone was locked or the app closed is
// replayed on the next open or reconnect, with the alarm, until this caregiver
// taps "Seen". The crisis bar keeps both call buttons on screen on every page.
// Which alerts are emergencies comes from the server (the "emergency" flag,
// models.EmergencyAlertKinds, issue #54), not from a list kept here.

// Orange: needs attention soon, not an emergency.
const AMBER_KINDS = new Set(["dose_override", "vomited", "correction", "emergency_card", "low_stock", "sugar_very_high", "fever", "fall", "seizure", "inr_out_of_range"]);

export interface ServerAlert {
  id: string;
  kind: "bp_crisis" | "bp_low" | "alertness" | "fast_positive" | "dose_override" | "vomited" | "correction" | "duty_handover" | "duty_unaccepted" | "emergency_card" | "low_stock" | "spo2_low" | "reposition_overdue" | "bowel_overdue" | "low_fluid" | "backup_failed" | "bleeding" | "sugar_low" | "sugar_very_high" | "fever" | "fever_high" | "dose_overdue" | "fall" | "seizure" | "inr_out_of_range";
  created_by_name?: string;
  emergency?: boolean;
  payload: Record<string, unknown>;
  created_at: string;
}

export function AlertCenter() {
  const { locale } = useLanguage();
  const { currentCaregiver, allCaregivers } = useCaregiver();
  const contacts = useContacts();
  const [alerts, setAlerts] = useState<ServerAlert[]>([]);
  // Overdue dose (issue #55): the "who gave it?" picker and any error, for one alert.
  const [doseUi, setDoseUi] = useState<{ id: string; picking: boolean; msg: string }>({ id: "", picking: false, msg: "" });
  const announced = useRef(new Set<string>());
  const ar = locale === "ar";
  const me = currentCaregiver?.id;

  const load = useCallback(async () => {
    if (!me) return;
    const list = await fetchApi<ServerAlert[]>("/alerts/unacked").catch(() => null);
    if (!Array.isArray(list)) return; // offline or unexpected: keep what is on screen
    setAlerts(list);
    const fresh = list.filter((a) => !announced.current.has(a.id));
    if (fresh.length) {
      fresh.forEach((a) => announced.current.add(a.id));
      const quiet = fresh.every((a) => DUTY_KINDS.has(a.kind) || a.kind === "low_stock" || a.kind === "bowel_overdue" || a.kind === "low_fluid" || a.kind === "backup_failed");
      playAlert(quiet ? "chime" : "crisis");
      haptic([400, 150, 400, 150, 400], { crisis: !quiet });
    }
  }, [me]);

  // On open / profile change, when the phone wakes, and after every stream resync.
  useEffect(() => {
    if (!me) {
      setAlerts([]);
      return;
    }
    load();
    const onVisible = () => document.visibilityState === "visible" && load();
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener(RESYNC_EVENT, load);
    return () => {
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener(RESYNC_EVENT, load);
    };
  }, [me, load]);

  // The doctor's low-BP thresholds, so the app classifies readings like the server.
  useEffect(() => {
    if (!me) return;
    fetchApi<{ bp_low_systolic: number; bp_low_diastolic: number; spo2_warn?: number; spo2_critical?: number; fever_c?: number }>("/settings/clinical")
      .then((t) => {
        if (!t) return;
        setBPLowThresholds(t.bp_low_systolic, t.bp_low_diastolic);
        if (t.spo2_warn && t.spo2_critical && t.fever_c) setOxygenFeverThresholds(t.spo2_warn, t.spo2_critical, t.fever_c);
      })
      .catch(() => {});
  }, [me]);

  // Live: any event that created an alert.
  useEffect(() => {
    const onEvent = (e: Event) => {
      const ev = (e as CustomEvent<SSEEvent>).detail;
      if ((ev?.payload as { alert_id?: string } | undefined)?.alert_id) load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    return () => window.removeEventListener("birrcare:sse_event", onEvent);
  }, [load]);

  // Keep the service worker registered so pushes reach this phone when locked.
  useEffect(() => {
    navigator.serviceWorker?.register("/sw.js").catch(() => {});
  }, []);

  const current = alerts[0];
  if (!current) return null;

  const acknowledge = async () => {
    const id = current.id;
    try {
      await fetchApi(`/alerts/${encodeURIComponent(id)}/ack`, { method: "POST" });
      setAlerts((list) => list.filter((a) => a.id !== id));
    } catch {
      // Stay on screen; the caregiver can tap again once the connection is back.
    }
  };

  // "Accept duty": the server clears this alert (and the sender's reminder).
  const acceptDuty = async () => {
    const id = current.id;
    try {
      await fetchApi(`/shifts/handover/${encodeURIComponent(String(current.payload.duty_handover_id))}/accept`, { method: "POST" });
      setAlerts((list) => list.filter((a) => a.id !== id));
    } catch {
      // Stay on screen; tap again once the connection is back.
    }
  };

  const p = current.payload;
  const duty = DUTY_KINDS.has(current.kind);
  const overdue = current.kind === "dose_overdue";
  // A missed blood thinner is red; other missed doses orange. Neither needs the call buttons.
  const red = current.emergency || (overdue ? Boolean(p.critical) : !duty && !AMBER_KINDS.has(current.kind));
  const calls = red && !overdue;
  const ui = doseUi.id === current.id ? doseUi : { id: current.id, picking: false, msg: "" };
  const canGive = overdue && currentCaregiver?.role !== "viewer";
  const otherGivers = (allCaregivers ?? []).filter((c) => c.id !== me);

  // Log the overdue dose as given, by me or by the sibling picked; the server then
  // clears the alert for everyone.
  const giveDose = async (givenBy?: string) => {
    const id = current.id;
    try {
      await fetchApi("/medications/log", {
        method: "POST",
        body: JSON.stringify({
          medication_id: p.medication_id,
          slot: p.slot,
          scheduled_date: p.scheduled_date,
          status: "taken",
          ...(givenBy ? { given_by: givenBy } : {}),
        }),
      });
      setAlerts((list) => list.filter((a) => a.id !== id));
    } catch (e) {
      const err = e as ApiError;
      if (err.data?.error === "DOSE_ALREADY_LOGGED") {
        setAlerts((list) => list.filter((a) => a.id !== id)); // someone else just did
        return;
      }
      const msg =
        err.data?.error === "DOSE_TOO_SOON"
          ? ar ? "لا يمكن الآن: الجرعة السابقة قريبة جداً. اسأل الطبيب أو المسؤول." : "Not now: the previous dose was too recent. Ask the doctor or an admin."
          : !err.status
          ? ar ? "لم تُرسل: تحقق من الاتصال وحاول مرة أخرى." : "Not sent: check the connection and try again."
          : err.message;
      setDoseUi({ id, picking: ui.picking, msg });
    }
  };
  const crisis = current.emergency || current.kind === "bp_crisis";
  const medName = String((ar ? p.name_ar : p.name_en) || p.name_en || p.name_ar || "");
  const when = <bdi dir="auto">{formatTime(current.created_at, locale)}</bdi>;
  const by = current.created_by_name ? <bdi>{current.created_by_name}</bdi> : null;
  const more = alerts.length > 1 ? (ar ? ` (+${alerts.length - 1} تنبيه آخر)` : ` (+${alerts.length - 1} more)`) : "";
  const doctorLabel = contacts.doctor_name || (ar ? "الطبيب" : "Doctor");
  const reading = p.systolic != null ? <Ltr>{String(p.systolic)}/{String(p.diastolic)}</Ltr> : null;

  let title: React.ReactNode;
  let advice = "";
  switch (current.kind) {
    case "bp_crisis":
      title = <>{ar ? "ضغط دم خطير" : "Blood pressure crisis"}: {reading}</>;
      break;
    case "bp_low":
      title = <>{ar ? "ضغط منخفض" : "Low blood pressure"}: {reading}</>;
      advice = ar ? "مدّدها على ظهرها، أعد القياس بعد 5 دقائق، واتصل بالطبيب." : "Lay her down, re-measure in 5 minutes, and call the doctor.";
      break;
    case "fast_positive":
      title = ar ? "علامة جلطة في فحص F.A.S.T. — اتصل بالإسعاف" : "Stroke sign in the F.A.S.T. check — call emergency";
      break;
    case "alertness":
      title = p.level === "drowsy" ? (ar ? "صعب إيقاظها" : "Hard to wake") : ar ? "مشوشة / بطيئة الاستجابة" : "Confused / slow to respond";
      advice =
        p.level === "drowsy"
          ? ar ? "اتصل بالطبيب الآن، وبالإسعاف إذا كان هذا جديداً." : "Call the doctor now, emergency if this is new."
          : ar ? "إذا كان هذا جديداً: افحص F.A.S.T. الآن واتصل بالطبيب." : "If this is new: do the F.A.S.T. check now and call the doctor.";
      break;
    case "correction": {
      // A typo that raised an alert was corrected (issue #15).
      const was = p.original_systolic != null ? <Ltr>{String(p.original_systolic)}/{String(p.original_diastolic)}</Ltr> : null;
      title = (
        <>
          {ar ? "تصحيح لقراءة سابقة" : "Correction to an earlier reading"}
          {reading && <>: {ar ? "كانت" : "it was"} {reading}</>}
          {was && <> ({ar ? "وليس" : "not"} {was})</>}
        </>
      );
      advice = String(p.reason || "");
      break;
    }
    case "spo2_low":
      // Issue #22: oxygen at or below the doctor's critical level.
      title = <>{ar ? "الأكسجين منخفض جداً" : "Oxygen very low"}: <Ltr>SpO₂ {String(p.spo2)}%</Ltr></>;
      advice = ar
        ? "اجلسها مستقيمة وتأكد أن الجهاز على إصبع دافئ. إن تأكد الرقم: اتصل بالإسعاف."
        : "Sit her upright and check the clip is on a warm finger. If it is confirmed: call emergency.";
      break;
    case "sugar_low":
      // Issue #54: the 15-15 rule only, never a dose.
      title = <>{ar ? "هبوط السكر" : "Low blood sugar"}: <Ltr>{String(p.blood_sugar)} mmol/L</Ltr></>;
      advice = ar
        ? "إن كانت تستطيع البلع: أعطها سكراً سريعاً (عصير) الآن وأعد القياس بعد 15 دقيقة. إن لم تستطع البلع أو لا تستجيب: اتصل بالإسعاف."
        : "If she can swallow: give fast sugar (juice) now and recheck in 15 minutes. If she can't swallow or doesn't respond: call emergency.";
      break;
    case "sugar_very_high":
      title = <>{ar ? "السكر مرتفع جداً" : "Blood sugar very high"}: <Ltr>{String(p.blood_sugar)} mmol/L</Ltr></>;
      advice = ar ? "أعد القياس واتصل بالطبيب. أعطها ماءً إن كانت تستطيع البلع." : "Re-measure and call the doctor. Offer water if she can swallow.";
      break;
    case "fever":
      title = <>{ar ? "حمّى" : "Fever"}: <Ltr>{String(p.temperature_c)} °C</Ltr></>;
      advice = ar ? "أعد القياس بعد 30 دقيقة، وقِس الأكسجين، واتصل بالطبيب إن استمرت." : "Re-measure in 30 minutes, check her oxygen, and call the doctor if it stays.";
      break;
    case "fever_high":
      title = (
        <>
          {ar ? "حمّى شديدة" : "High fever"}: <Ltr>{String(p.temperature_c)} °C</Ltr>
          {p.spo2 != null && <> · <Ltr>SpO₂ {String(p.spo2)}%</Ltr></>}
        </>
      );
      advice = ar
        ? "اتصل بالطبيب الآن. إن كان الأكسجين منخفضاً أو تتنفس بصعوبة أو بعد سعال أثناء الأكل: اتصل بالإسعاف."
        : "Call the doctor now. If her oxygen is low, she is breathing hard, or it follows coughing while eating: call emergency.";
      break;
    case "inr_out_of_range":
      // Issue #62: never a dose suggestion, only "tell the doctor".
      title = (
        <>
          {ar ? "نتيجة INR خارج المدى" : "INR outside the range"}: <Ltr>{String(p.value)}</Ltr> (
          <Ltr>
            {String(p.target_low)}–{String(p.target_high)}
          </Ltr>
          )
        </>
      );
      advice = ar ? "أخبر الطبيب قبل جرعة الوارفارين القادمة." : "Tell the doctor before the next warfarin dose.";
      break;
    case "fall": {
      // Issue #60: red (emergency flag) after a head knock, not awake, or pain / bleeding.
      const signs = [
        p.hit_head && (ar ? "ضربت رأسها" : "hit her head"),
        p.awake_normal === false && (ar ? "ليست واعية أو كلامها غير طبيعي" : "not awake or talking normally"),
        p.pain_or_bleeding && (ar ? "ألم أو نزيف" : "pain or bleeding"),
      ].filter(Boolean);
      title = <>{ar ? "سقطت" : "She fell"}{signs.length > 0 && <>: {signs.join(ar ? "، " : ", ")}</>}</>;
      advice = current.emergency
        ? ar ? "اتصل بالإسعاف إن لم يتصل أحد بعد." : "Call emergency if nobody has yet."
        : ar ? "لا ترفعها وحدك. راقبها 24 ساعة: كل 4 ساعات تأكد أنها واعية وتتكلم بشكل طبيعي." : "Don't lift her alone. Watch her for 24 hours: every 4 hours, check she is awake and talking normally.";
      break;
    }
    case "seizure": {
      const secs = Number(p.duration_seconds) || 0;
      title = (
        <>
          {ar ? "نوبة تشنج" : "Seizure"}: <Ltr>{`${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, "0")}`}</Ltr>
          {p.repeat ? (ar ? " (الثانية خلال يوم)" : " (second within a day)") : ""}
        </>
      );
      advice = current.emergency
        ? ar ? "اتصل بالإسعاف إن لم يتصل أحد بعد." : "Call emergency if nobody has yet."
        : ar ? "ابقَ معها حتى تستعيد وعيها، وأخبر الطبيب." : "Stay with her until she is fully awake, and tell the doctor.";
      break;
    }
    case "dose_overdue": {
      // Issue #55: a scheduled dose nobody has logged.
      const dose = (
        <>
          <bdi>{medName}</bdi> {p.dosage ? <bdi>{String(p.dosage)}</bdi> : null} (<Ltr>{String(p.slot_time || "")}</Ltr>)
        </>
      );
      title = ar ? <>لم تُسجَّل جرعة {dose}</> : <>{dose} isn&apos;t logged</>;
      advice = p.critical
        ? ar ? "مميّع للدم: تأكد الآن هل أُعطيت. لا تعطِ جرعة ثانية إن لم تكن متأكداً." : "Blood thinner: check now whether it was given. Don't give a second dose if you're not sure."
        : ar ? "تأكد هل أُعطيت، ثم سجّلها." : "Check whether it was given, then record it.";
      break;
    }
    case "low_stock": {
      // Issue #20: a medicine is about to run out.
      title = (
        <>
          <bdi>{medName}</bdi>: {daysLeftText(Number(p.days_left), ar)}
        </>
      );
      advice = p.blood_thinner
        ? ar ? "مميّع للدم: لا يجوز أن ينقطع. اشترِ علبة جديدة اليوم." : "Blood thinner: must not run out. Buy a new box today."
        : ar ? "اشترِ علبة جديدة قبل أن تنفد." : "Buy a new box before it runs out.";
      break;
    }
    case "emergency_card":
      // Issue #19: someone opened the paramedics' card (QR or SOS).
      title = ar ? "فُتحت بطاقة الطوارئ" : "The emergency card was opened";
      advice = p.opened_by
        ? ar ? `فتحها ${String(p.opened_by)} من التطبيق.` : `${String(p.opened_by)} opened it from the app.`
        : ar ? "فُتحت بدون تسجيل دخول (غالباً المسعفون عبر رمز QR). اتصل بمن عند الوالدة الآن." : "Opened without a login (likely paramedics via the QR code). Call whoever is with her now.";
      break;
    case "duty_handover": {
      const tags = Array.isArray(p.tags) ? (p.tags as string[]).map((k) => tagLabel(k, ar)).join(" · ") : "";
      title = <>{ar ? "سُلّمت إليك المناوبة" : "Duty was passed to you"}</>;
      advice = [tags, String(p.summary || "")].filter(Boolean).join(" — ");
      break;
    }
    case "duty_unaccepted":
      title = (
        <>
          <bdi>{String(p.to_name || "")}</bdi> {ar ? `لم يقبل المناوبة بعد ${p.minutes ?? 15} دقيقة` : `has not accepted the duty after ${p.minutes ?? 15} minutes`}
        </>
      );
      advice = ar ? "اتصل به أو ابقَ في المناوبة." : "Call them, or stay on duty.";
      break;
    case "reposition_overdue": {
      title = ar ? "تأخر تقليب المريض (تجنب قرح الفراش)" : "Repositioning overdue (prevent pressure sores)";
      advice = ar
        ? "تأخر التقليب لأكثر من 45 دقيقة عن الموعد المحدد. يرجى تقليب المريض الآن لتجنب تقرحات الضغط."
        : "Repositioning is overdue by more than 45 minutes. Please reposition the patient now to prevent pressure injuries.";
      break;
    }
    case "bowel_overdue": {
      title = ar ? "تنبيه: لم يحدث إخراج بطن منذ 3 أيام" : "Alert: No bowel movement in 3 days";
      advice = ar
        ? "مرت 3 أيام أو أكثر دون تسجيل إخراج. يرجى المتابعة واستشارة الطبيب أو إعطاء الملين إذا لزم الأمر."
        : "3 or more days without a recorded bowel movement. Please follow up and consult doctor or give prescribed laxative if needed.";
      break;
    }
    case "low_fluid": {
      title = ar ? "تنبيه: انخفاض كمية السوائل اليومية" : "Alert: Low daily fluid intake";
      advice = ar
        ? `السوائل المستهلكة اليوم أقل من 50% من الهدف اليومي (${p.total_ml ?? 0} مل من أصل ${p.target_ml ?? 1500} مل). يرجى تقديم السوائل للمريض.`
        : `Daily fluid intake is below 50% of the target (${p.total_ml ?? 0} ml of ${p.target_ml ?? 1500} ml). Please offer fluids.`;
      break;
    }
    case "backup_failed": {
      // Only the Super Admin gets this (issue #51).
      const reason = String(p.reason || "");
      if (reason === "invalid_grant") {
        title = ar ? "انتهى تسجيل دخول Google للنسخ الاحتياطي" : "Google sign-in for backups expired";
        advice = ar
          ? "لن تُرفع نسخ احتياطية حتى تعيد تسجيل الدخول. شغّل على الخادم: rclone config reconnect gdrive:"
          : "No backups are uploaded until you sign in again. Run on the server: rclone config reconnect gdrive:";
      } else if (reason === "restore_failed") {
        title = ar ? "فشل اختبار استرجاع النسخة الاحتياطية" : "Backup restore test failed";
        advice = ar ? "افتح القائمة ← الخادم لمعرفة السبب." : "Open Menu → Server to see why.";
      } else {
        title = ar ? "لم تُحفظ نسخة احتياطية منذ أكثر من يوم" : "No off-site backup for more than a day";
        advice = ar ? "افتح القائمة ← الخادم لمعرفة السبب." : "Open Menu → Server to see why.";
      }
      break;
    }
    case "bleeding": {
      // A bleeding sign was reported (patient on a blood thinner).
      const signs = Array.isArray(p.bleeding_signs) ? (p.bleeding_signs as string[]) : [];
      title = <>{ar ? "علامة نزيف" : "Bleeding sign"}: {signs.map((s) => bleedingLabel(s, ar)).join(" · ")}</>;
      advice = bleedingAdvice(signs, ar);
      break;
    }
    case "vomited":
      title = <>{ar ? "استفرغت بعد جرعة" : "Vomited after a dose"}{medName && <>: <bdi>{medName}</bdi></>}</>;
      break;
    default:
      title = (
        <>
          {ar ? "أُعطيت جرعة قبل موعدها المسموح (بأمر الطبيب)" : "A dose was given inside its minimum interval (doctor's order)"}
          {medName && <>: <bdi>{medName}</bdi></>}
        </>
      );
  }

  const seenButton = (
    <button
      type="button"
      onClick={acknowledge}
      className={`flex min-h-12 w-full items-center justify-center gap-1.5 rounded-xl text-base font-bold text-white ${red ? "bg-rose-900/50" : duty ? "bg-blue-950/40" : "bg-amber-950/30"}`}
    >
      <Check className="h-5 w-5" />
      {ar ? "تم الاطلاع" : "Seen"}
    </button>
  );

  return (
    <div
      role="alertdialog"
      aria-live="assertive"
      aria-label={typeof title === "string" ? title : ar ? "تنبيه" : "Alert"}
      data-testid={crisis ? "crisis-bar" : "alert-bar"}
      data-kind={current.kind}
      className={`fixed inset-x-0 top-0 z-[60] px-3 pb-3 pt-[calc(0.75rem+env(safe-area-inset-top))] text-white shadow-2xl ${
        red ? "bg-rose-700" : duty ? "bg-blue-800" : "bg-amber-800"
      }`}
    >
      <div className="mx-auto max-w-3xl space-y-2">
        <p className="flex items-start gap-2 text-base font-black leading-snug">
          {duty ? <ArrowRightLeft className="mt-0.5 h-5 w-5 shrink-0" /> : <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0" />}
          <span>
            {title}
            {more}
            {advice && <span className="block text-sm font-bold">{advice}</span>}
            <span className="block text-sm font-semibold">
              {by && (
                <>
                  {current.kind === "duty_handover" ? (ar ? "من" : "From") : current.kind === "duty_unaccepted" ? "" : ar ? "سجّله" : "Logged by"}{" "}
                  {current.kind !== "duty_unaccepted" && by}{" "}
                </>
              )}
              {when}
            </span>
          </span>
        </p>
        {calls && (
          <div className="grid grid-cols-2 gap-2">
            <a
              href={`tel:${contacts.emergency}`}
              className="flex min-h-12 items-center justify-center gap-1.5 rounded-xl bg-white px-2 text-base font-black text-rose-800"
            >
              <Siren className="h-5 w-5 shrink-0" />
              <span>{ar ? "الإسعاف" : "Emergency"}</span>
              <Ltr>{contacts.emergency}</Ltr>
            </a>
            {contacts.doctor_phone ? (
              <a
                href={`tel:${contacts.doctor_phone}`}
                className="flex min-h-12 items-center justify-center gap-1.5 rounded-xl border-2 border-white px-2 text-base font-black text-white"
              >
                <Phone className="h-5 w-5 shrink-0" />
                <bdi className="truncate">{doctorLabel}</bdi>
              </a>
            ) : (
              seenButton
            )}
          </div>
        )}
        {current.kind === "sugar_low" && <RecheckTimer since={current.created_at} ar={ar} />}
        {canGive && (
          <div className="space-y-2">
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() => giveDose()}
                data-testid="give-dose"
                className={`flex min-h-12 items-center justify-center rounded-xl bg-white px-2 text-base font-black ${red ? "text-rose-800" : "text-amber-900"}`}
              >
                <span>
                  {ar ? "أعطيتها أنا" : "Give as"} <bdi>{currentCaregiver?.name}</bdi>
                </span>
              </button>
              <button
                type="button"
                onClick={() => setDoseUi({ id: current.id, picking: !ui.picking, msg: "" })}
                aria-expanded={ui.picking}
                data-testid="someone-else-gave"
                className="flex min-h-12 items-center justify-center rounded-xl border-2 border-white px-2 text-base font-black text-white"
              >
                {ar ? "أعطاها شخص آخر" : "Someone else gave it"}
              </button>
            </div>
            {ui.picking && (
              <div className="rounded-xl bg-white p-2" role="group" aria-label={ar ? "من أعطاها؟" : "Who gave it?"}>
                <p className="mb-1 text-sm font-bold text-slate-800">{ar ? "من أعطاها؟" : "Who gave it?"}</p>
                <div className="grid grid-cols-2 gap-2">
                  {otherGivers.map((c) => (
                    <button
                      key={c.id}
                      type="button"
                      onClick={() => giveDose(c.id)}
                      className="min-h-12 rounded-xl border border-slate-300 px-2 text-base font-bold text-slate-900"
                    >
                      <bdi>{c.name}</bdi>
                    </button>
                  ))}
                </div>
              </div>
            )}
            {ui.msg && (
              <p role="alert" className="rounded-xl bg-white px-3 py-2 text-sm font-bold text-rose-800">
                {ui.msg}
              </p>
            )}
          </div>
        )}
        {current.kind === "alertness" && (
          <Link href="/fast" className="flex min-h-12 items-center justify-center rounded-xl bg-white text-base font-black text-rose-800">
            {ar ? "افحص F.A.S.T. الآن" : "Do the F.A.S.T. check now"}
          </Link>
        )}
        {current.kind === "duty_handover" ? (
          <button
            type="button"
            onClick={acceptDuty}
            data-testid="accept-duty"
            className="flex min-h-14 w-full items-center justify-center gap-2 rounded-xl bg-white text-lg font-black text-blue-900"
          >
            <ArrowRightLeft className="h-5 w-5" />
            {ar ? "أقبل المناوبة" : "Accept duty"}
          </button>
        ) : (
          (!calls || contacts.doctor_phone) && seenButton
        )}
      </div>
    </div>
  );
}

/** The 15-15 rule (issue #54): counts down to the sugar recheck, 15 minutes after the alert. */
function RecheckTimer({ since, ar }: { since: string; ar: boolean }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const left = Math.max(0, Math.ceil((new Date(since).getTime() + 15 * 60_000 - now) / 1000));
  const mmss = `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}`;
  return (
    <p data-testid="sugar-recheck" role="timer" className="rounded-xl bg-white px-3 py-2 text-center text-base font-black text-rose-800">
      {left > 0 ? (
        <>
          {ar ? "أعد قياس السكر بعد" : "Recheck sugar in"} <Ltr>{mmss}</Ltr>
        </>
      ) : ar ? (
        "أعد قياس السكر الآن"
      ) : (
        "Recheck sugar now"
      )}
    </p>
  );
}

/** Admins see this on every page while the server has no EMERGENCY_NUMBER (issue #11). */
export function EmergencySetupBanner() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const contacts = useContacts();
  const role = currentCaregiver?.role;
  if (contacts.configured !== false || (role !== "admin" && role !== "super_admin")) return null;
  return (
    <div role="alert" data-testid="emergency-setup-banner" className="bg-rose-700 px-4 py-2 text-center text-sm font-bold text-white">
      {locale === "ar"
        ? `رقم الطوارئ غير مضبوط. زر الاتصال يستخدم الرقم الاحتياطي ${contacts.emergency}. اضبط EMERGENCY_NUMBER في ملف .env على الخادم.`
        : `Emergency number not configured. The call button uses the fallback ${contacts.emergency}. Set EMERGENCY_NUMBER in the server's .env.`}
    </div>
  );
}

/**
 * Issue #58 (replaces the admin-only zone-name check of #26): when this phone's
 * clock is on a different UTC offset from home, every caregiver sees that the
 * times shown are home time, with home's clock. Bahrain and Riyadh (both UTC+3)
 * are the same clock, so no banner.
 */
export function HomeTimeBanner() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const zone = useHouseholdZone();
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 30_000);
    return () => clearInterval(id);
  }, []);
  if (!currentCaregiver || !clockDiffersFromHome(now, zone)) return null;
  const clock = now.toLocaleTimeString(locale === "ar" ? "ar-SA-u-ca-gregory-nu-latn" : "en-US", { hour: "numeric", minute: "2-digit", hour12: true, timeZone: zone });
  return (
    <div role="status" data-testid="home-time-banner" className="bg-blue-800 px-4 py-2 text-center text-sm font-bold text-white">
      {locale === "ar" ? "الأوقات المعروضة بتوقيت البيت" : "Times shown are home time"} (<bdi>{homeCity(zone)}</bdi>, <Ltr>{clock}</Ltr>)
    </div>
  );
}
