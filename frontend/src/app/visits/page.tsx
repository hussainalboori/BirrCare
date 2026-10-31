"use client";

import React, { useCallback, useEffect, useState } from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { OPEN_MED_CATALOG_EVENT } from "@/components/CarePause";
import { VoiceNoteButton } from "@/components/VoiceNoteButton";
import { AppointmentCalendar, appointmentsOn, type CalendarState } from "@/components/AppointmentCalendar";
import { Ltr } from "@/components/Ltr";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLiveSync } from "@/hooks/useLiveSync";
import { fetchApi, type ApiError } from "@/lib/api";
import { newRequestId } from "@/lib/outbox";
import { canManageMedications, canTakeActions } from "@/lib/permissions";
import { householdToday } from "@/lib/householdTime";
import { formatShortDate } from "@/lib/datetime";
import {
  APPOINTMENT_KINDS,
  APPOINTMENTS_CHANGED,
  appointmentDay,
  icsHref,
  kindLabel,
  type Appointment,
  type AppointmentKind,
  type AppointmentList,
  type DoctorQuestion,
  type QuestionList,
} from "@/lib/appointments";
import type { SSEEvent } from "@/lib/types";
import { CalendarPlus } from "lucide-react";

// Visits & questions (issue #63): appointments with who takes her, an "Add to my
// calendar" file, and a running list of questions for the doctor that are closed
// after the visit with what the doctor said.

const inputClass =
  "mt-1 block min-h-12 w-full rounded-xl border border-slate-300 bg-white px-3 text-base text-slate-900 focus:bg-white dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100 dark:focus:bg-slate-950";
const labelClass = "block text-sm font-semibold text-slate-700 dark:text-slate-300";
const cardClass = "rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900";
const primaryBtn = "min-h-12 rounded-xl bg-emerald-700 px-4 text-base font-bold text-white disabled:opacity-60";
const secondaryBtn =
  "min-h-12 rounded-xl border border-slate-300 bg-white px-3 text-base font-bold text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100";

interface Draft {
  kind: AppointmentKind;
  title: string;
  date: string;
  time: string;
  location: string;
  notes: string;
  assigned_caregiver_id: string;
}

const VIEW_KEY = "birrcare_visits_view";

const emptyDraft = (): Draft => ({ kind: "doctor", title: "", date: householdToday(), time: "10:00", location: "", notes: "", assigned_caregiver_id: "" });

