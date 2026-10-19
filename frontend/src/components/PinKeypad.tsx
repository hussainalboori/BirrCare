"use client";

import React from "react";
import { useLanguage } from "@/context/LanguageContext";
import { Delete } from "lucide-react";

interface PinKeypadProps {
  pin: string;
  disabled?: boolean;
  onDigit: (digit: string) => void;
  onDelete: () => void;
  onClear: () => void;
  /** Rendered between the dots and the keypad (error or hint line). */
  message?: React.ReactNode;
}

/** Four PIN dots and the oversized touch keypad shared by the login and change-PIN dialogs. */
export function PinKeypad({ pin, disabled = false, onDigit, onDelete, onClear, message }: PinKeypadProps) {
  const { locale } = useLanguage();

  return (
    <>
      {/* PIN Dots Display */}
      <div className="flex justify-center items-center gap-4 py-3 mb-2">
        {[0, 1, 2, 3].map((index) => {
          const isFilled = pin.length > index;
          return (
            <div
              key={index}
              className={`h-4 w-4 rounded-full transition-all duration-150 ${
                isFilled
                  ? "bg-emerald-600 scale-110 shadow-emerald-500/40 shadow-sm"
                  : "bg-slate-200 dark:bg-slate-700"
              }`}
            />
          );
        })}
      </div>

      {message}

      {/* Oversized Mobile Touch Keypad */}
      {/* dir="ltr": phone keypads keep 1-2-3 left-to-right even in Arabic UIs */}
      <div dir="ltr" className="grid grid-cols-3 gap-2.5 max-w-xs mx-auto">
        {["1", "2", "3", "4", "5", "6", "7", "8", "9"].map((digit) => (
          <button
            type="button"
            key={digit}
            onClick={() => onDigit(digit)}
            disabled={disabled}
            className="flex h-14 items-center justify-center rounded-2xl bg-slate-100 hover:bg-slate-200 active:bg-slate-300 dark:bg-slate-800 dark:hover:bg-slate-700 dark:active:bg-slate-600 text-xl font-bold text-slate-900 dark:text-white transition-transform active:scale-90"
          >
            {digit}
          </button>
        ))}
        <button
          type="button"
          onClick={onClear}
          disabled={disabled}
          aria-label={locale === "ar" ? "مسح" : "Clear"}
          className="flex h-14 items-center justify-center rounded-2xl bg-slate-100/60 hover:bg-slate-100 text-xs font-semibold text-slate-500 dark:bg-slate-800/60 dark:hover:bg-slate-800 dark:text-slate-400 active:scale-95"
        >
          C
        </button>
        <button
          type="button"
          onClick={() => onDigit("0")}
          disabled={disabled}
          className="flex h-14 items-center justify-center rounded-2xl bg-slate-100 hover:bg-slate-200 active:bg-slate-300 dark:bg-slate-800 dark:hover:bg-slate-700 text-xl font-bold text-slate-900 dark:text-white transition-transform active:scale-90"
        >
          0
        </button>
        <button
          type="button"
          onClick={onDelete}
          disabled={disabled}
          className="flex h-14 items-center justify-center rounded-2xl bg-slate-100/60 hover:bg-slate-100 text-slate-600 dark:bg-slate-800/60 dark:hover:bg-slate-800 dark:text-slate-300 active:scale-95"
          aria-label={locale === "ar" ? "حذف رقم" : "Delete digit"}
        >
          <Delete className="h-5 w-5" />
        </button>
      </div>
    </>
  );
}
