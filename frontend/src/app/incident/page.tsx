"use client";

import React, { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { newRequestId } from "@/lib/outbox";
import { canTakeActions } from "@/lib/permissions";
import { useContacts } from "@/components/CrisisActions";
import { Ltr } from "@/components/Ltr";
import { ArrowLeft, Phone, Siren } from "lucide-react";

// "Something happened" (issue #60): a fall or a seizure, with big buttons and
// plain steps, not a form. The call buttons stay on screen the whole time and
// calling never waits for the save.

type Flow = "choose" | "fall" | "seizure";
type FallKey = "hit_head" | "awake_normal" | "pain_or_bleeding";
const SEIZURE_EMERGENCY_MS = 5 * 60_000;

const FALL_QUESTIONS: { key: FallKey; en: string; ar: string }[] = [
  { key: "hit_head", en: "Did she hit her head?", ar: "هل ضربت رأسها؟" },
  { key: "awake_normal", en: "Is she awake and talking normally?", ar: "هل هي واعية وتتكلم بشكل طبيعي؟" },
  { key: "pain_or_bleeding", en: "New pain, can't move an arm or leg, or bleeding?", ar: "ألم جديد، أو لا تستطيع تحريك يد أو رجل، أو نزيف؟" },
];

interface Saved {
  escalate: boolean;
  repeat?: boolean;
  blood_thinners?: string[];
}

function mmss(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

export default function IncidentPage() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const contacts = useContacts();
  const ar = locale === "ar";
  const [flow, setFlow] = useState<Flow>("choose");
  const [answers, setAnswers] = useState<Partial<Record<FallKey, boolean>>>({});
  const [startedAt, setStartedAt] = useState<number | null>(null);
  const [stoppedMs, setStoppedMs] = useState<number | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [saved, setSaved] = useState<Saved | null>(null);
  const [notSent, setNotSent] = useState(false);
  const requestId = useRef<string | null>(null);

  // Seizure timer: tick while running.
  useEffect(() => {
    if (startedAt == null || stoppedMs != null) return;
    const id = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(id);
  }, [startedAt, stoppedMs]);

  const save = async (body: Record<string, unknown>) => {
    if (!canTakeActions(currentCaregiver?.role)) return;
    requestId.current ??= newRequestId();
    setNotSent(false);
    try {
      const res = await fetchApi<Saved>("/care/incidents", { method: "POST", body: JSON.stringify({ ...body, client_request_id: requestId.current }) });
      setSaved(res);
    } catch {
      setNotSent(true); // the steps stay on screen; try again below
    }
  };

  const answerFall = (key: FallKey, yes: boolean) => {
    const next = { ...answers, [key]: yes };
    setAnswers(next);
    if (FALL_QUESTIONS.every((q) => next[q.key] !== undefined)) save({ kind: "fall", ...next });
  };

  const calls = (onRed: boolean) => (
    <div className="grid grid-cols-1 gap-2">
      <a
        href={`tel:${contacts.emergency}`}
        data-testid="call-emergency"
        className={`flex min-h-16 items-center justify-center gap-2 rounded-2xl text-xl font-black active:scale-95 ${
          onRed ? "bg-white text-rose-800" : "bg-rose-700 text-white"
        }`}
      >
        <Siren className="h-7 w-7" />
        <span>{ar ? "اتصل بالإسعاف" : "Call emergency"}</span>
        <Ltr>{contacts.emergency}</Ltr>
      </a>
      {contacts.doctor_phone && (
        <a
          href={`tel:${contacts.doctor_phone}`}
          className={`flex min-h-12 items-center justify-center gap-2 rounded-2xl border-2 text-lg font-bold ${
            onRed ? "border-white text-white" : "border-slate-400 text-slate-900 dark:border-slate-500 dark:text-slate-100"
          }`}
        >
          <Phone className="h-5 w-5" />
          <bdi>{contacts.doctor_name || (ar ? "الطبيب" : "Doctor")}</bdi>
        </a>
      )}
    </div>
  );

  const back = (
    <Link href="/" className="inline-flex min-h-12 items-center gap-1 text-base font-bold underline">
      <ArrowLeft className="h-5 w-5 rtl:rotate-180" />
      {ar ? "الرئيسية" : "Home"}
    </Link>
  );
  const notSentLine = notSent && (
    <p role="alert" className="rounded-xl bg-white p-3 text-base font-bold text-rose-800">
      {ar ? "لم يُحفظ بعد (لا اتصال). اتبع الخطوات، وسيُرسل عند المحاولة مرة أخرى." : "Not saved yet (no connection). Follow the steps; try saving again."}{" "}
      <button
        type="button"
        className="min-h-12 underline"
        onClick={() =>
          flow === "fall" ? save({ kind: "fall", ...answers }) : save({ kind: "seizure", duration_seconds: Math.max(1, Math.round((stoppedMs ?? 0) / 1000)) })
        }
      >
        {ar ? "حاول الحفظ مرة أخرى" : "Try saving again"}
      </button>
    </p>
  );

  // 1. Choose.
  if (flow === "choose") {
    return (
      <main className="min-h-dvh bg-slate-50 px-4 pb-28 pt-6 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
        <div className="mx-auto flex max-w-md flex-col gap-4">
          <h1 className="text-2xl font-black">{ar ? "ماذا حدث؟" : "What happened?"}</h1>
          <button
            type="button"
            onClick={() => setFlow("fall")}
            data-testid="choose-fall"
            className="min-h-[28vh] rounded-3xl border-4 border-amber-600 bg-amber-50 text-3xl font-black text-amber-950 active:scale-95 dark:bg-amber-950 dark:text-amber-50"
          >
            {ar ? "سقطت" : "Fall"}
          </button>
          <button
            type="button"
            onClick={() => setFlow("seizure")}
            data-testid="choose-seizure"
            className="min-h-[28vh] rounded-3xl border-4 border-rose-700 bg-rose-50 text-3xl font-black text-rose-900 active:scale-95 dark:bg-rose-950 dark:text-rose-50"
          >
            {ar ? "نوبة تشنج (رجفة)" : "Seizure (shaking)"}
          </button>
          {calls(false)}
          {back}
        </div>
      </main>
    );
  }

  // 2. Fall: three yes/no questions, then what to do.
  if (flow === "fall") {
    const next = FALL_QUESTIONS.find((q) => answers[q.key] === undefined);
    if (next) {
      return (
        <main className="min-h-dvh bg-slate-50 px-4 pb-28 pt-6 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
          <div className="mx-auto flex max-w-md flex-col gap-4">
            <p className="text-sm font-bold">{ar ? "سقوط" : "Fall"} · <Ltr>{FALL_QUESTIONS.indexOf(next) + 1}/3</Ltr></p>
            <h1 className="text-3xl font-black leading-tight">{ar ? next.ar : next.en}</h1>
            <div className="grid min-h-[45vh] grid-cols-2 gap-3">
              <button type="button" onClick={() => answerFall(next.key, true)} className="rounded-3xl bg-slate-800 text-3xl font-black text-white active:scale-95 dark:bg-slate-200 dark:text-slate-900">
                {ar ? "نعم" : "Yes"}
              </button>
              <button type="button" onClick={() => answerFall(next.key, false)} className="rounded-3xl border-4 border-slate-800 text-3xl font-black text-slate-900 active:scale-95 dark:border-slate-200 dark:text-slate-100">
                {ar ? "لا" : "No"}
              </button>
            </div>
            {calls(false)}
          </div>
        </main>
      );
    }
    const escalate = !!answers.hit_head || answers.awake_normal === false || !!answers.pain_or_bleeding;
    const thinners = saved?.blood_thinners ?? [];
    const reason = [
      answers.hit_head && (ar ? "ضربت رأسها" : "hit her head"),
      answers.awake_normal === false && (ar ? "ليست واعية أو كلامها غير طبيعي" : "not awake or talking normally"),
      answers.pain_or_bleeding && (ar ? "ألم جديد أو لا تحرك طرفاً أو نزيف" : "new pain, a limb that won't move, or bleeding"),
    ].filter(Boolean);
    if (escalate) {
      return (
        <main data-testid="fall-emergency" className="min-h-dvh bg-rose-700 px-4 pb-28 pt-6 text-white">
          <div className="mx-auto flex max-w-md flex-col gap-4">
            <h1 className="text-3xl font-black leading-tight">{ar ? "اتصل بالإسعاف الآن" : "Call emergency now"}</h1>
            {calls(true)}
            <section className="rounded-2xl bg-rose-900/40 p-4 text-lg font-bold">
              <p>{ar ? "قل للمسعف:" : "Tell the dispatcher:"}</p>
              <p data-testid="dispatcher-line" className="text-xl font-black">
                {ar ? "سقطت" : "She fell"}
                {reason.length > 0 && <>, {reason.join(ar ? "، " : ", ")}</>}
                {thinners.length > 0 && (
                  <>
                    {ar ? "، وتأخذ مميّعاً للدم: " : ", on a blood thinner: "}
                    <bdi>{thinners.join(", ")}</bdi>
                  </>
                )}
              </p>
            </section>
            <p className="text-base font-bold">{ar ? "لا ترفعها وحدك. أبقها دافئة ولا تعطها طعاماً أو شراباً." : "Don't lift her alone. Keep her warm; nothing to eat or drink."}</p>
            {notSentLine}
            {back}
          </div>
        </main>
      );
    }
    return (
      <main data-testid="fall-calm" className="min-h-dvh bg-slate-50 px-4 pb-28 pt-6 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
        <div className="mx-auto flex max-w-md flex-col gap-4">
          <h1 className="text-3xl font-black leading-tight">{ar ? "لا ترفعها وحدك" : "Don't lift her alone"}</h1>
          <p className="text-lg font-bold">{ar ? "اطلب مساعدة لرفعها. أعد الاطمئنان عليها بعد 15 دقيقة." : "Get help to lift her. Check on her again in 15 minutes."}</p>
          <p className="rounded-2xl bg-amber-100 p-4 text-base font-bold text-amber-950 dark:bg-amber-950 dark:text-amber-50">
            {ar
              ? "خلال الـ 24 ساعة القادمة: كل 4 ساعات تأكد أنها واعية وتتكلم بشكل طبيعي. إن تغيّر ذلك، اتصل بالإسعاف."
              : "For the next 24 hours: every 4 hours, check she is awake and talking normally. If that changes, call emergency."}
          </p>
          {saved && <p className="text-base font-semibold">{ar ? "حُفظ وأُبلغت العائلة." : "Saved, and the family was told."}</p>}
          {notSentLine}
          {calls(false)}
          {back}
        </div>
      </main>
    );
  }

  // 3. Seizure: a big timer.
  const running = startedAt != null && stoppedMs == null;
  const elapsed = stoppedMs ?? (startedAt != null ? now - startedAt : 0);
  const red = elapsed >= SEIZURE_EMERGENCY_MS || !!saved?.escalate;
  return (
    <main data-testid="seizure" data-red={red ? "true" : "false"} className={`min-h-dvh px-4 pb-28 pt-6 ${red ? "bg-rose-700 text-white" : "bg-slate-50 text-slate-900 dark:bg-slate-950 dark:text-slate-100"}`}>
      <div className="mx-auto flex max-w-md flex-col gap-4">
        {red && <h1 className="text-3xl font-black leading-tight">{ar ? "اتصل بالإسعاف الآن" : "Call emergency now"}</h1>}
        {!red && <h1 className="text-2xl font-black">{ar ? "نوبة تشنج" : "Seizure"}</h1>}
        <ol className={`list-decimal space-y-1 ps-6 text-lg font-bold ${red ? "" : "text-slate-800 dark:text-slate-100"}`}>
          <li>{ar ? "اقلبها على جنبها." : "Turn her on her side."}</li>
          <li>{ar ? "لا تضع شيئاً في فمها." : "Nothing in her mouth."}</li>
          <li>{ar ? "أبعد الأشياء الصلبة عنها، ولا تمسكها بقوة." : "Move hard things away; don't hold her down."}</li>
        </ol>
        {startedAt == null ? (
          <button
            type="button"
            onClick={() => {
              const t = Date.now(); // on tap, not during render
              setStartedAt(t);
              setNow(t);
            }}
            data-testid="start-timer"
            className="min-h-[35vh] rounded-3xl bg-rose-700 text-4xl font-black text-white active:scale-95"
          >
            {ar ? "ابدأ المؤقت" : "Start timer"}
          </button>
        ) : (
          <div className="flex flex-col items-center gap-3">
            <p data-testid="seizure-timer" className="text-7xl font-black tabular-nums" aria-live="polite">
              <Ltr>{mmss(elapsed)}</Ltr>
            </p>
            {running ? (
              <button
                type="button"
                onClick={() => {
                  const ms = Date.now() - startedAt;
                  setStoppedMs(ms);
                  save({ kind: "seizure", duration_seconds: Math.max(1, Math.round(ms / 1000)) });
                }}
                data-testid="stop-timer"
                className={`min-h-24 w-full rounded-3xl text-3xl font-black active:scale-95 ${red ? "bg-white text-rose-800" : "bg-slate-800 text-white dark:bg-slate-200 dark:text-slate-900"}`}
              >
                {ar ? "توقفت" : "Stopped"}
              </button>
            ) : (
              <p className="text-lg font-bold">
                {ar ? "استمرت" : "It lasted"} <Ltr>{mmss(elapsed)}</Ltr>.{" "}
                {saved?.repeat ? (ar ? "هذه النوبة الثانية خلال يوم: اتصل بالإسعاف." : "This is the second one within a day: call emergency.") : ""}
                {saved && !saved.escalate && (ar ? " حُفظت وأُبلغت العائلة. ابقَ معها حتى تستعيد وعيها." : " Saved, and the family was told. Stay with her until she is fully awake.")}
              </p>
            )}
          </div>
        )}
        {calls(red)}
        {notSentLine}
        {back}
      </div>
    </main>
  );
}