export default function VisitsPage() {
  const { status: connectionStatus } = useLiveSync();
  const { locale } = useLanguage();
  const { currentCaregiver, allCaregivers } = useCaregiver();
  const ar = locale === "ar";
  const role = currentCaregiver?.role;
  const canEdit = canTakeActions(role);

  const [appts, setAppts] = useState<AppointmentList | null>(null);
  const [questions, setQuestions] = useState<QuestionList | null>(null);
  const [editing, setEditing] = useState<{ id: string | null; draft: Draft } | null>(null);
  const [formMsg, setFormMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState<string | null>(null);
  const [afterVisit, setAfterVisit] = useState<Appointment | null>(null);
  const [medsAnswer, setMedsAnswer] = useState<"" | "no" | "yes">("");
  const [newQuestion, setNewQuestion] = useState("");
  const [questionMsg, setQuestionMsg] = useState("");
  const [answering, setAnswering] = useState<{ id: string; text: string } | null>(null);
  const [showHistory, setShowHistory] = useState(false);
  // null = the list; otherwise the month on show. Remembered on this phone.
  const [cal, setCal] = useState<CalendarState | null>(null);

  const load = useCallback(async () => {
    const [a, q] = await Promise.all([
      fetchApi<AppointmentList>("/appointments").catch(() => null),
      fetchApi<QuestionList>("/doctor-questions").catch(() => null),
    ]);
    if (a && Array.isArray(a.upcoming)) setAppts(a);
    if (q && Array.isArray(q.open)) setQuestions(q);
  }, []);
  useEffect(() => {
    load();
    const onEvent = (e: Event) => {
      if (APPOINTMENTS_CHANGED.has((e as CustomEvent<SSEEvent>).detail?.type ?? "")) load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    return () => window.removeEventListener("birrcare:sse_event", onEvent);
  }, [load]);

  const openCalendar = (on: boolean) => {
    const today = householdToday();
    setCal(on ? { month: today.slice(0, 7), today, selected: today } : null);
    try {
      localStorage.setItem(VIEW_KEY, on ? "calendar" : "list");
    } catch {}
  };
  useEffect(() => {
    let saved: string | null = null;
    try {
      saved = localStorage.getItem(VIEW_KEY);
    } catch {}
    if (saved === "calendar") openCalendar(true);
  }, []);

  const notSent = ar ? "لم يُحفظ. تحقق من الاتصال وحاول مرة أخرى." : "Not saved. Check the connection and try again.";
  // The sign-in list; the server refuses a view-only member ("INVALID_CAREGIVER").
  const takers = allCaregivers ?? [];

  const saveAppointment = async () => {
    if (!editing) return;
    const d = editing.draft;
    if (!d.title.trim()) {
      setFormMsg(ar ? "اكتب عنواناً قصيراً، مثل: متابعة الأعصاب." : "Write a short title, e.g. Neurology follow-up.");
      return;
    }
    setBusy(true);
    setFormMsg("");
    try {
      await fetchApi<Appointment>(editing.id ? `/appointments/${encodeURIComponent(editing.id)}` : "/appointments", {
        method: editing.id ? "PUT" : "POST",
        body: JSON.stringify({ ...d, client_request_id: editing.id ? undefined : newRequestId() }),
      });
      setEditing(null);
      load();
    } catch (e) {
      const err = e as ApiError;
      const code = err.data?.error;
      setFormMsg(
        code === "INVALID_DATE"
          ? ar ? "التاريخ غير صحيح أو في الماضي." : "The date is wrong or in the past."
          : code === "INVALID_CAREGIVER"
          ? ar ? "اختر فرداً من العائلة يستطيع أخذها." : "Choose a family member who can take her."
          : code === "INVALID_TIME"
          ? ar ? "الوقت غير صحيح." : "The time is wrong."
          : err.status ? err.message : notSent,
      );
    } finally {
      setBusy(false);
    }
  };

  const setStatus = async (a: Appointment, status: "done" | "cancelled") => {
    try {
      await fetchApi<Appointment>(`/appointments/${encodeURIComponent(a.id)}/status`, { method: "POST", body: JSON.stringify({ status }) });
      setConfirmCancel(null);
      if (status === "done") {
        setAfterVisit(a);
        setMedsAnswer("");
      }
      load();
    } catch {
      setFormMsg(notSent);
    }
  };

  const addQuestion = async () => {
    const text = newQuestion.trim();
    if (!text) return;
    setQuestionMsg("");
    try {
      await fetchApi<DoctorQuestion>("/doctor-questions", { method: "POST", body: JSON.stringify({ text, client_request_id: newRequestId() }) });
      setNewQuestion("");
      load();
    } catch {
      setQuestionMsg(notSent);
    }
  };

  const answerQuestion = async () => {
    if (!answering?.text.trim()) return;
    setQuestionMsg("");
    try {
      await fetchApi(`/doctor-questions/${encodeURIComponent(answering.id)}/answer`, {
        method: "POST",
        body: JSON.stringify({ answer: answering.text.trim(), appointment_id: afterVisit?.id }),
      });
      setAnswering(null);
      load();
    } catch {
      setQuestionMsg(notSent);
    }
  };

  const removeQuestion = async (id: string) => {
    try {
      await fetchApi(`/doctor-questions/${encodeURIComponent(id)}`, { method: "DELETE" });
      load();
    } catch {
      setQuestionMsg(notSent);
    }
  };

  const takesHer = (a: Appointment) =>
    a.assigned_name ? (
      ar ? (
        <>
          يأخذها <bdi>{a.assigned_name}</bdi>
        </>
      ) : (
        <>
          <bdi>{a.assigned_name}</bdi> takes her
        </>
      )
    ) : ar ? (
      "لم يُحدَّد من يأخذها"
    ) : (
      "Nobody is set to take her yet"
    );

  const openQuestions = questions?.open ?? [];

  const questionRow = (q: DoctorQuestion) => (
    <li key={q.id} data-testid="open-question" className="rounded-xl border border-slate-200 p-3 dark:border-slate-700">
      <p className="text-base font-bold text-slate-900 dark:text-slate-100">
        <bdi>{q.text}</bdi>
      </p>
      <p className="text-sm text-slate-600 dark:text-slate-400">
        {q.by && <bdi>{q.by}</bdi>} · <bdi>{formatShortDate(q.created_at, locale)}</bdi>
      </p>
      {canEdit &&
        (answering?.id === q.id ? (
          <div className="mt-2 space-y-2">
            <label className={labelClass}>
              {ar ? "قال الطبيب…" : "Doctor said…"}
              <textarea
                value={answering.text}
                onChange={(e) => setAnswering({ id: q.id, text: e.target.value })}
                rows={2}
                data-testid="answer-input"
                className={`${inputClass} py-2`}
              />
            </label>
            <div className="grid grid-cols-2 gap-2">
              <button type="button" onClick={answerQuestion} className={primaryBtn} data-testid="save-answer">
                {ar ? "حفظ الإجابة" : "Save answer"}
              </button>
              <button type="button" onClick={() => setAnswering(null)} className={secondaryBtn}>
                {ar ? "إلغاء" : "Cancel"}
              </button>
            </div>
          </div>
        ) : (
          <div className="mt-2 flex flex-wrap gap-2">
            <button type="button" onClick={() => setAnswering({ id: q.id, text: "" })} className={secondaryBtn} data-testid="answer-question">
              {ar ? "قال الطبيب…" : "Doctor said…"}
            </button>
            {(q.created_by === currentCaregiver?.id || canManageMedications(role)) && (
              <button type="button" onClick={() => removeQuestion(q.id)} className={secondaryBtn}>
                {ar ? "حذف السؤال" : "Remove"}
              </button>
            )}
          </div>
        ))}
    </li>
  );

  const appointmentCard = (a: Appointment) => (
    <li key={a.id} data-testid="appointment" className="rounded-xl border border-slate-200 p-3 dark:border-slate-700">
      <p className="text-xs font-bold uppercase text-sky-800 dark:text-sky-300">{kindLabel(a.kind, ar)}</p>
      <p className="text-base font-black text-slate-900 dark:text-slate-100">
        <bdi>{a.title}</bdi>
      </p>
      <p className="text-base font-bold text-slate-800 dark:text-slate-200">
        <bdi>{appointmentDay(a.date, locale)}</bdi> <Ltr>{a.time}</Ltr>
      </p>
      {a.location && (
        <p className="text-sm text-slate-700 dark:text-slate-300">
          <bdi>{a.location}</bdi>
        </p>
      )}
      <p className="text-sm font-semibold text-slate-700 dark:text-slate-300">{takesHer(a)}</p>
      {a.notes && (
        <p className="mt-1 whitespace-pre-line text-sm text-slate-700 dark:text-slate-300">
          <bdi>{a.notes}</bdi>
        </p>
      )}
      <div className="mt-2 grid grid-cols-2 gap-2">
        <a href={icsHref(a.id)} download="birrcare-appointment.ics" className={`${secondaryBtn} flex items-center justify-center gap-1.5`} data-testid="add-to-calendar">
          <CalendarPlus className="h-5 w-5 shrink-0" />
          {ar ? "أضف لتقويمي" : "Add to my calendar"}
        </a>
        {canEdit && (
          <button type="button" onClick={() => setStatus(a, "done")} className={primaryBtn} data-testid="appointment-done">
            {ar ? "تمّ الموعد" : "It's done"}
          </button>
        )}
        {canEdit && (
          <button
            type="button"
            onClick={() =>
              setEditing({
                id: a.id,
                draft: { kind: a.kind, title: a.title, date: a.date, time: a.time, location: a.location ?? "", notes: a.notes ?? "", assigned_caregiver_id: a.assigned_caregiver_id ?? "" },
              })
            }
            className={secondaryBtn}
          >
            {ar ? "تعديل" : "Change"}
          </button>
        )}
        {canEdit &&
          (confirmCancel === a.id ? (
            <button type="button" onClick={() => setStatus(a, "cancelled")} className="min-h-12 rounded-xl bg-rose-700 px-3 text-base font-bold text-white">
              {ar ? "نعم، ألغِ الموعد" : "Yes, cancel it"}
            </button>
          ) : (
            <button type="button" onClick={() => setConfirmCancel(a.id)} className={secondaryBtn}>
              {ar ? "إلغاء الموعد" : "Cancel visit"}
            </button>
          ))}
      </div>
    </li>
  );

  return (
    <div className="min-h-screen">
      <CaregiverHeader connectionStatus={connectionStatus} />
      <main className="mx-auto max-w-3xl space-y-4 px-4 py-5 pb-28 sm:px-6">
        <h1 className="text-xl font-black text-slate-900 dark:text-white">{ar ? "المواعيد وأسئلة الطبيب" : "Visits & questions for the doctor"}</h1>

        {/* After a visit: close the questions, then "did any medicines change?" */}
        {afterVisit && (
          <section data-testid="after-visit" className="rounded-2xl border border-emerald-300 bg-emerald-50 p-4 text-emerald-950 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-50">
            <h2 className="text-base font-black">
              {ar ? "بعد الموعد: " : "After the visit: "}
              <bdi>{afterVisit.title}</bdi>
            </h2>
            {afterVisit.kind === "doctor" && (
              <>
                {openQuestions.length > 0 && (
                  <p className="mt-1 text-sm font-semibold">
                    {ar ? "اكتب ما قاله الطبيب عن كل سؤال أدناه." : "Write what the doctor said for each question below."}
                  </p>
                )}
                <p className="mt-2 text-base font-black">{ar ? "هل غيّر الطبيب أي دواء؟" : "Did the doctor change any medicines?"}</p>
                {medsAnswer === "" && (
                  <div className="mt-2 grid grid-cols-2 gap-2">
                    <button type="button" onClick={() => setMedsAnswer("yes")} className={primaryBtn} data-testid="meds-changed-yes">
                      {ar ? "نعم" : "Yes"}
                    </button>
                    <button type="button" onClick={() => setMedsAnswer("no")} className={secondaryBtn}>
                      {ar ? "لا" : "No"}
                    </button>
                  </div>
                )}
                {medsAnswer === "yes" &&
                  (canManageMedications(role) ? (
                    <button
                      type="button"
                      onClick={() => window.dispatchEvent(new Event(OPEN_MED_CATALOG_EVENT))}
                      className={`${primaryBtn} mt-2 w-full`}
                      data-testid="open-medicines"
                    >
                      {ar ? "افتح الأدوية وعدّلها (اختر من متى يبدأ التغيير)" : "Open medicines and change them (choose when it starts)"}
                    </button>
                  ) : (
                    <p className="mt-2 text-sm font-bold">{ar ? "اطلب من المسؤول تعديل الأدوية في التطبيق اليوم." : "Ask an admin to change the medicines in the app today."}</p>
                  ))}
              </>
            )}
            <button type="button" onClick={() => setAfterVisit(null)} className={`${secondaryBtn} mt-3 w-full`}>
              {ar ? "تم" : "Done"}
            </button>
          </section>
        )}

        {/* Questions for the doctor */}
        <section className={cardClass} data-testid="questions">
          <h2 className="text-base font-black text-slate-900 dark:text-slate-100">{ar ? "أسئلة للطبيب" : "Questions for the doctor"}</h2>
          <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">
            {ar ? "أضف السؤال متى خطر لك؛ تظهر الأسئلة أول صفحة في تقرير الطبيب." : "Add a question whenever you think of one; they are the first page of the doctor's report."}
          </p>
          {canEdit && (
            <form
              className="mt-3 flex gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                addQuestion();
              }}
            >
              <input
                value={newQuestion}
                onChange={(e) => setNewQuestion(e.target.value)}
                maxLength={500}
                placeholder={ar ? "مثال: هل تستطيع المشي بالمشاية وحدها؟" : "e.g. Can she walk with the frame alone?"}
                aria-label={ar ? "سؤال جديد للطبيب" : "New question for the doctor"}
                data-testid="question-input"
                className={`${inputClass} mt-0 min-w-0 flex-1`}
              />
              <VoiceNoteButton ar={ar} onText={(text) => setNewQuestion((prev) => (prev.trim() ? `${prev.trim()} ${text}` : text))} />
              <button type="submit" className={primaryBtn} data-testid="add-question">
                {ar ? "أضف" : "Add"}
              </button>
            </form>
          )}
          {questionMsg && (
            <p role="alert" className="mt-2 text-sm font-bold text-rose-700 dark:text-rose-300">
              {questionMsg}
            </p>
          )}
          {openQuestions.length > 0 ? (
            <ul className="mt-3 space-y-2">{openQuestions.map(questionRow)}</ul>
          ) : (
            <p className="mt-3 text-sm text-slate-600 dark:text-slate-400">{ar ? "لا أسئلة مفتوحة." : "No open questions."}</p>
          )}
          {(questions?.answered.length ?? 0) > 0 && (
            <div className="mt-3">
              <button type="button" onClick={() => setShowHistory((v) => !v)} aria-expanded={showHistory} className={`${secondaryBtn} w-full`}>
                {showHistory ? (ar ? "إخفاء ما قاله الطبيب" : "Hide what the doctor said") : ar ? `ما قاله الطبيب (${questions?.answered.length})` : `What the doctor said (${questions?.answered.length})`}
              </button>
              {showHistory && (
                <ul className="mt-2 space-y-2" data-testid="answered-questions">
                  {questions?.answered.map((q) => (
                    <li key={q.id} className="rounded-xl bg-slate-50 p-3 dark:bg-slate-800">
                      <p className="text-sm font-bold text-slate-900 dark:text-slate-100">
                        <bdi>{q.text}</bdi>
                      </p>
                      <p className="text-sm text-slate-800 dark:text-slate-200">
                        {ar ? "قال الطبيب: " : "Doctor said: "}
                        <bdi>{q.answer}</bdi>
                      </p>
                      <p className="text-xs text-slate-600 dark:text-slate-400">
                        {q.answered_by_name && <bdi>{q.answered_by_name}</bdi>} · <bdi>{formatShortDate(q.answered_at, locale)}</bdi>
                      </p>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </section>

        {/* Appointments */}
        <section className={cardClass}>
          <div className="flex items-center justify-between gap-2">
            <h2 className="text-base font-black text-slate-900 dark:text-slate-100">{ar ? "المواعيد القادمة" : "Coming up"}</h2>
            {canEdit && !editing && (
              <button type="button" onClick={() => setEditing({ id: null, draft: emptyDraft() })} className={primaryBtn} data-testid="add-appointment">
                {ar ? "أضف موعداً" : "Add appointment"}
              </button>
            )}
          </div>

          {editing && (
            <form
              data-testid="appointment-form"
              className="mt-3 space-y-3 rounded-xl border border-slate-200 p-3 dark:border-slate-700"
              onSubmit={(e) => {
                e.preventDefault();
                saveAppointment();
              }}
            >
              <div role="radiogroup" aria-label={ar ? "نوع الموعد" : "Kind"} className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                {APPOINTMENT_KINDS.map((k) => (
                  <button
                    key={k.key}
                    type="button"
                    role="radio"
                    aria-checked={editing.draft.kind === k.key}
                    onClick={() => setEditing({ ...editing, draft: { ...editing.draft, kind: k.key } })}
                    className={`min-h-12 rounded-xl border px-2 text-base font-bold ${
                      editing.draft.kind === k.key
                        ? "border-emerald-700 bg-emerald-700 text-white"
                        : "border-slate-300 bg-white text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
                    }`}
                  >
                    {ar ? k.ar : k.en}
                  </button>
                ))}
              </div>
              <label className={labelClass}>
                {ar ? "العنوان" : "Title"}
                <input
                  value={editing.draft.title}
                  onChange={(e) => setEditing({ ...editing, draft: { ...editing.draft, title: e.target.value } })}
                  maxLength={120}
                  placeholder={ar ? "مثال: متابعة الأعصاب" : "e.g. Neurology follow-up"}
                  data-testid="appointment-title"
                  className={inputClass}
                />
              </label>
              <div className="grid grid-cols-2 gap-2">
                <label className={labelClass}>
                  {ar ? "التاريخ" : "Date"}
                  <input
                    type="date"
                    value={editing.draft.date}
                    min={editing.id ? undefined : householdToday()}
                    onChange={(e) => setEditing({ ...editing, draft: { ...editing.draft, date: e.target.value } })}
                    data-testid="appointment-date"
                    className={inputClass}
                  />
                </label>
                <label className={labelClass}>
                  {ar ? "الوقت (بتوقيت البيت)" : "Time (home time)"}
                  <input
                    type="time"
                    value={editing.draft.time}
                    onChange={(e) => setEditing({ ...editing, draft: { ...editing.draft, time: e.target.value } })}
                    data-testid="appointment-time"
                    className={inputClass}
                  />
                </label>
              </div>
              <label className={labelClass}>
                {ar ? "المكان" : "Where"}
                <input
                  value={editing.draft.location}
                  onChange={(e) => setEditing({ ...editing, draft: { ...editing.draft, location: e.target.value } })}
                  maxLength={200}
                  className={inputClass}
                />
              </label>
              <label className={labelClass}>
                {ar ? "من يأخذها؟" : "Who takes her?"}
                <select
                  value={editing.draft.assigned_caregiver_id}
                  onChange={(e) => setEditing({ ...editing, draft: { ...editing.draft, assigned_caregiver_id: e.target.value } })}
                  data-testid="appointment-assignee"
                  className={inputClass}
                >
                  <option value="">{ar ? "لم يُحدَّد بعد" : "Not decided yet"}</option>
                  {takers.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className={labelClass}>
                {ar ? "ملاحظات" : "Notes"}
                <textarea
                  value={editing.draft.notes}
                  onChange={(e) => setEditing({ ...editing, draft: { ...editing.draft, notes: e.target.value } })}
                  rows={2}
                  placeholder={ar ? "مثال: صائمة من منتصف الليل" : "e.g. Fasting from midnight"}
                  className={`${inputClass} py-2`}
                />
              </label>
              <p className="text-sm text-slate-600 dark:text-slate-400">
                {ar ? "يصل تذكير لمن يأخذها مساء اليوم السابق وقبل الموعد بساعتين." : "Whoever takes her gets a reminder the evening before and 2 hours before."}
              </p>
              {formMsg && (
                <p role="alert" className="text-sm font-bold text-rose-700 dark:text-rose-300">
                  {formMsg}
                </p>
              )}
              <div className="grid grid-cols-2 gap-2">
                <button type="submit" disabled={busy} className={primaryBtn} data-testid="save-appointment">
                  {ar ? "حفظ" : "Save"}
                </button>
                <button type="button" onClick={() => setEditing(null)} className={secondaryBtn}>
                  {ar ? "إلغاء" : "Cancel"}
                </button>
              </div>
            </form>
          )}

          <div role="tablist" className="mt-3 grid grid-cols-2 gap-1 rounded-xl bg-slate-100 p-1 dark:bg-slate-800">
            {([false, true] as const).map((on) => (
              <button
                key={String(on)}
                type="button"
                role="tab"
                aria-selected={!!cal === on}
                data-testid={on ? "view-calendar" : "view-list"}
                onClick={() => openCalendar(on)}
                className={`min-h-12 rounded-lg text-base font-bold ${
                  !!cal === on ? "bg-white text-slate-900 shadow-sm dark:bg-slate-950 dark:text-white" : "text-slate-600 dark:text-slate-300"
                }`}
              >
                {on ? (ar ? "التقويم" : "Calendar") : ar ? "القائمة" : "List"}
              </button>
            ))}
          </div>

          {cal ? (
            <>
              <AppointmentCalendar appointments={[...(appts?.upcoming ?? []), ...(appts?.recent ?? [])]} state={cal} onChange={setCal} locale={locale} />
              {cal.selected && (
                <div data-testid="calendar-selected" className="mt-4">
                  <h3 className="text-base font-black text-slate-900 dark:text-slate-100">
                    <bdi>{appointmentDay(cal.selected, locale)}</bdi>
                  </h3>
                  {(() => {
                    const day = cal.selected;
                    const upcomingIds = new Set(appts?.upcoming.map((a) => a.id));
                    const list = appointmentsOn([...(appts?.upcoming ?? []), ...(appts?.recent ?? [])], day);
                    if (list.length === 0) {
                      return (
                        <div className="mt-2">
                          <p className="text-sm text-slate-600 dark:text-slate-400">{ar ? "لا مواعيد في هذا اليوم." : "No visits on this day."}</p>
                          {canEdit && !editing && day >= cal.today && (
                            <button
                              type="button"
                              onClick={() => setEditing({ id: null, draft: { ...emptyDraft(), date: day } })}
                              className={`${secondaryBtn} mt-2`}
                              data-testid="add-on-day"
                            >
                              {ar ? "أضف موعداً في هذا اليوم" : "Add a visit on this day"}
                            </button>
                          )}
                        </div>
                      );
                    }
                    return (
                      <ul className="mt-2 space-y-3">
                        {list.map((a) =>
                          upcomingIds.has(a.id) ? (
                            appointmentCard(a)
                          ) : (
                            <li key={a.id} className="rounded-xl border border-slate-200 p-3 text-sm text-slate-800 dark:border-slate-700 dark:text-slate-200">
                              <bdi className="font-bold">{a.title}</bdi> · <Ltr>{a.time}</Ltr> ·{" "}
                              {a.status === "done" ? (ar ? "تمّ" : "done") : ar ? "لم يُسجَّل" : "not marked"}
                            </li>
                          ),
                        )}
                      </ul>
                    );
                  })()}
                </div>
              )}
            </>
          ) : (
            <>
              {appts && appts.upcoming.length === 0 && !editing && (
                <p className="mt-3 text-sm text-slate-600 dark:text-slate-400">{ar ? "لا مواعيد قادمة." : "No appointments coming up."}</p>
              )}
              <ul className="mt-3 space-y-3">{appts?.upcoming.map(appointmentCard)}</ul>
            </>
          )}
        </section>

        {(appts?.recent.length ?? 0) > 0 && (
          <section className={cardClass}>
            <h2 className="text-base font-black text-slate-900 dark:text-slate-100">{ar ? "مواعيد سابقة" : "Earlier visits"}</h2>
            <ul className="mt-2 divide-y divide-slate-200 dark:divide-slate-800">
              {appts?.recent.map((a) => (
                <li key={a.id} className="py-2 text-sm text-slate-800 dark:text-slate-200">
                  <bdi className="font-bold">{a.title}</bdi> · <bdi>{appointmentDay(a.date, locale)}</bdi> <Ltr>{a.time}</Ltr> ·{" "}
                  {a.status === "done" ? (ar ? "تمّ" : "done") : a.status === "cancelled" ? (ar ? "أُلغي" : "cancelled") : ar ? "لم يُسجَّل" : "not marked"}
                </li>
              ))}
            </ul>
          </section>
        )}
      </main>
    </div>
  );
}
