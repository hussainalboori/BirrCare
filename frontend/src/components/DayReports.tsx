"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { canTakeActions, isViewer } from "@/lib/permissions";
import { firstName } from "@/lib/actor";
import { TAG_CHOICES, isUrgentTag, tagLabel } from "@/lib/handoverTags";
import { enqueue, isNetworkError, newRequestId, OUTBOX_RESULT_EVENT } from "@/lib/outbox";
import { formatTime, localDateString } from "@/lib/datetime";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import { VoiceNoteButton } from "@/components/VoiceNoteButton";
import { Ltr } from "@/components/Ltr";
import type { HandoverNote, SSEEvent } from "@/lib/types";
import { AlertTriangle, ArrowRightLeft, Calendar, Check, ClipboardList, Loader2, Send } from "lucide-react";

/** Notes sent from this page are stored as handover notes of this kind. */
export const REPORT_KIND = "report";

/**
 * The family reports things as they happen, several times a day, instead of
 * one handover at the end of a shift. Every report of the chosen day is listed
 * below the form, newest first, and updates live on every phone.
 */
export function DayReports() {
  const { locale } = useLanguage();
  const { currentCaregiver, openPinModal } = useCaregiver();
  const ar = locale === "ar";

  const [date, setDate] = useState<string>(() => localDateString());
  const [today, setToday] = useState<string>(() => localDateString());
  const [notes, setNotes] = useState<HandoverNote[] | null>(null);

  const [tags, setTags] = useState<string[]>([]);
  const [summary, setSummary] = useState("");
  const [urgentFlag, setUrgentFlag] = useState(false);
  const [sending, setSending] = useState(false);
  const inFlightRef = useRef(false);
  const [formError, setFormError] = useState("");
  const [sentMessage, setSentMessage] = useState("");
  const tagUrgent = tags.some(isUrgentTag);

  const load = useCallback(async () => {
    const list = await fetchApi<HandoverNote[] | null>(`/handovers?date=${date}`).catch(() => undefined);
    if (list !== undefined) setNotes(list || []);
  }, [date]);

  useEffect(() => {
    if (!currentCaregiver) return;
    load();
    const onEvent = (e: Event) => {
      const type = (e as CustomEvent<SSEEvent>).detail?.type;
      if (type === "NOTE_ADDED" || type === "DUTY_PASSED" || type === "DUTY_ACCEPTED") load();
    };
    // A new day starts while the page is open: follow it if "today" was shown.
    const onFocus = () => {
      const now = localDateString();
      if (now === today) return;
      setDate((d) => (d === today ? now : d));
      setToday(now);
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    window.addEventListener(RESYNC_EVENT, load);
    window.addEventListener(OUTBOX_RESULT_EVENT, load);
    window.addEventListener("focus", onFocus);
    return () => {
      window.removeEventListener("birrcare:sse_event", onEvent);
      window.removeEventListener(RESYNC_EVENT, load);
      window.removeEventListener(OUTBOX_RESULT_EVENT, load);
      window.removeEventListener("focus", onFocus);
    };
  }, [currentCaregiver, load, today]);

  const toggleTag = (key: string) => setTags((prev) => (prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError("");
    setSentMessage("");
    if (!summary.trim() && tags.length === 0) {
      setFormError(ar ? "اختر شيئاً واحداً على الأقل أو اكتب ما حدث" : "Tap at least one item or write what happened");
      return;
    }
    if (!currentCaregiver) {
      openPinModal();
      return;
    }
    if (!canTakeActions(currentCaregiver.role)) {
      setFormError(ar ? "حسابك للمشاهدة فقط" : "Your account is view-only");
      return;
    }
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    setSending(true);
    const note = { shift_type: REPORT_KIND, summary: summary.trim(), urgent_flag: urgentFlag || tagUrgent, tags };
    const requestId = newRequestId();
    let queued = false;
    try {
      try {
        await fetchApi("/handovers", { method: "POST", body: JSON.stringify({ ...note, client_request_id: requestId }) });
      } catch (err) {
        if (!isNetworkError(err)) throw err;
        await enqueue({
          id: requestId,
          kind: "note",
          endpoint: "/handovers",
          body: note,
          caregiver_id: currentCaregiver.id,
          caregiver_name: currentCaregiver.name,
          label_ar: "بلاغ",
          label_en: "report",
        });
        queued = true;
      }
      setTags([]);
      setSummary("");
      setUrgentFlag(false);
      setSentMessage(
        queued
          ? ar ? "حُفظ البلاغ على هذا الجهاز ولم يُرسل بعد — سيُرسل عند عودة الاتصال." : "Saved on this phone, not sent yet — it will be sent when the connection is back."
          : ar ? "أُرسل البلاغ للعائلة. أرسل غيره متى ما حدث شيء." : "Report sent to the family. Send another whenever something happens.",
      );
      if (date !== today) setDate(today);
      else load();
    } catch (err) {
      setFormError((err as Error).message || (ar ? "تعذر الإرسال" : "Could not send"));
    } finally {
      inFlightRef.current = false;
      setSending(false);
    }
  };

  const isToday = date === today;
  const count = notes?.length ?? 0;

  return (
    <>
      {!isViewer(currentCaregiver?.role) && (
        <form
          onSubmit={submit}
          data-testid="report-form"
          className="mb-6 rounded-3xl border border-slate-200 bg-white p-4 shadow-lg sm:p-5 dark:border-slate-800 dark:bg-slate-900"
        >
          <h2 className="text-base font-bold text-slate-900 dark:text-white">{ar ? "بلّغ عمّا يحدث الآن" : "Report what's happening now"}</h2>
          <p className="mb-3 mt-1 text-xs text-slate-600 dark:text-slate-300">
            {ar ? "أرسل بلاغاً كلما حدث شيء — لا تنتظر نهاية اليوم. كل البلاغات تظهر للعائلة تحت." : "Send a report each time something happens — don't wait for the end of the day. Every report shows below for the family."}
          </p>

          <div className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
            {TAG_CHOICES.map((tag) => {
              const on = tags.includes(tag.key);
              return (
                <button
                  type="button"
                  key={tag.key}
                  aria-pressed={on}
                  onClick={() => toggleTag(tag.key)}
                  className={`flex min-h-14 items-center justify-center gap-1.5 rounded-2xl border-2 px-2 text-center text-sm font-bold leading-tight transition-all active:scale-95 ${
                    on
                      ? tag.urgent
                        ? "border-rose-600 bg-rose-600 text-white"
                        : "border-emerald-600 bg-emerald-600 text-white"
                      : tag.urgent
                        ? "border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200"
                        : "border-slate-200 bg-slate-50 text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
                  }`}
                >
                  {on && <Check className="h-4 w-4 shrink-0" />}
                  <span>{ar ? tag.ar : tag.en}</span>
                </button>
              );
            })}
          </div>

          <textarea
            rows={3}
            value={summary}
            onChange={(e) => setSummary(e.target.value)}
            placeholder={ar ? "ماذا حدث؟ مثال: استفرغت بعد الريوق" : "What happened? e.g. vomited after breakfast"}
            aria-label={ar ? "ماذا حدث" : "What happened"}
            className="w-full rounded-2xl border border-slate-200 bg-slate-50 p-3 text-base text-slate-900 focus:border-emerald-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white dark:focus:bg-slate-900"
          />
          <div className="mb-4 mt-2 flex justify-end">
            <VoiceNoteButton ar={ar} onText={(text) => setSummary((prev) => (prev.trim() ? `${prev.trim()} ${text}` : text))} />
          </div>

          {tagUrgent ? (
            <p className="mb-4 flex items-center gap-1.5 rounded-xl bg-rose-50 p-3 text-sm font-bold text-rose-800 dark:bg-rose-950/40 dark:text-rose-200">
              <AlertTriangle className="h-4 w-4 shrink-0" />
              {ar ? "سيُعلَّم البلاغ كعاجل" : "This report will be marked urgent"}
            </p>
          ) : (
            <label className="mb-4 flex min-h-12 cursor-pointer items-center gap-2 rounded-xl border border-rose-200 bg-rose-50/50 p-3 dark:border-rose-900/60 dark:bg-rose-950/20">
              <input
                type="checkbox"
                checked={urgentFlag}
                onChange={(e) => setUrgentFlag(e.target.checked)}
                className="h-5 w-5 shrink-0 rounded-sm text-rose-600 focus:ring-rose-500"
              />
              <span className="flex items-center gap-1.5 text-sm font-bold text-rose-700 dark:text-rose-400">
                <AlertTriangle className="h-4 w-4" />
                {ar ? "عاجل — يظهر باللون الأحمر للعائلة" : "Urgent — shows in red for the family"}
              </span>
            </label>
          )}

          {formError && (
            <p role="alert" className="mb-3 rounded-xl bg-rose-50 p-3 text-sm font-bold text-rose-800 dark:bg-rose-950/40 dark:text-rose-200">
              {formError}
            </p>
          )}
          {sentMessage && (
            <p role="status" className="mb-3 rounded-xl bg-emerald-50 p-3 text-sm font-bold text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200">
              {sentMessage}
            </p>
          )}

          <button
            type="submit"
            disabled={sending}
            aria-busy={sending}
            className="flex min-h-14 w-full items-center justify-center gap-2 rounded-2xl bg-emerald-600 px-5 text-base font-black text-white shadow-sm transition-all hover:bg-emerald-700 active:scale-95 disabled:opacity-50"
          >
            {sending ? <Loader2 className="h-5 w-5 animate-spin" /> : <Send className="h-5 w-5" />}
            <span>
              {sending
                ? ar ? "جارٍ الإرسال…" : "Sending…"
                : currentCaregiver
                  ? ar ? `أرسل البلاغ باسم ${firstName(currentCaregiver.name)}` : `Send report as ${firstName(currentCaregiver.name)}`
                  : ar ? "أرسل البلاغ" : "Send report"}
            </span>
          </button>
        </form>
      )}

      <section data-testid="day-reports" className="mb-6">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-base font-bold text-slate-900 dark:text-white">
            {isToday ? (ar ? "بلاغات اليوم" : "Today's reports") : ar ? "بلاغات يوم" : "Reports on"}{" "}
            <span className="text-slate-500 dark:text-slate-400">
              (<Ltr>{count}</Ltr>)
            </span>
          </h2>
          <div className="flex items-center gap-2">
            {!isToday && (
              <button
                type="button"
                onClick={() => setDate(today)}
                className="min-h-11 rounded-xl border border-emerald-200 bg-emerald-50 px-3 text-sm font-bold text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300"
              >
                {ar ? "عودة لليوم" : "Back to today"}
              </button>
            )}
            <label className="flex items-center gap-2 rounded-2xl border border-slate-200 bg-white px-3 py-1 dark:border-slate-800 dark:bg-slate-900">
              <Calendar className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
              <input
                type="date"
                value={date}
                max={today}
                onChange={(e) => e.target.value && setDate(e.target.value)}
                aria-label={ar ? "اختر اليوم" : "Choose the day"}
                className="min-h-11 bg-transparent text-base font-bold text-slate-800 focus:outline-none dark:text-slate-200"
              />
            </label>
          </div>
        </div>

        {notes === null ? (
          <div className="h-24 w-full animate-pulse rounded-2xl bg-slate-200 dark:bg-slate-800" />
        ) : notes.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-slate-300 p-6 text-center dark:border-slate-700">
            <ClipboardList className="mx-auto mb-2 h-8 w-8 text-slate-400" />
            <p className="text-sm font-semibold text-slate-600 dark:text-slate-300">
              {isToday ? (ar ? "لا بلاغات اليوم بعد" : "No reports yet today") : ar ? "لا بلاغات في هذا اليوم" : "No reports on this day"}
            </p>
          </div>
        ) : (
          <ol className="space-y-3">
            {notes.map((n) => {
              const urgent = n.urgent_flag || (n.tags || []).some(isUrgentTag);
              return (
                <li
                  key={n.id}
                  data-testid="report-item"
                  className={`rounded-2xl border p-3 ${
                    urgent ? "border-rose-300 bg-rose-50 dark:border-rose-900 dark:bg-rose-950/40" : "border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900"
                  }`}
                >
                  <div className="mb-1.5 flex flex-wrap items-center gap-2">
                    <span className="rounded-lg bg-slate-100 px-2 py-0.5 text-sm font-black text-slate-900 dark:bg-slate-800 dark:text-white">
                      <Ltr>{formatTime(n.created_at, locale)}</Ltr>
                    </span>
                    <span
                      className="flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold text-white"
                      style={{ backgroundColor: n.avatar_color || "#059669" }}
                    >
                      {n.caregiver_name?.slice(0, 1)}
                    </span>
                    <bdi className="text-sm font-bold text-slate-800 dark:text-slate-200">{firstName(n.caregiver_name)}</bdi>
                    {n.shift_type !== REPORT_KIND && (
                      <span className="rounded-md bg-blue-50 px-2 py-0.5 text-xs font-bold text-blue-800 dark:bg-blue-950/60 dark:text-blue-300">
                        {ar ? "تسليم مناوبة" : "Shift handover"}
                      </span>
                    )}
                    {urgent && (
                      <span className="inline-flex items-center gap-1 rounded-full bg-rose-600 px-2.5 py-0.5 text-xs font-bold text-white">
                        <AlertTriangle className="h-3 w-3" />
                        {ar ? "عاجل" : "Urgent"}
                      </span>
                    )}
                  </div>
                  {n.tags && n.tags.length > 0 && (
                    <div className="mb-1.5 flex flex-wrap gap-1.5">
                      {n.tags.map((k) => (
                        <span
                          key={k}
                          className={`rounded-full px-2.5 py-1 text-xs font-bold ${
                            isUrgentTag(k) ? "bg-rose-100 text-rose-800 dark:bg-rose-950 dark:text-rose-200" : "bg-emerald-50 text-emerald-800 dark:bg-emerald-950/50 dark:text-emerald-200"
                          }`}
                        >
                          {tagLabel(k, ar)}
                        </span>
                      ))}
                    </div>
                  )}
                  {n.summary && <p className="whitespace-pre-wrap text-sm leading-relaxed text-slate-800 dark:text-slate-200">{n.summary}</p>}
                  {n.passed_to_name && (
                    <p className="mt-1.5 flex items-center gap-1.5 text-xs font-bold text-blue-800 dark:text-blue-300">
                      <ArrowRightLeft className="h-3.5 w-3.5" />
                      {ar ? "سُلّمت المناوبة إلى" : "Duty passed to"} <bdi>{firstName(n.passed_to_name)}</bdi>
                    </p>
                  )}
                </li>
              );
            })}
          </ol>
        )}
      </section>
    </>
  );
}
