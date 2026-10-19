"use client";

import React, { useEffect, useState } from "react";
import QRCode from "qrcode";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { fetchApi } from "@/lib/api";
import { PinKeypad } from "./PinKeypad";
import { X, ShieldCheck, ShieldOff, Copy, Check, Loader2, Smartphone } from "lucide-react";

type Step = "status" | "pin" | "scan" | "codes" | "disable";

interface Props {
  isOpen: boolean;
  onClose: () => void;
}

/** Super Admin: turn the authenticator app (2FA) on or off. */
export function TwoFactorModal({ isOpen, onClose }: Props) {
  const { currentCaregiver, refreshCaregiver } = useCaregiver();
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const enabled = !!currentCaregiver?.totp_enabled;

  const [step, setStep] = useState<Step>("status");
  const [pin, setPin] = useState("");
  const [code, setCode] = useState("");
  const [setup, setSetup] = useState<{ secret: string; uri: string; qr: string } | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (isOpen) {
      setStep("status");
      setPin("");
      setCode("");
      setSetup(null);
      setRecoveryCodes([]);
      setError("");
    }
  }, [isOpen]);

  if (!isOpen) return null;

  const describe = (err: any) => {
    const c = err?.data?.error;
    if (c === "INCORRECT_PIN") return ar ? "الرمز السري غير صحيح" : "Incorrect PIN";
    if (c === "INCORRECT_CODE") return ar ? "الرمز غير صحيح، تأكد من وقت الهاتف واستخدم أحدث رمز" : "Code doesn't match; check the phone's time and use the newest code";
    if (err?.status === 429) return ar ? "محاولات كثيرة، حاول لاحقاً" : "Too many attempts, try again later";
    return err?.data?.message || err?.message || (ar ? "حدث خطأ" : "Something went wrong");
  };

  // PIN entered on the keypad: start setup (when off) or move to the code (when turning off).
  const onPinComplete = async (value: string) => {
    if (enabled) {
      setStep("disable");
      return;
    }
    setBusy(true);
    try {
      const res = await fetchApi<{ secret: string; uri: string }>("/auth/2fa/setup", {
        method: "POST",
        body: JSON.stringify({ pin: value }),
      });
      const qr = await QRCode.toDataURL(res.uri, { margin: 1, width: 240 });
      setSetup({ ...res, qr });
      setStep("scan");
    } catch (err) {
      setError(describe(err));
      setPin("");
    } finally {
      setBusy(false);
    }
  };

  const enable = async (value: string = code) => {
    setBusy(true);
    setError("");
    try {
      const res = await fetchApi<{ recovery_codes: string[] }>("/auth/2fa/enable", {
        method: "POST",
        body: JSON.stringify({ code: value }),
      });
      setRecoveryCodes(res.recovery_codes || []);
      setStep("codes");
    } catch (err) {
      setError(describe(err));
      setCode("");
    } finally {
      setBusy(false);
    }
  };

  const disable = async () => {
    setBusy(true);
    setError("");
    try {
      await fetchApi("/auth/2fa/disable", { method: "POST", body: JSON.stringify({ pin, code }) });
      await refreshCaregiver();
      onClose();
    } catch (err) {
      const e = describe(err);
      if ((err as any)?.data?.error === "INCORRECT_PIN") {
        setPin("");
        setStep("pin");
      }
      setError(e);
      setCode("");
    } finally {
      setBusy(false);
    }
  };

  const finish = async () => {
    await refreshCaregiver();
    onClose();
  };

  const copyCodes = async () => {
    try {
      await navigator.clipboard.writeText(recoveryCodes.join("\n"));
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // clipboard blocked: the codes are on screen to write down
    }
  };

  const codeInput = (onDone: (value: string) => void) => (
    <input
      type="text"
      dir="ltr"
      inputMode="numeric"
      autoComplete="one-time-code"
      value={code}
      disabled={busy}
      onChange={(e) => {
        const v = e.target.value.replace(/\D/g, "").slice(0, 6);
        setCode(v);
        setError("");
        if (v.length === 6) onDone(v);
      }}
      placeholder="000000"
      aria-label={ar ? "رمز المصادقة" : "Authenticator code"}
      className="min-h-14 w-full rounded-2xl border-2 border-slate-200 bg-white px-4 text-center font-mono text-3xl font-black tracking-[0.3em] text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
    />
  );

  const errorLine = error && (
    <p role="alert" className="text-center text-xs font-semibold text-rose-600 dark:text-rose-400">
      {error}
    </p>
  );

  return (
    <div className="fixed inset-0 z-50 flex items-end sm:items-center justify-center bg-slate-900/60 backdrop-blur-xs p-0 sm:p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-label={ar ? "تطبيق المصادقة" : "Authenticator app"}
        className="relative w-full max-w-md max-h-dvh overflow-y-auto rounded-t-3xl sm:rounded-3xl bg-white p-6 pb-[calc(1.5rem+env(safe-area-inset-bottom))] shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800"
      >
        {step !== "codes" && (
          <button
            type="button"
            onClick={onClose}
            aria-label={ar ? "إغلاق" : "Close"}
            className="absolute top-3 end-3 flex min-h-12 min-w-12 items-center justify-center rounded-full text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800"
          >
            <X className="h-5 w-5" />
          </button>
        )}

        <div className="mb-4 text-center">
          <div className={`mb-2 inline-flex h-12 w-12 items-center justify-center rounded-2xl ${enabled || step === "codes" ? "bg-emerald-50 text-emerald-600 dark:bg-emerald-950/60 dark:text-emerald-400" : "bg-amber-50 text-amber-600 dark:bg-amber-950/60 dark:text-amber-400"}`}>
            {enabled || step === "codes" ? <ShieldCheck className="h-6 w-6" /> : <ShieldOff className="h-6 w-6" />}
          </div>
          <h3 className="text-lg font-bold text-slate-900 dark:text-white">{ar ? "تطبيق المصادقة (2FA)" : "Authenticator app (2FA)"}</h3>
        </div>

        {step === "status" && (
          <div className="space-y-4">
            <p className="text-sm leading-relaxed text-slate-600 dark:text-slate-300">
              {enabled
                ? ar
                  ? "مفعّل: الدخول لحساب المشرف العام يحتاج الرمز السري ورمزاً من تطبيق المصادقة. لو انكشف الرمز السري وحده فلن يستطيع أحد الدخول."
                  : "On: signing in to this Super Admin account needs the PIN and a code from your authenticator app. A leaked PIN alone can't get in."
                : ar
                  ? "غير مفعّل: أي شخص يعرف الرمز السري يستطيع الدخول بصلاحيات المشرف العام. فعّل تطبيق المصادقة (مثل Google Authenticator أو كلمات السر في iPhone)."
                  : "Off: anyone who learns the PIN gets full Super Admin access. Turn on an authenticator app (Google Authenticator, Microsoft Authenticator, iPhone Passwords)."}
            </p>
            <button
              type="button"
              onClick={() => setStep("pin")}
              className={`min-h-12 w-full rounded-2xl text-sm font-bold text-white active:scale-95 ${enabled ? "bg-rose-600" : "bg-emerald-600"}`}
            >
              {enabled ? (ar ? "إيقاف تطبيق المصادقة" : "Turn off") : ar ? "تفعيل تطبيق المصادقة" : "Turn on"}
            </button>
          </div>
        )}

        {step === "pin" && (
          <PinKeypad
            pin={pin}
            disabled={busy}
            onDigit={(d) => {
              if (pin.length >= 4 || busy) return;
              const next = pin + d;
              setPin(next);
              setError("");
              if (next.length === 4) onPinComplete(next);
            }}
            onDelete={() => setPin((p) => p.slice(0, -1))}
            onClear={() => setPin("")}
            message={
              <p className={`mb-3 text-center ${error ? "text-xs font-semibold text-rose-600 dark:text-rose-400" : "text-xs text-slate-500 dark:text-slate-400"}`}>
                {error || (ar ? "أدخل رمزك السري للتأكيد" : "Enter your PIN to confirm")}
              </p>
            }
          />
        )}

        {step === "scan" && setup && (
          <div className="space-y-4">
            <ol className="list-decimal space-y-1 ps-5 text-sm text-slate-600 dark:text-slate-300">
              <li>{ar ? "افتح تطبيق المصادقة على هاتفك واختر إضافة حساب." : "Open your authenticator app and add an account."}</li>
              <li>{ar ? "امسح الرمز، أو اضغط الزر إن كنت على نفس الهاتف." : "Scan the code, or tap the button if you're on that phone."}</li>
              <li>{ar ? "أدخل الرمز المكوّن من 6 أرقام الذي يظهر." : "Enter the 6-digit code it shows."}</li>
            </ol>
            {/* QR codes must stay dark-on-light to scan, even in dark mode */}
            <img src={setup.qr} alt={ar ? "رمز QR لتطبيق المصادقة" : "Authenticator QR code"} className="mx-auto h-56 w-56 rounded-2xl bg-white p-2" />
            <a
              href={setup.uri}
              className="flex min-h-12 items-center justify-center gap-2 rounded-2xl border border-slate-200 text-sm font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200"
            >
              <Smartphone className="h-4 w-4" />
              {ar ? "فتح في تطبيق المصادقة" : "Open in authenticator app"}
            </a>
            <details className="text-xs text-slate-500 dark:text-slate-400">
              <summary className="min-h-12 cursor-pointer py-3">{ar ? "إدخال يدوي" : "Enter key manually"}</summary>
              <code dir="ltr" className="block break-all rounded-xl bg-slate-100 p-3 font-mono text-sm text-slate-800 dark:bg-slate-800 dark:text-slate-100">
                {setup.secret.match(/.{1,4}/g)?.join(" ")}
              </code>
            </details>
            {codeInput(enable)}
            {errorLine}
            <button
              type="button"
              onClick={() => enable()}
              disabled={busy || code.length !== 6}
              className="flex min-h-12 w-full items-center justify-center gap-2 rounded-2xl bg-emerald-600 text-sm font-bold text-white disabled:opacity-50"
            >
              {busy && <Loader2 className="h-4 w-4 animate-spin" />}
              {ar ? "تأكيد وتفعيل" : "Confirm and turn on"}
            </button>
          </div>
        )}

        {step === "codes" && (
          <div className="space-y-4">
            <p className="text-sm font-semibold text-emerald-700 dark:text-emerald-400">
              {ar ? "تم التفعيل. تم تسجيل الخروج من الأجهزة الأخرى." : "Turned on. Other devices were signed out."}
            </p>
            <div className="rounded-2xl border-2 border-amber-400 bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
              {ar
                ? "احفظ رموز الاسترداد هذه في مكان آمن (ورقة أو مدير كلمات السر). كل رمز يُستخدم مرة واحدة إذا فقدت هاتفك. لن تظهر مرة أخرى."
                : "Save these recovery codes somewhere safe (paper or a password manager). Each works once if you lose your phone. They won't be shown again."}
            </div>
            <ul dir="ltr" className="grid grid-cols-2 gap-2 font-mono text-base font-bold text-slate-900 dark:text-white">
              {recoveryCodes.map((c) => (
                <li key={c} className="rounded-xl bg-slate-100 py-2 text-center dark:bg-slate-800">
                  {c}
                </li>
              ))}
            </ul>
            <button
              type="button"
              onClick={copyCodes}
              className="flex min-h-12 w-full items-center justify-center gap-2 rounded-2xl border border-slate-200 text-sm font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200"
            >
              {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
              {copied ? (ar ? "تم النسخ" : "Copied") : ar ? "نسخ الرموز" : "Copy codes"}
            </button>
            <button type="button" onClick={finish} className="min-h-12 w-full rounded-2xl bg-emerald-600 text-sm font-bold text-white">
              {ar ? "حفظتها، إنهاء" : "I saved them, finish"}
            </button>
          </div>
        )}

        {step === "disable" && (
          <div className="space-y-4">
            <p className="text-center text-sm text-slate-600 dark:text-slate-300">
              {ar ? "أدخل رمزاً من تطبيق المصادقة أو رمز استرداد لإيقافه." : "Enter a code from the authenticator app (or a recovery code) to turn it off."}
            </p>
            <input
              type="text"
              dir="ltr"
              autoComplete="one-time-code"
              value={code}
              disabled={busy}
              onChange={(e) => {
                setCode(e.target.value.slice(0, 16));
                setError("");
              }}
              placeholder="000000"
              aria-label={ar ? "رمز المصادقة" : "Authenticator code"}
              className="min-h-14 w-full rounded-2xl border-2 border-slate-200 bg-white px-4 text-center font-mono text-2xl font-black text-slate-900 focus:border-rose-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
            />
            {errorLine}
            <button
              type="button"
              onClick={disable}
              disabled={busy || !code.trim()}
              className="flex min-h-12 w-full items-center justify-center gap-2 rounded-2xl bg-rose-600 text-sm font-bold text-white disabled:opacity-50"
            >
              {busy && <Loader2 className="h-4 w-4 animate-spin" />}
              {ar ? "إيقاف تطبيق المصادقة" : "Turn off two-factor"}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
