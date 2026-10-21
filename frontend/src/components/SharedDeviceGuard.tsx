"use client";

import React, { useEffect, useRef, useState } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { fetchApi, WROTE_EVENT } from "@/lib/api";
import { firstName } from "@/lib/actor";
import { Users } from "lucide-react";

const TOUCH_EVERY_MS = 60_000;
const CHIP_MS = 5_000;

/**
 * Issue #17, shared tablet only. Tells the server someone is still using it
 * (taps and typing, at most once a minute), checks the session once the idle
 * time has passed so the PIN keypad comes up for the next person, and after
 * each save asks for 5 seconds "Still Hussain?".
 */
export function SharedDeviceGuard() {
  const { currentCaregiver, openPinModal, refreshCaregiver } = useCaregiver();
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const shared = !!currentCaregiver?.shared_device;
  const idleMs = (currentCaregiver?.idle_minutes || 20) * 60_000;
  const [chip, setChip] = useState(false);
  const lastActivity = useRef(0);
  const lastTouch = useRef(0);

  useEffect(() => {
    if (!shared) return;
    lastActivity.current = lastTouch.current = Date.now();
    const onActivity = () => {
      const now = Date.now();
      lastActivity.current = now;
      if (now - lastTouch.current >= TOUCH_EVERY_MS) {
        lastTouch.current = now;
        fetchApi("/auth/touch", { method: "POST" }).catch(() => {});
      }
    };
    // Past the idle time (plus the touch throttle) the server has signed this
    // person out: ask it, and a 401 opens the keypad for whoever is next.
    const check = window.setInterval(() => {
      if (Date.now() - lastActivity.current > idleMs + TOUCH_EVERY_MS) refreshCaregiver();
    }, 30_000);
    window.addEventListener("pointerdown", onActivity, { passive: true });
    window.addEventListener("keydown", onActivity);
    return () => {
      window.clearInterval(check);
      window.removeEventListener("pointerdown", onActivity);
      window.removeEventListener("keydown", onActivity);
    };
  }, [shared, idleMs, refreshCaregiver]);

  useEffect(() => {
    if (!shared) return;
    let timer: number | undefined;
    const onWrote = () => {
      setChip(true);
      window.clearTimeout(timer);
      timer = window.setTimeout(() => setChip(false), CHIP_MS);
    };
    window.addEventListener(WROTE_EVENT, onWrote);
    return () => {
      window.removeEventListener(WROTE_EVENT, onWrote);
      window.clearTimeout(timer);
    };
  }, [shared]);

  if (!shared || !chip || !currentCaregiver) return null;
  const name = firstName(currentCaregiver.name);

  return (
    <div
      role="status"
      data-testid="still-you"
      className="fixed inset-x-0 top-[calc(4.5rem+env(safe-area-inset-top))] z-40 mx-auto flex w-11/12 max-w-md items-center gap-2 rounded-2xl bg-slate-900 p-2 ps-4 text-white shadow-xl dark:bg-slate-700"
    >
      <span
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-sm font-bold"
        style={{ backgroundColor: currentCaregiver.avatar_color }}
      >
        {name.slice(0, 1)}
      </span>
      <p className="flex-1 text-sm font-bold">{ar ? `ما زلت ${name}؟` : `Still ${name}?`}</p>
      <button type="button" onClick={() => setChip(false)} className="min-h-12 rounded-xl px-3 text-sm font-bold">
        {ar ? "نعم" : "Yes"}
      </button>
      <button
        type="button"
        onClick={() => {
          setChip(false);
          openPinModal();
        }}
        className="flex min-h-12 items-center gap-1.5 rounded-xl bg-white px-3 text-sm font-bold text-slate-900"
      >
        <Users className="h-4 w-4" />
        {ar ? "تبديل" : "Switch"}
      </button>
    </div>
  );
}
