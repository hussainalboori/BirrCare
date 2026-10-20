"use client";

import React, { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { canTakeActions } from "@/lib/permissions";
import { formatTime } from "@/lib/datetime";
import { useContacts } from "@/components/CrisisActions";
import { Ltr } from "@/components/Ltr";
import { Smile, Hand, MessageCircle, Siren, Phone, ArrowLeft, Clock } from "lucide-react";

// F.A.S.T. stroke check (issue #13). All three questions are on one screen so any
// "yes" reaches "Call emergency now" in two taps from Home. Calling never waits for
// the save. "Last seen normal" (the T in F.A.S.T.) is asked after, for the paramedics.

type Key = "face" | "arm" | "speech";
type Answers = Partial<Record<Key, boolean>>;
const RECHECK_MS = 15 * 60_000;
const nowMs = () => Date.now();

const QUESTIONS: { key: Key; icon: typeof Smile; ar: string; en: string; askAr: string; askEn: string }[] = [
  { key: "face", icon: Smile, ar: "الوجه", en: "Face", askAr: "اطلب منها أن تبتسم. هل يميل جانب من وجهها؟", askEn: "Ask her to smile. Does one side of her face droop?" },
  { key: "arm", icon: Hand, ar: "الذراعان", en: "Arms", askAr: "اطلب منها رفع الذراعين معاً. هل تنزل إحداهما؟", askEn: "Ask her to raise both arms. Does one arm drift down?" },
  { key: "speech", icon: MessageCircle, ar: "الكلام", en: "Speech", askAr: "اطلب منها تكرار جملة بسيطة. هل كلامها ثقيل أو غريب؟", askEn: "Ask her to repeat a simple sentence. Is her speech slurred or strange?" },
];

export default function FastPage() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const contacts = useContacts();
  const ar = locale === "ar";
  const [answers, setAnswers] = useState<Answers>({});
  const [result, setResult] = useState<"positive" | "negative" | null>(null);
  const [savedId, setSavedId] = useState<string | null>(null);
  const [lastNormal, setLastNormal] = useState<Date | null>(null);
  const [recheckAt, setRecheckAt] = useState<number | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const saving = useRef(false);

  useEffect(() => {
    if (!recheckAt) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [recheckAt]);

  const save = async (a: Answers) => {
    if (saving.current || !canTakeActions(currentCaregiver?.role)) return;
    saving.current = true;
    try {
      const res = await fetchApi<{ id: string }>("/vitals", {
        method: "POST",
        body: JSON.stringify({ vital_type: "fast", fast_result: { face: !!a.face, arm: !!a.arm, speech: !!a.speech } }),
      });
      setSavedId(res?.id ?? null);
    } catch {
      // Offline: the screen still says what to do; the answers stay on screen.
    } finally {
      saving.current = false;
    }
  };

  const answer = (key: Key, yes: boolean) => {
    const next = { ...answers, [key]: yes };
    setAnswers(next);
    if (yes) {
      setResult("positive");
      save(next);
    } else if (QUESTIONS.every((q) => next[q.key] === false)) {
      setResult("negative");
      const t = nowMs(); // on tap, not during render
      setRecheckAt(t + RECHECK_MS);
      setNow(t);
      save(next);
    }
  };

  const markLastNormal = async (at: Date) => {
    setLastNormal(at);
    if (savedId) {
      await fetchApi(`/vitals/${encodeURIComponent(savedId)}/last-normal`, {
        method: "PUT",
        body: JSON.stringify({ last_normal_at: at.toISOString() }),
      }).catch(() => {});
    }
  };

  const restart = () => {
    setAnswers({});
    setResult(null);
    setSavedId(null);
    setLastNormal(null);
    setRecheckAt(null);
  };

  const doctorLabel = contacts.doctor_name || (ar ? "الطبيب" : "the doctor");

  if (result === "positive") {
    const minutesAgo = [0, 30, 60, 120];
    return (
      <main data-testid="fast-emergency" className="min-h-dvh bg-rose-700 px-4 pb-28 pt-6 text-white">
        <div className="mx-auto max-w-md space-y-4">
          <h1 className="text-center text-3xl font-black leading-tight">
            {ar ? "قد تكون جلطة. اتصل بالإسعاف الآن" : "This may be a stroke. Call emergency now"}
          </h1>
          <a
            href={`tel:${contacts.emergency}`}
            className="flex min-h-24 items-center justify-center gap-3 rounded-3xl bg-white text-2xl font-black text-rose-800 shadow-2xl active:scale-95"
          >
            <Siren className="h-8 w-8" />
            <span>{ar ? "اتصل بالإسعاف" : "Call emergency"}</span>
            <Ltr>{contacts.emergency}</Ltr>
          </a>
          {contacts.doctor_phone && (
            <a
              href={`tel:${contacts.doctor_phone}`}
              className="flex min-h-14 items-center justify-center gap-2 rounded-2xl border-2 border-white text-lg font-bold text-white"
            >
              <Phone className="h-5 w-5" />
              <span>
                {ar ? "ثم اتصل بـ" : "Then call "}
                <bdi>{doctorLabel}</bdi>
              </span>
            </a>
          )}

          <section className="space-y-2 rounded-2xl bg-rose-900/40 p-4">
            <p className="flex items-center gap-2 text-lg font-bold">
              <Clock className="h-5 w-5" />
              {ar ? "متى رأيتها طبيعية آخر مرة؟" : "When did you last see her normal?"}
            </p>
            <p className="text-sm">{ar ? "أخبر المسعفين بهذا الوقت، فهو يحدد العلاج." : "Tell the paramedics this time; it decides the treatment."}</p>
            <div className="grid grid-cols-2 gap-2">
              {minutesAgo.map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => markLastNormal(new Date(Date.now() - m * 60_000))}
                  className="min-h-12 rounded-xl bg-white/15 text-base font-bold text-white"
                >
                  {m === 0 ? (ar ? "قبل قليل" : "Just now") : ar ? (m < 60 ? `قبل ${m} دقيقة` : m === 60 ? "قبل ساعة" : "قبل ساعتين") : m < 60 ? `${m} min ago` : `${m / 60} h ago`}
                </button>
              ))}
            </div>
            <label className="block text-sm font-semibold">
              {ar ? "أو اختر الوقت:" : "Or pick the time:"}
              <input
                type="time"
                className="mt-1 w-full rounded-xl px-3 py-2 text-base text-slate-900"
                onChange={(e) => {
                  const [h, mi] = e.target.value.split(":").map(Number);
                  if (Number.isNaN(h)) return;
                  const d = new Date();
                  d.setHours(h, mi, 0, 0);
                  if (d.getTime() > Date.now()) d.setDate(d.getDate() - 1);
                  markLastNormal(d);
                }}
              />
            </label>
            {lastNormal && (
              <p data-testid="last-normal" className="rounded-xl bg-white p-3 text-center text-xl font-black text-rose-800">
                {ar ? "آخر مرة طبيعية: " : "Last seen normal: "}
                <bdi dir="auto">{formatTime(lastNormal.toISOString(), locale)}</bdi>
              </p>
            )}
          </section>

          <button type="button" onClick={restart} className="min-h-12 w-full rounded-xl text-base font-semibold text-white/90 underline">
            {ar ? "أعد الفحص" : "Start the check again"}
          </button>
        </div>
      </main>
    );
  }

  if (result === "negative") {
    const left = Math.max(0, (recheckAt ?? 0) - now);
    const mm = Math.floor(left / 60_000);
    const ss = Math.floor((left % 60_000) / 1000);
    return (
      <main data-testid="fast-negative" className="mx-auto max-w-md space-y-4 px-4 pb-28 pt-6">
        <h1 className="text-2xl font-black text-slate-900 dark:text-white">{ar ? "لا توجد علامات جلطة واضحة الآن" : "No clear stroke signs right now"}</h1>
        <p className="text-base text-slate-700 dark:text-slate-300">
          {ar
            ? "أعد الفحص بعد 15 دقيقة. إذا ظهرت أي علامة في أي وقت، اتصل بالإسعاف فوراً."
            : "Check again in 15 minutes. If any sign appears at any time, call emergency at once."}
        </p>
        <p className="text-center text-5xl font-black text-slate-900 dark:text-white" aria-live="polite">
          <Ltr>
            {String(mm).padStart(2, "0")}:{String(ss).padStart(2, "0")}
          </Ltr>
        </p>
        <button type="button" onClick={restart} className="min-h-14 w-full rounded-2xl bg-slate-900 text-lg font-bold text-white dark:bg-white dark:text-slate-900">
          {ar ? "أعد الفحص الآن" : "Check again now"}
        </button>
        {contacts.doctor_phone && (
          <a
            href={`tel:${contacts.doctor_phone}`}
            className="flex min-h-14 items-center justify-center gap-2 rounded-2xl border-2 border-rose-700 text-lg font-bold text-rose-800 dark:text-rose-300"
          >
            <Phone className="h-5 w-5" />
            <span>
              {ar ? "اتصل بـ" : "Call "}
              <bdi>{doctorLabel}</bdi>
            </span>
          </a>
        )}
        <a href={`tel:${contacts.emergency}`} className="flex min-h-12 items-center justify-center gap-2 rounded-2xl bg-rose-700 text-base font-bold text-white">
          <Siren className="h-5 w-5" />
          {ar ? "اتصل بالإسعاف" : "Call emergency"} <Ltr>{contacts.emergency}</Ltr>
        </a>
      </main>
    );
  }

  return (
    <main className="mx-auto max-w-md space-y-3 px-4 pb-28 pt-4">
      <div className="flex items-center gap-2">
        <Link href="/" aria-label={ar ? "رجوع" : "Back"} className="flex min-h-12 min-w-12 items-center justify-center rounded-xl text-slate-600 dark:text-slate-300">
          <ArrowLeft className="h-5 w-5 rtl:rotate-180" />
        </Link>
        <h1 className="text-xl font-black text-slate-900 dark:text-white">{ar ? "فحص سريع للجلطة (F.A.S.T.)" : "Quick stroke check (F.A.S.T.)"}</h1>
      </div>
      <p className="text-base text-slate-600 dark:text-slate-300">{ar ? "أجب عن الثلاثة. أي «نعم» تعني: اتصل بالإسعاف." : "Answer all three. Any “yes” means: call emergency."}</p>

      {QUESTIONS.map((q) => {
        const Icon = q.icon;
        const a = answers[q.key];
        return (
          <section key={q.key} data-testid={`fast-${q.key}`} className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
            <div className="mb-3 flex items-start gap-3">
              <Icon className="h-10 w-10 shrink-0 text-rose-700 dark:text-rose-300" />
              <div>
                <p className="text-sm font-black uppercase text-slate-500">{ar ? q.ar : q.en}</p>
                <p className="text-lg font-bold leading-snug text-slate-900 dark:text-white">{ar ? q.askAr : q.askEn}</p>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() => answer(q.key, true)}
                className="min-h-16 rounded-2xl bg-rose-700 text-xl font-black text-white active:scale-95"
              >
                {ar ? "نعم" : "Yes"}
              </button>
              <button
                type="button"
                onClick={() => answer(q.key, false)}
                aria-pressed={a === false}
                className={`min-h-16 rounded-2xl text-xl font-black active:scale-95 ${
                  a === false ? "bg-emerald-700 text-white" : "bg-slate-100 text-slate-800 dark:bg-slate-800 dark:text-slate-100"
                }`}
              >
                {ar ? "لا" : "No"}
              </button>
            </div>
          </section>
        );
      })}
    </main>
  );
}
