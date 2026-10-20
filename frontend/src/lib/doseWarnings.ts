import type { Locale } from "./i18n";
import { formatDuration } from "./datetime";

const mins = (n: number, locale: Locale) => formatDuration(n * 60_000, locale);

/**
 * Copy for partial / vomited / refused doses (issue #9).
 *
 * Safety rule: the app NEVER suggests a re-dose or an amount. It only says what
 * happened and routes to the doctor or pharmacist. src/__tests__/doseWarnings.test.ts
 * enforces this for every string here.
 */
export const DOSE_COPY = {
  ar: {
    notFullyGiven: "لم تأخذها كاملة؟",
    whatHappened: "ماذا حدث؟",
    partial: "أخذت جزءاً فقط",
    refused: "رفضت أخذها",
    skipReasons: ["نائمة", "الطبيب أوقفه", "نفد الدواء"],
    badgePartial: "جرعة جزئية",
    badgeRefused: "رفضت",
    vomitedQuestion: "استفرغت بعدها؟",
    vomitedWhen: "بعد كم دقيقة من الجرعة؟",
    dontKnow: "لا أعرف",
    minutes: (n: number) => mins(n, "ar"),
    warnPartial: "⚠ أخذت جزءاً من الجرعة فقط. اتصل بالطبيب أو الصيدلي قبل إعطاء أي جرعة أخرى.",
    warnVomited: (m: number | null) =>
      `⚠ أُعطيت ثم استفرغت${m == null ? "" : ` بعد ${mins(m, "ar")}`}. اتصل بالطبيب أو الصيدلي قبل إعطاء أي جرعة أخرى.`,
    toastPartial: "سجّل جرعة جزئية",
    toastVomited: (m: number | null) => `سجّل أنها استفرغت${m == null ? "" : ` بعد ${mins(m, "ar")}`} من الجرعة`,
    toastRefused: "سجّل أنها رفضت الجرعة",
  },
  en: {
    notFullyGiven: "Not fully taken?",
    whatHappened: "What happened?",
    partial: "Took only part",
    refused: "Refused it",
    skipReasons: ["Asleep", "Doctor said hold", "Out of stock"],
    badgePartial: "Partial dose",
    badgeRefused: "Refused",
    vomitedQuestion: "Vomited after?",
    vomitedWhen: "How many minutes after the dose?",
    dontKnow: "Don't know",
    minutes: (n: number) => mins(n, "en"),
    warnPartial: "⚠ Only part of this dose was taken. Call the doctor or pharmacist before giving any more.",
    warnVomited: (m: number | null) =>
      `⚠ Given, then vomited${m == null ? "" : ` ${mins(m, "en")} later`}. Call the doctor or pharmacist before giving any more.`,
    toastPartial: "recorded a partial dose",
    toastVomited: (m: number | null) => `recorded vomiting${m == null ? "" : ` ${mins(m, "en")}`} after a dose`,
    toastRefused: "recorded a refused dose",
  },
} satisfies Record<Locale, unknown>;

export const VOMIT_MINUTES = [5, 10, 15, 30, 60];
