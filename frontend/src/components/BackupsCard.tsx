"use client";

import { useCallback, useEffect, useState, type ReactNode } from "react";
import { AlertTriangle, CheckCircle2, CloudUpload, Loader2, XCircle } from "lucide-react";
import { fetchApi } from "@/lib/api";
import { BackupStatus, fetchBackupStatus } from "@/lib/backups";
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
