"use client";

import { haptic } from "@/lib/theme";
import React, { useEffect, useRef, useState } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { ShieldCheck, Loader2, ArrowLeft } from "lucide-react";

interface Props {
  challenge: string;
  /** Back to the PIN keypad (also used when the challenge expired). */
  onBack: (message?: string) => void;
}

/** Second sign-in step: 6-digit authenticator code, or a one-time recovery code. */
export function TwoFactorCodeStep({ challenge, onBack }: Props) {
  const { verifyTwoFactor } = useCaregiver();
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState(false);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, [recovery]);

  const submit = async (value: string) => {
    if (submitting) return;
    setSubmitting(true);
    setError("");
    const res = await verifyTwoFactor(challenge, value);
    setSubmitting(false);
    if (res.success) return;
    if (res.status === 401) {
      onBack(ar ? "انتهت المهلة، أدخل الرمز السري مرة أخرى" : "Timed out, enter your PIN again");
      return;
    }
    if (res.status === 429) {
      const mins = res.retryAfter ? Math.max(1, Math.ceil(res.retryAfter / 60)) : null;
      setError(ar ? `محاولات كثيرة.${mins ? ` حاول بعد ${mins} دقيقة.` : ""}` : `Too many attempts.${mins ? ` Try again in ${mins} min.` : ""}`);
    } else {
      setError(ar ? "الرمز غير صحيح، استخدم أحدث رمز في التطبيق" : "Incorrect code, use the newest code in the app");
    }
    setCode("");
    haptic([60, 50, 60]);
  };

  const onChange = (value: string) => {
    const v = recovery ? value.slice(0, 16) : value.replace(/\D/g, "").slice(0, 6);
    setCode(v);
    setError("");
    if (!recovery && v.length === 6) submit(v);
  };

  return (
    <div className="space-y-4">
      <div className="text-center">
        <div className="inline-flex h-12 w-12 items-center justify-center rounded-2xl bg-emerald-50 text-emerald-600 dark:bg-emerald-950/60 dark:text-emerald-400 mb-2">
          <ShieldCheck className="h-6 w-6" />
        </div>
        <h3 className="text-lg font-bold text-slate-900 dark:text-white">
          {recovery ? (ar ? "رمز الاسترداد" : "Recovery code") : ar ? "رمز تطبيق المصادقة" : "Authenticator code"}
        </h3>
        <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
          {recovery
            ? ar ? "أدخل أحد رموز الاسترداد المحفوظة (يُستخدم مرة واحدة)" : "Enter one of your saved recovery codes (single use)"
            : ar ? "افتح تطبيق المصادقة وأدخل الرمز المكوّن من 6 أرقام" : "Open your authenticator app and enter the 6-digit code"}
        </p>
      </div>

      <input
        ref={inputRef}
        type="text"
        dir="ltr"
        inputMode={recovery ? "text" : "numeric"}
        autoComplete="one-time-code"
        autoCapitalize="off"
        spellCheck={false}
        value={code}
        disabled={submitting}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && code && submit(code)}
        placeholder={recovery ? "xxxxx-xxxxx" : "000000"}
        aria-label={recovery ? (ar ? "رمز الاسترداد" : "Recovery code") : ar ? "رمز المصادقة" : "Authenticator code"}
        className="min-h-14 w-full rounded-2xl border-2 border-slate-200 bg-white px-4 text-center font-mono text-3xl font-black tracking-[0.3em] text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
      />

      {error && (
        <p role="alert" className="text-center text-xs font-semibold text-rose-600 dark:text-rose-400 animate-shake">
          {error}
        </p>
      )}

      {(recovery || submitting) && (
        <button
          type="button"
          disabled={submitting || !code.trim()}
          onClick={() => submit(code)}
          className="flex min-h-12 w-full items-center justify-center gap-2 rounded-2xl bg-emerald-600 text-sm font-bold text-white disabled:opacity-50"
        >
          {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
          {ar ? "دخول" : "Sign in"}
        </button>
      )}

      <div className="flex items-center justify-between gap-2">
        <button
          type="button"
          onClick={() => onBack()}
          className="flex min-h-12 items-center gap-1 rounded-xl px-3 text-sm font-semibold text-slate-600 dark:text-slate-300"
        >
          <ArrowLeft className="h-4 w-4 rtl:rotate-180" />
          {ar ? "رجوع" : "Back"}
        </button>
        <button
          type="button"
          onClick={() => {
            setRecovery((r) => !r);
            setCode("");
            setError("");
          }}
          className="min-h-12 rounded-xl px-3 text-sm font-semibold text-emerald-700 dark:text-emerald-400"
        >
          {recovery ? (ar ? "استخدم تطبيق المصادقة" : "Use authenticator app") : ar ? "فقدت هاتفي؟ رمز استرداد" : "Lost phone? Recovery code"}
        </button>
      </div>
    </div>
  );
}
