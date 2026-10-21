"use client";

import { useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import { SSEEvent } from "@/lib/types";
import { fetchApi } from "@/lib/api";

// Everyone sees this while the Super Admin's update installs (issue #43): the
// app restarts for about a minute, and taps meanwhile wait in the outbox (#16).
const GIVE_UP_MS = 5 * 60_000;

export function UpdateBanner() {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const [target, setTarget] = useState<string | null>(null);

  useEffect(() => {
    let giveUp: ReturnType<typeof setTimeout> | null = null;
    let waiting: string | null = null;

    const onEvent = (e: Event) => {
      const ev = (e as CustomEvent<SSEEvent>).detail;
      if (ev?.type !== "UPDATE_STARTING") return;
      waiting = String((ev.payload as { target_sha?: string } | undefined)?.target_sha || "");
      setTarget(waiting);
      if (giveUp) clearTimeout(giveUp);
      giveUp = setTimeout(() => {
        waiting = null;
        setTarget(null);
      }, GIVE_UP_MS);
    };

    // The stream reconnects after the restart: hide once the new version answers.
    const onResync = async () => {
      if (waiting === null) return;
      try {
        const h = await fetchApi<{ version?: string }>("/health");
        if (!waiting || (h.version && waiting.startsWith(h.version.slice(0, 7)))) {
          waiting = null;
          setTarget(null);
        }
      } catch {
        // still restarting
      }
    };

    window.addEventListener("birrcare:sse_event", onEvent);
    window.addEventListener(RESYNC_EVENT, onResync);
    return () => {
      window.removeEventListener("birrcare:sse_event", onEvent);
      window.removeEventListener(RESYNC_EVENT, onResync);
      if (giveUp) clearTimeout(giveUp);
    };
  }, []);

  if (target === null) return null;
  return (
    <div role="status" data-testid="update-starting-banner" className="bg-sky-700 px-4 py-2 text-center text-sm font-bold text-white">
      {ar
        ? "يتم تحديث رعاية بر وسيعود خلال دقيقة تقريباً. أي شيء تضغطه يُحفظ في هاتفك ويُرسل عند عودته."
        : "Birrcare is updating and will be back in about a minute. Anything you tap is kept on your phone and sent when it's back."}
    </div>
  );
}
