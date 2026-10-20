"use client";

import React, { useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { BellRing, X } from "lucide-react";

// Issue #12: offer lock-screen alerts (Web Push) on this phone. iOS only delivers
// push to an app added to the Home Screen (iOS 16.4+), so there we explain that first.

type State = "hidden" | "off" | "ios-install" | "denied" | "on" | "just-on";
const DISMISS_KEY = "birrcare_push_dismissed";

function isIOS() {
  return /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
}
function isStandalone() {
  return window.matchMedia?.("(display-mode: standalone)").matches || (navigator as unknown as { standalone?: boolean }).standalone === true;
}
function keyBytes(base64url: string): Uint8Array {
  const b64 = (base64url + "=".repeat((4 - (base64url.length % 4)) % 4)).replace(/-/g, "+").replace(/_/g, "/");
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}

export function PushOptIn() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const [state, setState] = useState<State>("hidden");
  const [busy, setBusy] = useState(false);
  const ar = locale === "ar";
  const me = currentCaregiver?.id;
  const viewer = currentCaregiver?.role;

  useEffect(() => {
    if (!me) return;
    let alive = true;
    (async () => {
      const supported = "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
      let dismissed = false;
      try {
        dismissed = localStorage.getItem(DISMISS_KEY) === "1";
      } catch {}
      if (!supported) {
        if (alive) setState(isIOS() && !isStandalone() && !dismissed ? "ios-install" : "hidden");
        return;
      }
      if (Notification.permission === "denied") {
        if (alive) setState(dismissed ? "hidden" : "denied");
        return;
      }
      const reg = await navigator.serviceWorker.register("/sw.js").catch(() => null);
      const sub = reg ? await reg.pushManager.getSubscription().catch(() => null) : null;
      if (sub && Notification.permission === "granted") {
        // Re-register so a shared phone's alerts follow whoever is signed in now.
        await fetchApi("/push/subscribe", { method: "POST", body: JSON.stringify(sub.toJSON()) }).catch(() => {});
        if (alive) setState("on");
        return;
      }
      if (alive) setState(dismissed ? "hidden" : "off");
    })();
    return () => {
      alive = false;
    };
  }, [me, viewer]);

  const enable = async () => {
    setBusy(true);
    try {
      const permission = await Notification.requestPermission();
      if (permission !== "granted") {
        setState("denied");
        return;
      }
      const { public_key } = await fetchApi<{ public_key: string }>("/push/key");
      const reg = await navigator.serviceWorker.ready;
      const sub =
        (await reg.pushManager.getSubscription()) ??
        (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(public_key) as BufferSource }));
      await fetchApi("/push/subscribe", { method: "POST", body: JSON.stringify(sub.toJSON()) });
      setState("just-on");
    } catch {
      alert(ar ? "تعذر تفعيل التنبيهات على هذا الهاتف" : "Could not turn on alerts on this phone");
    } finally {
      setBusy(false);
    }
  };

  const dismiss = () => {
    try {
      localStorage.setItem(DISMISS_KEY, "1");
    } catch {}
    setState("hidden");
  };

  if (state === "hidden" || state === "on") return null;

  if (state === "just-on") {
    return (
      <div data-testid="push-opt-in" className="mb-4 space-y-2 rounded-2xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-900 dark:bg-emerald-950/30">
        <p className="text-base font-semibold text-emerald-900 dark:text-emerald-100">
          {ar ? "التنبيهات مفعّلة. جرّبها: اضغط الزر ثم اقفل الهاتف، سيصلك تنبيه تجريبي خلال 10 ثوانٍ." : "Alerts are on. Try it: tap the button, then lock the phone. A test alert arrives in 10 seconds."}
        </p>
        <div className="flex gap-2">
          <button type="button" onClick={() => fetchApi("/push/test", { method: "POST" }).catch(() => {})} className="min-h-12 rounded-xl bg-emerald-700 px-4 text-base font-bold text-white">
            {ar ? "إرسال تنبيه تجريبي" : "Send a test alert"}
          </button>
          <button type="button" onClick={() => setState("on")} className="min-h-12 rounded-xl px-4 text-base font-semibold text-emerald-800 dark:text-emerald-200">
            {ar ? "تم" : "Done"}
          </button>
        </div>
      </div>
    );
  }

  const text =
    state === "ios-install"
      ? ar
        ? "لتصلك تنبيهات الطوارئ والهاتف مقفل: في Safari اضغط زر المشاركة ثم «إضافة إلى الشاشة الرئيسية»، وافتح رعاية بر من الشاشة الرئيسية."
        : "To get emergency alerts while the phone is locked: in Safari tap Share, then “Add to Home Screen”, and open Birrcare from the Home Screen."
      : state === "denied"
      ? ar
        ? "التنبيهات مرفوضة لهذا الموقع. فعّلها من إعدادات الهاتف ← الإشعارات، ليصلك تنبيه الأزمات والهاتف مقفل."
        : "Notifications are blocked for this site. Allow them in the phone's settings → Notifications to get crisis alerts while it's locked."
      : ar
      ? "فعّل التنبيهات ليصلك تنبيه أزمة الضغط وتنبيهات الأدوية المهمة حتى والهاتف مقفل."
      : "Turn on alerts to get blood-pressure crises and important medication alerts even while the phone is locked.";

  return (
    <div data-testid="push-opt-in" className="mb-4 flex items-start gap-3 rounded-2xl border border-rose-200 bg-rose-50 p-4 dark:border-rose-900 dark:bg-rose-950/30">
      <BellRing className="mt-0.5 h-5 w-5 shrink-0 text-rose-700 dark:text-rose-300" />
      <div className="flex-1 space-y-2">
        <p className="text-base font-semibold text-rose-950 dark:text-rose-100">{text}</p>
        {state === "off" && (
          <button
            type="button"
            onClick={enable}
            disabled={busy}
            className="min-h-12 rounded-xl bg-rose-700 px-4 text-base font-bold text-white disabled:opacity-50"
          >
            {ar ? "تفعيل التنبيهات على هذا الهاتف" : "Turn on alerts on this phone"}
          </button>
        )}
      </div>
      <button
        type="button"
        onClick={dismiss}
        aria-label={ar ? "ليس الآن" : "Not now"}
        className="flex min-h-12 min-w-12 items-center justify-center rounded-full text-rose-700 hover:bg-rose-100 dark:text-rose-300 dark:hover:bg-rose-900/40"
      >
        <X className="h-5 w-5" />
      </button>
    </div>
  );
}
