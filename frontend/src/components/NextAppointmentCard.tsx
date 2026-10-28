"use client";

import React, { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useLanguage } from "@/context/LanguageContext";
import { fetchApi } from "@/lib/api";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import { APPOINTMENTS_CHANGED, appointmentDay, type AppointmentList, type QuestionList } from "@/lib/appointments";
import type { SSEEvent } from "@/lib/types";
import { CalendarClock } from "lucide-react";
import { Ltr } from "./Ltr";

// Home card (issue #63): "Next: Neurology, Sun 10:00 — Hussain takes her", and
// how many questions are waiting for the doctor. One tap to Visits & questions.
export function NextAppointmentCard() {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const [appts, setAppts] = useState<AppointmentList | null>(null);
  const [questions, setQuestions] = useState<QuestionList | null>(null);

  const load = useCallback(async () => {
    const [a, q] = await Promise.all([
      fetchApi<AppointmentList>("/appointments").catch(() => null),
      fetchApi<QuestionList>("/doctor-questions").catch(() => null),
    ]);
    if (a && Array.isArray(a.upcoming)) setAppts(a); // offline or unexpected: keep what is shown
    if (q && Array.isArray(q.open)) setQuestions(q);
  }, []);
  useEffect(() => {
    load();
    const onEvent = (e: Event) => {
      if (APPOINTMENTS_CHANGED.has((e as CustomEvent<SSEEvent>).detail?.type ?? "")) load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    window.addEventListener(RESYNC_EVENT, load);
    return () => {
      window.removeEventListener("birrcare:sse_event", onEvent);
      window.removeEventListener(RESYNC_EVENT, load);
    };
  }, [load]);

  const next = appts?.upcoming[0];
  const open = questions?.open.length ?? 0;
  if (!appts && !questions) return null;

  return (
    <Link
      href="/visits"
      data-testid="next-appointment"
      className="mb-4 flex min-h-14 items-start gap-3 rounded-2xl border border-sky-200 bg-sky-50 px-4 py-3 text-sky-950 active:scale-[0.98] dark:border-sky-900 dark:bg-sky-950/60 dark:text-sky-50"
    >
      <CalendarClock className="mt-0.5 h-5 w-5 shrink-0 text-sky-700 dark:text-sky-300" />
      <span className="min-w-0 flex-1">
        {next ? (
          <span className="block text-base font-black">
            {ar ? "القادم: " : "Next: "}
            <bdi>{next.title}</bdi>{ar ? "، " : ", "}<bdi>{appointmentDay(next.date, locale)}</bdi> <Ltr>{next.time}</Ltr>
            <span className="block text-sm font-bold">
              {next.assigned_name ? (
                ar ? (
                  <>
                    يأخذها <bdi>{next.assigned_name}</bdi>
                  </>
                ) : (
                  <>
                    <bdi>{next.assigned_name}</bdi> takes her
                  </>
                )
              ) : ar ? (
                "لم يُحدَّد من يأخذها"
              ) : (
                "Nobody is set to take her yet"
              )}
            </span>
          </span>
        ) : (
          <span className="block text-base font-black">{ar ? "المواعيد وأسئلة الطبيب" : "Visits & questions for the doctor"}</span>
        )}
        <span className="block text-sm font-semibold" data-testid="open-questions-count">
          {open > 0
            ? ar
              ? `أسئلة للطبيب: ${open}`
              : `${open} question${open === 1 ? "" : "s"} for the doctor`
            : ar
            ? "لا أسئلة للطبيب بعد — أضف سؤالاً متى خطر لك"
            : "No questions for the doctor yet — add one when you think of it"}
        </span>
      </span>
    </Link>
  );
}
