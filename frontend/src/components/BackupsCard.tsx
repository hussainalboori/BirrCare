"use client";

import { useCallback, useEffect, useState, type ReactNode } from "react";
import { AlertTriangle, CheckCircle2, CloudUpload, Loader2, XCircle } from "lucide-react";
import { fetchApi } from "@/lib/api";
import { BackupSchedule, BackupStatus, fetchBackupStatus } from "@/lib/backups";
import { UPDATE_STATUS_EVENT } from "@/lib/updates";
import { Ltr } from "./Ltr";

// Off-site backups for the Super Admin (issue #51): when the last encrypted
// backup reached Google Drive (and the second copy), whether the weekly
// restore test passed, and a "Back up now" button.

const POLL_MS = 5000;

export function BackupsCard({ ar }: { ar: boolean }) {
  const [s, setS] = useState<BackupStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);
  // Backup times being edited; null = showing what the server has.
  const [draft, setDraft] = useState<BackupSchedule | null>(null);
  const [newTime, setNewTime] = useState("");
  const [savingTimes, setSavingTimes] = useState(false);
  const [timesMsg, setTimesMsg] = useState("");
  // Switching to "latest copy only" deletes older copies: confirm first.
  const [confirmLatest, setConfirmLatest] = useState(false);

  const load = useCallback(async () => {
    try {
      const next = await fetchBackupStatus();
      if (typeof next?.reason !== "string") throw new Error("bad status");
      setS(next);
      setLoadFailed(false);
      return next;
    } catch {
      setLoadFailed(true);
      return null;
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // While a backup runs, check every few seconds until it finishes.
  const pending = !!s?.pending || s?.status?.status === "running";
  useEffect(() => {
    if (!pending) return;
    const id = setInterval(async () => {
      const next = await load();
      if (next && !next.pending && next.status?.status !== "running") {
        window.dispatchEvent(new Event(UPDATE_STATUS_EVENT));
      }
    }, POLL_MS);
    return () => clearInterval(id);
  }, [pending, load]);

  const fmt = (iso?: string) => {
    if (!iso) return "";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    return new Intl.DateTimeFormat(ar ? "ar-u-nu-latn" : "en-GB", { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }).format(d);
  };

  const runNow = async () => {
    setBusy(true);
    setMsg("");
    try {
      setS(await fetchApi<BackupStatus>("/admin/backup/run", { method: "POST" }));
      window.dispatchEvent(new Event(UPDATE_STATUS_EVENT));
    } catch (err: unknown) {
      const code = (err as { data?: { error?: string } })?.data?.error;
      setMsg(code === "IN_PROGRESS" ? (ar ? "يوجد نسخ احتياطي قيد التشغيل الآن" : "A backup is already running") : ar ? "تعذر بدء النسخ الاحتياطي، حاول مجدداً" : "Could not start a backup, please try again");
      load();
    } finally {
      setBusy(false);
    }
  };

  const saveTimes = async () => {
    if (!draft) return;
    setSavingTimes(true);
    setTimesMsg("");
    try {
      setS(await fetchApi<BackupStatus>("/admin/backup/schedule", { method: "PUT", body: JSON.stringify(draft) }));
      setDraft(null);
      setConfirmLatest(false);
      setTimesMsg(ar ? "تم الحفظ" : "Saved");
    } catch {
      setTimesMsg(ar ? "تعذر الحفظ: اختر من 1 إلى 6 أوقات" : "Could not save: choose 1 to 6 times");
    } finally {
      setSavingTimes(false);
    }
  };

  const fmtTime = (iso?: string) => {
    if (!iso) return "";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    return new Intl.DateTimeFormat(ar ? "ar-u-nu-latn" : "en-GB", { hour: "2-digit", minute: "2-digit" }).format(d);
  };

  if (!s) {
    return (
      <p className="py-3 text-sm text-slate-500 dark:text-slate-400">
        {loadFailed ? (ar ? "تعذر قراءة حالة النسخ الاحتياطي" : "Could not read the backup status") : ar ? "جارٍ التحميل..." : "Loading..."}
      </p>
    );
  }

  const st = s.status;
  // Each name isolated on its own, so the list keeps its order in Arabic.
  const dests = (st?.destinations ?? []).map((d) => d.name).filter(Boolean);
  const names = dests.length
    ? dests.flatMap((n, i) => [i > 0 ? (ar ? " و " : " + ") : null, <bdi key={n}>{n}</bdi>]).filter((x) => x !== null)
    : null;
  const red = s.reason === "stale" || s.reason === "restore_failed" || s.reason === "invalid_grant";
  const amber = s.reason === "failed" || s.reason === "not_set_up";

  let headline: ReactNode;
  let detail: ReactNode = null;
  switch (s.reason) {
    case "not_set_up":
      headline = ar ? "النسخ الاحتياطي خارج الخادم غير مُعدّ بعد" : "Off-site backups aren't set up yet";
      detail = ar
        ? "إذا تعطل قرص الخادم تضيع كل الجرعات والقراءات. اتبع دليل الإعداد (القسم 9 في docs/DEPLOYMENT.md) على الخادم."
        : "If the server's disk dies, every dose and reading is lost. Follow the setup guide (docs/DEPLOYMENT.md §9) on the server.";
      break;
    case "invalid_grant":
      headline = ar ? "انتهى تسجيل دخول Google للنسخ الاحتياطي" : "Google sign-in for backups expired";
      detail = (
        <>
          {ar ? "شغّل هذا الأمر على الخادم:" : "Run this on the server:"}
          <pre dir="ltr" className="mt-1 whitespace-pre-wrap break-words rounded-lg bg-white/70 p-2 text-xs dark:bg-slate-900/60">rclone config reconnect gdrive:</pre>
        </>
      );
      break;
    case "restore_failed":
      headline = ar ? "فشل اختبار الاسترجاع الأسبوعي" : "The weekly restore test failed";
      detail = st?.restore_test_error ? <bdi dir="ltr">{st.restore_test_error}</bdi> : null;
      break;
    case "stale":
      headline = ar ? "لا يوجد نسخ احتياطي منذ أكثر من يوم" : "No backup for more than a day";
      detail = st?.error ? <bdi dir="ltr">{st.error}</bdi> : null;
      break;
    case "failed":
      headline = ar ? "فشلت آخر محاولة نسخ احتياطي" : "The last backup attempt failed";
      detail = st?.error ? <bdi dir="ltr">{st.error}</bdi> : null;
      break;
    default:
      headline = ar ? "النسخ الاحتياطي يعمل" : "Backups are working";
  }

  const tone = red
    ? "border-rose-300 bg-rose-50 text-rose-900 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200"
    : amber
      ? "border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200"
      : "border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-200";
  const Icon = red ? XCircle : amber ? AlertTriangle : CheckCircle2;

  return (
    <section data-testid="backups-card" data-reason={s.reason} aria-label={ar ? "النسخ الاحتياطي" : "Backups"} className="space-y-2">
      <h3 className="text-base font-black text-slate-900 dark:text-white">{ar ? "النسخ الاحتياطي" : "Backups"}</h3>

      <div className={`rounded-2xl border p-3 text-sm ${tone}`}>
        <p className="flex items-start gap-2 font-bold">
          <Icon className="mt-0.5 h-5 w-5 shrink-0" />
          <span>{headline}</span>
        </p>
        {detail && <div className="mt-1 break-words">{detail}</div>}
      </div>

      {st?.last_success_at && (
        <p data-testid="backup-last" className="text-sm text-slate-700 dark:text-slate-200">
          {ar ? "آخر نسخة خارج الخادم: " : "Last off-site backup: "}
          <Ltr>{fmt(st.last_success_at)}</Ltr>
          {names && (
            <>
              {ar ? " إلى " : " to "}
              {names}
            </>
          )}
        </p>
      )}
      {s.configured && (
        <p data-testid="backup-restore" className="text-sm text-slate-700 dark:text-slate-200">
          {st?.last_restore_test_at ? (
            st.last_restore_test_ok ? (
              <>
                {ar ? "اختبار الاسترجاع نجح: " : "Restore test passed: "}
                <Ltr>{fmt(st.last_restore_test_at)}</Ltr>
              </>
            ) : (
              <>
                {ar ? "اختبار الاسترجاع فشل: " : "Restore test failed: "}
                <Ltr>{fmt(st.last_restore_test_at)}</Ltr>
              </>
            )
          ) : ar ? (
            "لم يُجرَ اختبار استرجاع بعد"
          ) : (
            "No restore test yet"
          )}
        </p>
      )}

      {(st?.destinations ?? []).some((d) => d.snapshots) && (
        <p data-testid="backup-copies" className="text-sm text-slate-700 dark:text-slate-200">
          {ar ? "نسخ يمكن الرجوع إليها: " : "Copies to go back to: "}
          {(st?.destinations ?? []).map((d, i) => (
            <span key={d.name}>
              {i > 0 && " · "}
              <bdi>{d.name}</bdi> <Ltr>{d.snapshots ?? 0}</Ltr>
            </span>
          ))}
        </p>
      )}

      {s.postponed && (
        <p data-testid="backup-postponed" className="rounded-xl bg-amber-50 p-2.5 text-sm font-semibold text-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
          {s.postponed.reason === "crisis"
            ? ar ? "تأجّل النسخ الاحتياطي لوجود تنبيه طوارئ مفتوح. " : "Backup postponed: an emergency alert is open. "
            : ar ? "تأجّل النسخ الاحتياطي لأن التطبيق قيد الاستخدام. " : "Backup postponed: the app is in use. "}
          {ar ? "يحاول كل 5 دقائق، ويعمل حتماً عند " : "It tries every 5 minutes and runs anyway at "}
          <Ltr>{fmtTime(new Date(Date.parse(s.postponed.slot) + 2 * 3600_000).toISOString())}</Ltr>.
        </p>
      )}

      {s.schedule && (() => {
        const sch = draft ?? s.schedule;
        const edit = (next: BackupSchedule) => {
          setDraft(next);
          setTimesMsg("");
        };
        return (
          <div data-testid="backup-times" className="rounded-2xl border border-slate-200 p-3 dark:border-slate-800">
            <p className="text-sm font-bold text-slate-900 dark:text-white">{ar ? "أوقات النسخ الاحتياطي (بتوقيت المنزل)" : "Backup times (household time)"}</p>
            {s.next_at && !draft && (
              <p data-testid="backup-next" className="mt-0.5 text-xs text-slate-600 dark:text-slate-300">
                {ar ? "النسخة القادمة: " : "Next backup: "}
                <Ltr>{fmt(s.next_at)}</Ltr>
              </p>
            )}
            <ul className="mt-2 flex flex-wrap gap-2">
              {sch.times.map((t) => (
                <li key={t} className="flex min-h-12 items-center gap-1 rounded-xl bg-slate-100 ps-3 dark:bg-slate-800">
                  <Ltr className="text-base font-bold text-slate-900 dark:text-white">{t}</Ltr>
                  <button
                    type="button"
                    disabled={sch.times.length <= 1}
                    onClick={() => edit({ ...sch, times: sch.times.filter((x) => x !== t) })}
                    className="min-h-12 rounded-xl px-3 text-sm font-semibold text-rose-700 disabled:opacity-40 dark:text-rose-300"
                  >
                    {ar ? "إزالة" : "Remove"}
                  </button>
                </li>
              ))}
            </ul>
            {sch.times.length < 6 && (
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <input
                  type="time"
                  value={newTime}
                  onChange={(e) => setNewTime(e.target.value)}
                  aria-label={ar ? "وقت جديد" : "New time"}
                  data-testid="backup-new-time"
                  className="min-h-12 rounded-xl border border-slate-200 bg-white px-3 text-base text-slate-900 dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                />
                <button
                  type="button"
                  disabled={!newTime || sch.times.includes(newTime)}
                  onClick={() => {
                    edit({ ...sch, times: [...sch.times, newTime].sort() });
                    setNewTime("");
                  }}
                  className="min-h-12 rounded-xl border border-slate-300 px-4 text-sm font-bold text-slate-700 disabled:opacity-40 dark:border-slate-700 dark:text-slate-200"
                >
                  {ar ? "إضافة الوقت" : "Add time"}
                </button>
              </div>
            )}
            <button
              type="button"
              role="switch"
              aria-checked={sch.avoid_busy}
              data-testid="backup-avoid-busy"
              onClick={() => edit({ ...sch, avoid_busy: !sch.avoid_busy })}
              className="mt-2 flex min-h-12 w-full items-center justify-between gap-3 rounded-xl px-1 text-start"
            >
              <span>
                <span className="block text-sm font-bold text-slate-900 dark:text-white">{ar ? "انتظر إذا كان التطبيق قيد الاستخدام" : "Wait while the app is in use"}</span>
                <span className="block text-xs text-slate-600 dark:text-slate-300">
                  {ar
                    ? "إذا سُجّل شيء في آخر 10 دقائق أو كان هناك تنبيه طوارئ، يتأجل حتى ساعتين ثم يعمل حتماً."
                    : "If something was saved in the last 10 minutes or an emergency alert is open, it waits up to 2 hours, then runs anyway."}
                </span>
              </span>
              <span className={`shrink-0 rounded-full px-3 py-1 text-xs font-bold ${sch.avoid_busy ? "bg-emerald-600 text-white" : "bg-slate-200 text-slate-700 dark:bg-slate-700 dark:text-slate-200"}`}>
                {sch.avoid_busy ? (ar ? "مفعّل" : "On") : ar ? "متوقف" : "Off"}
              </span>
            </button>
            <fieldset className="mt-3 border-t border-slate-200 pt-3 dark:border-slate-800">
              <legend className="text-sm font-bold text-slate-900 dark:text-white">{ar ? "ماذا نحتفظ به" : "What to keep"}</legend>
              {(["history", "latest"] as const).map((r) => {
                const selected = (sch.retention ?? "history") === r;
                return (
                  <button
                    key={r}
                    type="button"
                    role="radio"
                    aria-checked={selected}
                    data-testid={`backup-keep-${r}`}
                    onClick={() => {
                      if (r === "latest" && !selected) {
                        setConfirmLatest(true);
                        return;
                      }
                      setConfirmLatest(false);
                      edit({ ...sch, retention: r });
                    }}
                    className={`mt-2 block min-h-12 w-full rounded-xl border-2 p-2.5 text-start ${selected ? "border-emerald-600 bg-emerald-50 dark:bg-emerald-950/40" : "border-slate-200 dark:border-slate-700"}`}
                  >
                    <span className="block text-sm font-bold text-slate-900 dark:text-white">
                      {r === "history" ? (ar ? "الاحتفاظ بالسجل (مُستحسن)" : "Keep history (recommended)") : ar ? "أحدث نسخة فقط" : "Latest copy only"}
                    </span>
                    <span className="block text-xs text-slate-600 dark:text-slate-300">
                      {r === "history"
                        ? ar
                          ? "آخر 8 نسخ، ونسخة لكل يوم لمدة 30 يوماً، ولكل أسبوع 12 أسبوعاً، ولكل شهر سنة. يمكنك الرجوع ليوم سابق إذا حدث خطأ. تُحفظ التغييرات فقط، فالمساحة الإضافية قليلة."
                          : "The last 8, one a day for 30 days, one a week for 12 weeks, one a month for a year. You can go back to an earlier day if something went wrong. Only changes are stored, so it uses little extra space."
                        : ar
                          ? "كل نسخة جديدة تحذف السابقة. أقل مساحة، لكن إذا حُفظ خطأ أو تلف فلا يمكن الرجوع."
                          : "Each new backup deletes the previous one. Least space, but if a mistake or damage gets backed up, there is nothing to go back to."}
                    </span>
                  </button>
                );
              })}
              {confirmLatest && (
                <div data-testid="backup-latest-confirm" className="mt-2 rounded-xl border border-amber-300 bg-amber-50 p-2.5 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
                  <p className="font-bold">
                    {ar
                      ? "عند النسخة القادمة ستُحذف كل النسخ الأقدم ولا يمكن استرجاعها."
                      : "At the next backup, all older copies are deleted and can't be brought back."}
                  </p>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <button
                      type="button"
                      data-testid="backup-latest-yes"
                      onClick={() => {
                        setConfirmLatest(false);
                        edit({ ...sch, retention: "latest" });
                      }}
                      className="min-h-12 rounded-xl border border-amber-500 px-3 text-sm font-bold"
                    >
                      {ar ? "نعم، أحدث نسخة فقط" : "Yes, keep only the newest copy"}
                    </button>
                    <button type="button" onClick={() => setConfirmLatest(false)} className="min-h-12 rounded-xl bg-slate-800 px-3 text-sm font-bold text-white dark:bg-slate-700">
                      {ar ? "لا، احتفظ بالسجل" : "No, keep history"}
                    </button>
                  </div>
                </div>
              )}
            </fieldset>

            {draft && (
              <div className="mt-2 flex flex-wrap gap-2">
                <button type="button" data-testid="backup-times-save" disabled={savingTimes} onClick={saveTimes} className="min-h-12 rounded-xl bg-emerald-700 px-4 text-base font-bold text-white disabled:opacity-50">
                  {ar ? "حفظ" : "Save"}
                </button>
                <button type="button" onClick={() => { setDraft(null); setConfirmLatest(false); }} className="min-h-12 rounded-xl border border-slate-300 px-4 text-sm font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200">
                  {ar ? "إلغاء" : "Cancel"}
                </button>
              </div>
            )}
            {timesMsg && <p className="mt-1 text-sm font-semibold text-slate-700 dark:text-slate-200">{timesMsg}</p>}
          </div>
        );
      })()}

      {pending ? (
        <p data-testid="backup-running" className="flex min-h-12 items-center gap-2 text-sm font-bold text-sky-800 dark:text-sky-200">
          <Loader2 className="h-5 w-5 animate-spin" />
          {ar ? "جارٍ النسخ الاحتياطي..." : "Backing up..."}
        </p>
      ) : (
        <button
          type="button"
          data-testid="backup-now"
          disabled={busy || !s.configured}
          onClick={runNow}
          className="flex min-h-12 items-center gap-2 rounded-xl bg-emerald-700 px-4 text-base font-bold text-white disabled:opacity-50"
        >
          <CloudUpload className="h-5 w-5" />
          {ar ? "انسخ احتياطياً الآن" : "Back up now"}
        </button>
      )}
      {msg && <p className="text-sm font-semibold text-rose-700 dark:text-rose-300">{msg}</p>}
    </section>
  );
}
