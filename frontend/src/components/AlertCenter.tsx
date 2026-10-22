"use client";

import { haptic } from "@/lib/theme";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { formatTime } from "@/lib/datetime";
import { playAlert } from "@/lib/alertSound";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import { setBPLowThresholds, setOxygenFeverThresholds } from "@/lib/clinical";
import Link from "next/link";
import type { SSEEvent } from "@/lib/types";
import { useContacts } from "./CrisisActions";
import { Ltr } from "./Ltr";
import { tagLabel } from "@/lib/handoverTags";
import { daysLeftText } from "@/lib/stockText";
import { Phone, Siren, Check, AlertTriangle, ArrowRightLeft } from "lucide-react";

// Duty handovers (issue #18) are addressed to one sibling and aren't emergencies.
const DUTY_KINDS = new Set(["duty_handover", "duty_unaccepted"]);

// Alerts every sibling must see (issues #11, #12): a BP crisis, a dose given inside
// its minimum interval, vomiting after a dose. They come from the server
// (/alerts/unacked), so one missed while the phone was locked or the app closed is
// replayed on the next open or reconnect, with the alarm, until this caregiver
// taps "Seen". The crisis bar keeps both call buttons on screen on every page.

export interface ServerAlert {
  id: string;
  kind: "bp_crisis" | "bp_low" | "alertness" | "fast_positive" | "dose_override" | "vomited" | "correction" | "duty_handover" | "duty_unaccepted" | "emergency_card" | "low_stock" | "spo2_low" | "reposition_overdue" | "bowel_overdue" | "low_fluid" | "backup_failed";
  created_by_name?: string;
  payload: Record<string, unknown>;
  created_at: string;
}

export function AlertCenter() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const contacts = useContacts();
  const [alerts, setAlerts] = useState<ServerAlert[]>([]);
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
  const red = !duty && current.kind !== "dose_override" && current.kind !== "vomited" && current.kind !== "correction" && current.kind !== "emergency_card" && current.kind !== "low_stock";
  const crisis = current.kind === "bp_crisis";
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
        {red && (
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
          (!red || contacts.doctor_phone) && seenButton
        )}
      </div>
    </div>
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

/** Admins see this when the server timezone does not match device timezone (issue #26). */
export function TimezoneMismatchBanner() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const [serverTz, setServerTz] = useState<string | null>(null);

  useEffect(() => {
    fetchApi<{ timezone?: string }>("/health")
      .then((data) => {
        if (data?.timezone) {
          setServerTz(data.timezone);
        }
      })
      .catch(() => {});
  }, []);

  const role = currentCaregiver?.role;
  if (role !== "admin" && role !== "super_admin") return null;
  if (!serverTz) return null;

  const deviceTz = typeof Intl !== "undefined" && Intl.DateTimeFormat ? Intl.DateTimeFormat().resolvedOptions().timeZone : null;
  if (!deviceTz || serverTz === deviceTz) return null;

  return (
    <div role="alert" data-testid="timezone-mismatch-banner" className="bg-amber-600 px-4 py-2 text-center text-sm font-bold text-white">
      {locale === "ar"
        ? `تنبيه المنطقة الزمنية: الخادم يعمل بتوقيت (${serverTz}) بينما جهازك مضبوط على (${deviceTz}). قد تختلف تواريخ الجرعات والتقارير بين منتصف الليل وفجر اليوم.`
        : `Time zone mismatch: Server is in (${serverTz}) while your device is set to (${deviceTz}). Dose dates and reports may differ around midnight.`}
    </div>
  );
}

