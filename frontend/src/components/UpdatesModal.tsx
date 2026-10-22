"use client";

import { useCallback, useEffect, useState } from "react";
import { AlertTriangle, CheckCircle2, Download, Loader2, XCircle } from "lucide-react";
import { useLanguage } from "@/context/LanguageContext";
import { fetchApi } from "@/lib/api";
import { CHECKER_COMMAND, UPDATE_STATUS_EVENT, UpdateStatus, fetchUpdateStatus, shortSha } from "@/lib/updates";
import { Ltr } from "./Ltr";
import { BackupsCard } from "./BackupsCard";

// Super Admin only (issue #43): see what is running, read what changed, and
// install when the house is calm, or skip. Nothing installs by itself unless
// "Automatic updates" is switched on.

type Phase = "idle" | "crisis" | "installing" | "done" | "failed";

const POLL_MS = 5000;

export function UpdatesModal({ isOpen, onClose }: { isOpen: boolean; onClose: () => void }) {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [phase, setPhase] = useState<Phase>("idle");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [installTarget, setInstallTarget] = useState("");

  const changed = () => window.dispatchEvent(new Event(UPDATE_STATUS_EVENT));

  const load = useCallback(async () => {
    try {
      const s = await fetchUpdateStatus();
      setStatus(s);
      return s;
    } catch {
      return null; // restarting, or offline
    }
  }, []);

  useEffect(() => {
    if (!isOpen) return;
    load().then((s) => {
      // Reopened while an install is still running.
      if (s?.pending && s.available?.target_sha) {
        setInstallTarget(s.available.target_sha);
        setPhase("installing");
      }
    });
  }, [isOpen, load]);

  // While installing: poll until the new version answers or the host reports a result.
  useEffect(() => {
    if (!isOpen || phase !== "installing") return;
    const tick = async () => {
      const s = await load();
      const want = shortSha(installTarget);
      if (!s || !want) return;
      if (s.version.startsWith(want)) {
        setPhase("done");
        changed();
      } else if (s.result && s.result.sha.startsWith(want) && s.result.status !== "success" && !s.pending) {
        setPhase("failed");
        changed();
      }
    };
    const id = setInterval(tick, POLL_MS);
    return () => clearInterval(id);
  }, [isOpen, phase, installTarget, load]);

  if (!isOpen) return null;

  const install = async (confirmDuringAlert: boolean) => {
    const target = status?.available?.target_sha;
    if (!target) return;
    setBusy(true);
    setMsg("");
    try {
      await fetchApi("/admin/update/install", {
        method: "POST",
        body: JSON.stringify({ target_sha: target, confirm_during_alert: confirmDuringAlert }),
      });
      setInstallTarget(target);
      setPhase("installing");
      changed();
    } catch (err: any) {
      const code = err?.data?.error;
      if (code === "ACTIVE_CRISIS") {
        setPhase("crisis");
      } else if (code === "IN_PROGRESS") {
        setInstallTarget(target);
        setPhase("installing");
      } else if (code === "NEEDS_CONFIG") {
        setMsg(ar ? "يحتاج هذا التحديث إعداداً جديداً على الخادم أولاً." : "This update needs a new setting on the server first.");
        load();
      } else if (code === "STALE_TARGET") {
        setMsg(ar ? "وصل إصدار أحدث. تم تحديث القائمة، راجعها ثم اضغط تثبيت." : "A newer version arrived. The list is refreshed; check it and press Install again.");
        setPhase("idle");
        load();
      } else {
        setMsg(ar ? "تعذر طلب التحديث، حاول مجدداً" : "Could not request the update, please try again");
      }
    } finally {
      setBusy(false);
    }
  };

  const skip = async () => {
    const target = status?.available?.target_sha;
    if (!target) return;
    setBusy(true);
    setMsg("");
    try {
      setStatus(await fetchApi<UpdateStatus>("/admin/update/skip", { method: "POST", body: JSON.stringify({ target_sha: target }) }));
      setPhase("idle");
      changed();
    } catch {
      setMsg(ar ? "تعذر تخطي هذا الإصدار، حاول مجدداً" : "Could not skip this version, please try again");
      load();
    } finally {
      setBusy(false);
    }
  };

  const setAuto = async (auto: boolean) => {
    setBusy(true);
    setMsg("");
    try {
      setStatus(await fetchApi<UpdateStatus>("/admin/update/policy", { method: "PUT", body: JSON.stringify({ mode: auto ? "auto" : "manual" }) }));
      changed();
    } catch {
      setMsg(ar ? "تعذر حفظ الإعداد، حاول مجدداً" : "Could not save the setting, please try again");
    } finally {
      setBusy(false);
    }
  };

  const fmt = (iso?: string) => {
    if (!iso) return "";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    return new Intl.DateTimeFormat(ar ? "ar-u-nu-latn" : "en-GB", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }).format(d);
  };

  const s = status;
  const commits = s?.available?.commits ?? [];
  const installedAt = s?.result?.status === "success" && s.result.sha.startsWith(s.version) ? fmt(s.result.finished_at) : "";
  const auto = s?.policy.mode === "auto";
  // Issue #52: a new required setting must be answered on the server first.
  const needs = s?.available?.needs_config ?? [];
  const offer = !!s?.update_available && phase !== "installing" && phase !== "done";

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-900/60 p-0 backdrop-blur-xs sm:items-center sm:p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-label={ar ? "الخادم: النسخ الاحتياطي والتحديثات" : "Server: backups & updates"}
        data-testid="updates-card"
        className="relative max-h-dvh w-full max-w-lg overflow-y-auto rounded-t-3xl border border-slate-200 bg-white p-5 pb-[calc(1.25rem+env(safe-area-inset-bottom))] shadow-2xl sm:rounded-3xl dark:border-slate-800 dark:bg-slate-900"
      >
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-lg font-black text-slate-900 dark:text-white">{ar ? "الخادم" : "Server"}</h2>
          <button
            type="button"
            onClick={onClose}
            className="min-h-12 rounded-xl border border-slate-200 px-4 text-sm font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200"
          >
            {ar ? "إغلاق" : "Close"}
          </button>
        </div>

        <div className="mb-4 border-b border-slate-200 pb-4 dark:border-slate-800">
          <BackupsCard ar={ar} />
        </div>

        <h3 className="mb-2 text-base font-black text-slate-900 dark:text-white">{ar ? "التحديثات" : "Updates"}</h3>
        {!s ? (
          <p className="py-6 text-center text-sm text-slate-500 dark:text-slate-400">{ar ? "جارٍ التحميل..." : "Loading..."}</p>
        ) : (
          <div className="space-y-3">
            <p data-testid="update-running" className="text-sm font-semibold text-slate-700 dark:text-slate-200">
              {ar ? "يعمل الآن الإصدار " : "Running "}
              <Ltr>{s.version}</Ltr>
              {installedAt && (
                <>
                  {" · "}
                  {ar ? "ثُبّت " : "installed "}
                  <Ltr>{installedAt}</Ltr>
                </>
              )}
            </p>

            {s.checker_stale && (
              <div data-testid="checker-stale" className="rounded-2xl border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
                <p className="font-bold">{ar ? "برنامج فحص التحديثات لا يعمل على الخادم" : "The update checker isn't running on the server"}</p>
                <p className="mt-1">{ar ? "شغّل هذا الأمر على الخادم:" : "Run this on the server:"}</p>
                <pre dir="ltr" className="mt-1 overflow-x-auto whitespace-pre-wrap break-all rounded-lg bg-white/70 p-2 text-xs dark:bg-slate-900/60">{CHECKER_COMMAND}</pre>
              </div>
            )}

            {phase === "installing" && (
              <div data-testid="update-installing" className="flex items-center gap-2 rounded-2xl border border-sky-200 bg-sky-50 p-3 text-sm font-bold text-sky-900 dark:border-sky-900 dark:bg-sky-950/40 dark:text-sky-200">
                <Loader2 className="h-5 w-5 shrink-0 animate-spin" />
                <span>
                  {ar ? "جارٍ التثبيت… سيعود رعاية بر خلال دقيقة تقريباً." : "Installing… Birrcare will be back in about a minute."}
                </span>
              </div>
            )}
            {phase === "done" && (
              <div data-testid="update-done" className="flex items-center gap-2 rounded-2xl border border-emerald-200 bg-emerald-50 p-3 text-sm font-bold text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-200">
                <CheckCircle2 className="h-5 w-5 shrink-0" />
                <span>
                  {ar ? "تم التحديث إلى " : "Updated to "}
                  <Ltr>{shortSha(installTarget)}</Ltr>
                </span>
              </div>
            )}
            {phase === "failed" && (
              <div data-testid="update-failed" className="rounded-2xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-900 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200">
                <p className="flex items-center gap-2 font-bold">
                  <XCircle className="h-5 w-5 shrink-0" />
                  <span>
                    {ar ? "فشل التحديث. ما زال يعمل الإصدار " : "Update failed. Still running "}
                    <Ltr>{s.version}</Ltr>
                  </span>
                </p>
                {s.result?.status === "needs_config" ? (
                  <p data-testid="update-failed-needs-config" className="mt-2">
                    {ar
                      ? "يحتاج هذا الإصدار إعداداً جديداً. شغّل ./scripts/deploy.sh على الخادم مرة واحدة للإجابة عليه، ثم ثبّت مجدداً."
                      : "This version needs a new setting. Run ./scripts/deploy.sh on the server once to answer it, then install again."}
                  </p>
                ) : s.result?.log_tail && (
                  <pre dir="ltr" className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-white/70 p-2 text-xs dark:bg-slate-900/60">{s.result.log_tail}</pre>
                )}
              </div>
            )}

            {offer ? (
              <div className="rounded-2xl border border-slate-200 p-3 dark:border-slate-800">
                <p className="text-base font-black text-slate-900 dark:text-white">
                  {ar ? `يوجد تحديث · ${commits.length} ${commits.length === 1 ? "تغيير" : "تغييرات"}` : `Update available · ${commits.length} ${commits.length === 1 ? "change" : "changes"}`}
                </p>
                <ul data-testid="update-commits" className="mt-2 space-y-1.5">
                  {commits.map((c) => (
                    <li key={c.sha} className="flex gap-2 text-sm text-slate-700 dark:text-slate-200">
                      <Ltr className="shrink-0 font-mono text-xs leading-5 text-slate-500 dark:text-slate-400">{c.sha}</Ltr>
                      <bdi dir="ltr" className="min-w-0 break-words">{c.subject}</bdi>
                    </li>
                  ))}
                </ul>

                {needs.length > 0 && (
                  <div data-testid="update-needs-config" className="mt-3 rounded-xl border border-amber-300 bg-amber-50 p-2.5 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
                    <p className="flex gap-2 font-bold">
                      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                      <span>{ar ? "هذا التحديث يحتاج إعداداً جديداً:" : "This update needs a new setting:"}</span>
                    </p>
                    <ul className="mt-1 list-disc ps-6">
                      {needs.map((n) => (
                        <li key={n.key}>
                          <bdi>{ar ? n.question_ar : n.question_en}</bdi>
                        </li>
                      ))}
                    </ul>
                    <p className="mt-1">{ar ? "شغّل هذا الأمر على الخادم مرة واحدة للإجابة عليه، ثم ثبّت:" : "Run this on the server once to answer it, then install:"}</p>
                    <pre dir="ltr" className="mt-1 whitespace-pre-wrap break-words rounded-lg bg-white/70 p-2 text-xs dark:bg-slate-900/60">cd ~/birrcare &amp;&amp; ./scripts/deploy.sh</pre>
                  </div>
                )}

                {s.available?.changes_database && (
                  <p data-testid="update-db-notice" className="mt-3 flex gap-2 rounded-xl bg-amber-50 p-2.5 text-sm font-semibold text-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                    <span>
                      {ar
                        ? "هذا التحديث يغيّر قاعدة البيانات. تُؤخذ نسخة احتياطية أولاً، والرجوع للإصدار السابق يحتاج تلك النسخة (راجع قسم الاسترجاع)."
                        : "This update changes the database. A backup is taken first. Going back needs that backup (see Rollback)."}
                    </span>
                  </p>
                )}

                {phase === "crisis" && (
                  <div data-testid="update-crisis" className="mt-3 rounded-xl border border-rose-300 bg-rose-50 p-3 text-sm text-rose-900 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200">
                    <p className="font-bold">
                      {ar
                        ? "يوجد تنبيه طوارئ خلال آخر 30 دقيقة لم يؤكده أحد. التطبيق سيتوقف دقيقة أثناء التحديث."
                        : "An emergency alert from the last 30 minutes hasn't been acknowledged. The app will be down for about a minute while it updates."}
                    </p>
                    <div className="mt-2 flex flex-wrap gap-2">
                      <button type="button" disabled={busy} onClick={() => setPhase("idle")} className="min-h-12 rounded-xl bg-slate-800 px-4 text-sm font-bold text-white disabled:opacity-50 dark:bg-slate-700">
                        {ar ? "انتظر، لا تثبّت الآن" : "Wait, don't install now"}
                      </button>
                      <button type="button" disabled={busy} onClick={() => install(true)} className="min-h-12 rounded-xl border border-rose-400 px-4 text-sm font-bold text-rose-800 disabled:opacity-50 dark:text-rose-200">
                        {ar ? "ثبّت على أي حال" : "Install anyway"}
                      </button>
                    </div>
                  </div>
                )}

                {phase !== "crisis" && (
                  <div className="mt-3 flex flex-wrap gap-2">
                    <button
                      type="button"
                      data-testid="update-install"
                      disabled={busy || needs.length > 0}
                      onClick={() => install(false)}
                      className="flex min-h-12 items-center gap-2 rounded-xl bg-emerald-700 px-4 text-base font-bold text-white disabled:opacity-50"
                    >
                      <Download className="h-5 w-5" />
                      {ar ? "ثبّت الآن" : "Install now"}
                    </button>
                    <button
                      type="button"
                      data-testid="update-skip"
                      disabled={busy}
                      onClick={skip}
                      className="min-h-12 rounded-xl border border-slate-300 px-4 text-base font-bold text-slate-700 disabled:opacity-50 dark:border-slate-700 dark:text-slate-200"
                    >
                      {ar ? "تخطَّ هذا الإصدار" : "Skip this version"}
                    </button>
                  </div>
                )}
              </div>
            ) : (
              phase !== "installing" &&
              phase !== "done" && (
                <p data-testid="update-none" className="flex items-center gap-2 text-sm text-slate-600 dark:text-slate-300">
                  <CheckCircle2 className="h-5 w-5 text-emerald-600" />
                  {ar ? "رعاية بر محدّث، لا يوجد تحديث جديد." : "Birrcare is up to date."}
                </p>
              )
            )}

            <button
              type="button"
              role="switch"
              aria-checked={auto}
              data-testid="update-auto"
              disabled={busy}
              onClick={() => setAuto(!auto)}
              className="flex min-h-12 w-full items-center justify-between gap-3 rounded-2xl border border-slate-200 px-4 py-2 text-start disabled:opacity-50 dark:border-slate-800"
            >
              <span>
                <span className="block text-sm font-bold text-slate-900 dark:text-white">{ar ? "التحديث التلقائي" : "Automatic updates"}</span>
                <span className="block text-xs text-slate-500 dark:text-slate-400">
                  {auto
                    ? ar ? "مفعّل: يُثبَّت أي تحديث فور نجاح فحوصاته" : "On: updates install as soon as their checks pass"
                    : ar ? "متوقف: أنت تختار وقت التثبيت" : "Off: you choose when to install"}
                </span>
              </span>
              <span className={`shrink-0 rounded-full px-3 py-1 text-xs font-bold ${auto ? "bg-emerald-600 text-white" : "bg-slate-200 text-slate-700 dark:bg-slate-700 dark:text-slate-200"}`}>
                {auto ? (ar ? "مفعّل" : "On") : ar ? "متوقف" : "Off"}
              </span>
            </button>

            {msg && <p className="text-sm font-semibold text-rose-700 dark:text-rose-300">{msg}</p>}
          </div>
        )}
      </div>
    </div>
  );
}
