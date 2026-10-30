"use client";

import { useState } from "react";
import { Eraser, Loader2 } from "lucide-react";
import { fetchApi } from "@/lib/api";
import { Ltr } from "./Ltr";

// "Start fresh" (Super Admin, Menu → Server): after the family has tried the
// app, remove the trial records so real use starts with a clean screen. People,
// medicines, settings, the roster ahead and upcoming visits stay; the server
// copies the database first. Other phones reload when it is done.

type Counts = Record<"doses" | "readings" | "alerts" | "notes" | "care" | "shifts" | "visits", number>;

const ROWS: { key: keyof Counts; en: string; ar: string }[] = [
  { key: "doses", en: "Doses given or skipped", ar: "الجرعات المسجلة" },
  { key: "readings", en: "Readings (pressure, sugar, tests)", ar: "القراءات (الضغط، السكر، التحاليل)" },
  { key: "alerts", en: "Warnings", ar: "التنبيهات" },
  { key: "notes", en: "Shift notes and handovers", ar: "ملاحظات المناوبات والتسليم" },
  { key: "care", en: "Care tracker (turns, fluids, bowel…)", ar: "متابعة الرعاية (التقليب، السوائل، الإخراج…)" },
  { key: "shifts", en: "Past shifts", ar: "المناوبات السابقة" },
  { key: "visits", en: "Past visits and answered questions", ar: "المواعيد السابقة والأسئلة المجاب عنها" },
];

// Typed to confirm, in either language.
const CONFIRM_WORDS = ["RESET", "مسح"];

export function ResetCard({ ar }: { ar: boolean }) {
  const [counts, setCounts] = useState<Counts | null>(null);
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [done, setDone] = useState<string | null>(null);

  const open = async () => {
    setMsg("");
    setBusy(true);
    try {
      const res = await fetchApi<{ counts: Counts }>("/admin/reset");
      setCounts(res.counts);
    } catch {
      setMsg(ar ? "تعذر التحميل، حاول مجدداً" : "Could not load, please try again");
    } finally {
      setBusy(false);
    }
  };

  const confirmed = CONFIRM_WORDS.includes(typed.trim().toUpperCase());

  const reset = async () => {
    setMsg("");
    setBusy(true);
    try {
      const res = await fetchApi<{ copy: string }>("/admin/reset", { method: "POST", body: JSON.stringify({ confirm: "start_fresh" }) });
      setDone(res.copy);
      setCounts(null);
      setTyped("");
    } catch {
      setMsg(ar ? "لم يُحذف شيء. تحقق من الاتصال وحاول مجدداً." : "Nothing was removed. Check the connection and try again.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <section data-testid="reset-card" className="text-slate-900 dark:text-white">
      <h3 className="flex items-center gap-2 text-base font-black">
        <Eraser className="h-5 w-5 shrink-0" />
        {ar ? "البدء من جديد" : "Start fresh"}
      </h3>
      <p className="mt-1 text-sm text-slate-600 dark:text-slate-300">
        {ar
          ? "بعد فترة التجربة: يحذف السجلات التجريبية والتنبيهات القديمة. يبقى الأشخاص ورموزهم، والأدوية ومواعيدها، والإعدادات."
          : "After trying the app: removes the trial records and old warnings. People and their PINs, medicines and their times, and settings stay."}
      </p>

      {done ? (
        <div data-testid="reset-done" className="mt-3 rounded-xl border border-emerald-300 bg-emerald-50 p-3 text-sm text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-100">
          <p className="font-bold">{ar ? "تم. بدأ التطبيق من جديد." : "Done. The app starts fresh."}</p>
          <p className="mt-1">
            {ar ? "نسخة من كل شيء قبل الحذف محفوظة على الخادم في: " : "A copy of everything from before is saved on the server in: "}
            <Ltr>{`data/${done}`}</Ltr>
          </p>
          <button type="button" onClick={() => window.location.reload()} className="mt-2 min-h-12 rounded-xl bg-emerald-700 px-4 text-base font-bold text-white">
            {ar ? "إعادة تحميل التطبيق" : "Reload the app"}
          </button>
        </div>
      ) : !counts ? (
        <button
          type="button"
          data-testid="reset-open"
          disabled={busy}
          onClick={open}
          className="mt-3 flex min-h-12 items-center gap-2 rounded-xl border-2 border-rose-300 px-4 text-base font-bold text-rose-700 disabled:opacity-50 dark:border-rose-800 dark:text-rose-300"
        >
          {busy && <Loader2 className="h-5 w-5 animate-spin" />}
          {ar ? "البدء من جديد…" : "Start fresh…"}
        </button>
      ) : (
        <div data-testid="reset-confirm" className="mt-3 rounded-xl border border-rose-300 bg-rose-50 p-3 text-sm text-rose-950 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-100">
          <p className="font-bold">{ar ? "سيُحذف:" : "This removes:"}</p>
          <ul className="mt-1 space-y-0.5">
            {ROWS.map((r) => (
              <li key={r.key} className="flex justify-between gap-3">
                <span>{ar ? r.ar : r.en}</span>
                <Ltr className="font-bold">{counts[r.key] ?? 0}</Ltr>
              </li>
            ))}
          </ul>
          <p className="mt-2 font-bold">{ar ? "يبقى:" : "This stays:"}</p>
          <p>
            {ar
              ? "الأشخاص ورموزهم، الأدوية والمخزون الحالي، المناوبات من اليوم فصاعداً، المواعيد القادمة والأسئلة المفتوحة للطبيب، الإعدادات، وسجل التدقيق."
              : "People and PINs, medicines and current stock, shifts from today on, upcoming visits and open questions for the doctor, settings, and the audit log."}
          </p>
          <p className="mt-2">
            {ar ? "يحفظ الخادم نسخة كاملة أولاً. راجع عدد الحبوب في المخزون بعدها." : "The server saves a full copy first. Recount the medicine stock afterwards."}
          </p>
          <label className="mt-3 block font-bold">
            {ar ? "اكتب " : "Type "}
            <Ltr>{ar ? "مسح" : "RESET"}</Ltr>
            {ar ? " للتأكيد" : " to confirm"}
            <input
              data-testid="reset-word"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              className="mt-1 block min-h-12 w-full rounded-xl border border-rose-300 bg-white px-3 text-base text-slate-900 dark:border-rose-800 dark:bg-slate-900 dark:text-slate-100"
            />
          </label>
          <div className="mt-3 flex flex-wrap gap-2">
            <button
              type="button"
              data-testid="reset-go"
              disabled={!confirmed || busy}
              onClick={reset}
              className="flex min-h-12 items-center gap-2 rounded-xl bg-rose-700 px-4 text-base font-bold text-white disabled:opacity-50"
            >
              {busy && <Loader2 className="h-5 w-5 animate-spin" />}
              {ar ? "احذف السجلات التجريبية" : "Remove the trial records"}
            </button>
            <button
              type="button"
              onClick={() => {
                setCounts(null);
                setTyped("");
              }}
              className="min-h-12 rounded-xl border border-slate-300 px-4 text-sm font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200"
            >
              {ar ? "إلغاء" : "Cancel"}
            </button>
          </div>
        </div>
      )}
      {msg && (
        <p role="alert" className="mt-2 text-sm font-bold text-rose-700 dark:text-rose-300">
          {msg}
        </p>
      )}
    </section>
  );
}
