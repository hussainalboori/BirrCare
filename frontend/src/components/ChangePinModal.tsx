"use client";

import React, { useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { fetchApi } from "@/lib/api";
import { PinKeypad } from "./PinKeypad";
import { X, KeyRound, CheckCircle2 } from "lucide-react";

type Step = "current" | "new" | "confirm" | "done";

const isWeak = (pin: string) => pin === "1234" || /^(\d)\1{3}$/.test(pin);

interface ChangePinModalProps {
  isOpen: boolean;
  onClose: () => void;
}

/** Current PIN → new PIN → confirm, on the same keypad as the login screen. */
export function ChangePinModal({ isOpen, onClose }: ChangePinModalProps) {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const [step, setStep] = useState<Step>("current");
  const [pin, setPin] = useState("");
  const [currentPin, setCurrentPin] = useState("");
  const [newPin, setNewPin] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (isOpen) {
      setStep("current");
      setPin("");
      setCurrentPin("");
      setNewPin("");
      setError("");
    }
  }, [isOpen]);

  if (!isOpen) return null;

  const buzz = (pattern: number | number[]) => {
    if (typeof navigator !== "undefined" && "vibrate" in navigator) navigator.vibrate?.(pattern);
  };

  const fail = (message: string, backTo: Step) => {
    setError(message);
    setStep(backTo);
    setPin("");
    buzz([60, 50, 60]);
  };

  const submit = async (confirmed: string) => {
    setSubmitting(true);
    try {
      await fetchApi("/auth/change-pin", {
        method: "POST",
        body: JSON.stringify({ current_pin: currentPin, new_pin: confirmed }),
      });
      setStep("done");
      setPin("");
      buzz(40);
    } catch (err: any) {
      const code = err?.data?.error;
      if (code === "INCORRECT_PIN") {
        fail(ar ? "الرمز الحالي غير صحيح" : "Current PIN is incorrect", "current");
      } else if (err?.status === 429) {
        const mins = err.retryAfter ? Math.max(1, Math.ceil(err.retryAfter / 60)) : null;
        fail(
          ar
            ? `تم إيقاف المحاولات مؤقتاً.${mins ? ` حاول بعد ${mins} دقيقة.` : ""}`
            : `Too many incorrect attempts.${mins ? ` Try again in ${mins} min.` : ""}`,
          "current"
        );
      } else if (code === "SAME_PIN") {
        fail(ar ? "الرمز الجديد مطابق للرمز الحالي" : "New PIN is the same as the current one", "new");
      } else if (code === "WEAK_PIN") {
        fail(ar ? "اختر رمزاً غير 1234 وغير مكرر الأرقام" : "Avoid 1234 and repeated digits", "new");
      } else {
        fail(ar ? "تعذر الاتصال بالخادم، حاول مجدداً" : "Could not reach the server, please retry", "current");
      }
    } finally {
      setSubmitting(false);
    }
  };

  const handleDigit = (digit: string) => {
    if (pin.length >= 4 || submitting || step === "done") return;
    buzz(35);
    const next = pin + digit;
    setPin(next);
    setError("");
    if (next.length < 4) return;

    if (step === "current") {
      setCurrentPin(next);
      setStep("new");
      setPin("");
    } else if (step === "new") {
      if (isWeak(next)) {
        fail(ar ? "اختر رمزاً غير 1234 وغير مكرر الأرقام" : "Avoid 1234 and repeated digits", "new");
        return;
      }
      setNewPin(next);
      setStep("confirm");
      setPin("");
    } else if (step === "confirm") {
      if (next !== newPin) {
        fail(ar ? "الرمزان غير متطابقين، أدخل الرمز الجديد مرة أخرى" : "PINs don't match, enter the new PIN again", "new");
        return;
      }
      submit(next);
    }
  };

  const title = {
    current: ar ? "أدخل رمزك الحالي" : "Enter your current PIN",
    new: ar ? "أدخل الرمز الجديد" : "Enter a new PIN",
    confirm: ar ? "أعد إدخال الرمز الجديد" : "Enter the new PIN again",
    done: ar ? "تم تغيير الرمز" : "PIN changed",
  }[step];

  return (
    <div className="fixed inset-0 z-50 flex items-end sm:items-center justify-center bg-slate-900/60 backdrop-blur-xs p-0 sm:p-4 animate-in fade-in duration-200">
      <div
        role="dialog"
        aria-modal="true"
        aria-label={ar ? "تغيير رمز PIN" : "Change PIN"}
        className="relative w-full max-w-md max-h-dvh overflow-y-auto rounded-t-3xl sm:rounded-3xl bg-white p-6 pb-[calc(1.5rem+env(safe-area-inset-bottom))] shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800"
      >
        <button
          type="button"
          onClick={onClose}
          aria-label={ar ? "إغلاق" : "Close"}
          className="absolute top-3 end-3 flex min-h-12 min-w-12 items-center justify-center rounded-full p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800"
        >
          <X className="h-5 w-5" />
        </button>

        <div className="text-center mb-4">
          <div className="inline-flex h-12 w-12 items-center justify-center rounded-2xl bg-emerald-50 text-emerald-600 dark:bg-emerald-950/60 dark:text-emerald-400 mb-2">
            {step === "done" ? <CheckCircle2 className="h-6 w-6" /> : <KeyRound className="h-6 w-6" />}
          </div>
          <h3 className="text-lg font-bold text-slate-900 dark:text-white">{title}</h3>
          {step !== "done" && (
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
              {ar ? "الخطوة" : "Step"} {step === "current" ? 1 : step === "new" ? 2 : 3} / 3
            </p>
          )}
        </div>

        {step === "done" ? (
          <div className="space-y-4 text-center">
            <p className="text-sm text-slate-600 dark:text-slate-300">
              {ar ? "استخدم الرمز الجديد في المرة القادمة التي تبدّل فيها الحساب." : "Use the new PIN next time you switch profile."}
            </p>
            <button
              type="button"
              onClick={onClose}
              className="min-h-12 w-full rounded-2xl bg-emerald-600 text-sm font-bold text-white active:scale-95"
            >
              {ar ? "تم" : "Done"}
            </button>
          </div>
        ) : (
          <PinKeypad
            pin={pin}
            disabled={submitting}
            onDigit={handleDigit}
            onDelete={() => {
              setPin((p) => p.slice(0, -1));
              setError("");
            }}
            onClear={() => {
              setPin("");
              setError("");
            }}
            message={
              <p
                role={error ? "alert" : undefined}
                className={`text-center mb-3 ${error ? "text-xs font-semibold text-rose-600 dark:text-rose-400 animate-shake" : "text-[11px] text-slate-400"}`}
              >
                {error || (step === "new" ? (ar ? "تجنّب 1234 والأرقام المكررة" : "Avoid 1234 and repeated digits") : " ")}
              </p>
            }
          />
        )}
      </div>
    </div>
  );
}
