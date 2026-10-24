"use client";

import { useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import { fetchApi } from "@/lib/api";
import { RefreshCw } from "lucide-react";

const CHECK_EVERY_MS = 10 * 60_000;

/** Same version: the server's short sha against the one this page was loaded from. */
export function sameVersion(page: string, server: string): boolean {
  const a = page.slice(0, 7);
  const b = server.slice(0, 7);
  return !a || !b || a === b;
}

/**
 * A phone that keeps the app open never reloads it by itself, so after an
 * update it can keep showing the old screens for days. This page knows the
 * version it was loaded from; when the server answers with another one, it
 * offers a one-tap reload (never automatic: a half-typed note must not vanish).
 */
export function NewVersionBanner({ pageVersion }: { pageVersion: string }) {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const [stale, setStale] = useState(false);

  useEffect(() => {
    if (!pageVersion || pageVersion === "dev") return;
    const check = async () => {
      try {
        const h = await fetchApi<{ version?: string }>("/health");
        const v = h?.version || "";
        if (v && v !== "dev" && !sameVersion(pageVersion, v)) setStale(true);
      } catch {
        // offline or restarting: try again later
      }
    };
    const onVisible = () => {
      if (document.visibilityState === "visible") check();
    };
    const first = setTimeout(check, 3000);
    const every = setInterval(check, CHECK_EVERY_MS);
    window.addEventListener(RESYNC_EVENT, check);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      clearTimeout(first);
      clearInterval(every);
      window.removeEventListener(RESYNC_EVENT, check);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [pageVersion]);

  if (!stale) return null;
  return (
    <div role="status" data-testid="new-version-banner" className="flex flex-wrap items-center justify-center gap-2 bg-emerald-700 px-4 py-2 text-center text-sm font-bold text-white">
      <span>{ar ? "نسخة جديدة من التطبيق جاهزة." : "A new version of the app is ready."}</span>
      <button
        type="button"
        onClick={() => window.location.reload()}
        className="flex min-h-11 items-center gap-1.5 rounded-xl bg-white px-4 text-sm font-black text-emerald-800"
      >
        <RefreshCw className="h-4 w-4" />
        {ar ? "حدّث الآن" : "Update now"}
      </button>
    </div>
  );
}
