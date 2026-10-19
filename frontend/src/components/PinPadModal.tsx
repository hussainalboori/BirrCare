"use client";

import React, { useState, useEffect } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { X, Delete, Lock, CheckCircle2 } from "lucide-react";

export function PinPadModal() {
  const { isPinModalOpen, closePinModal, allCaregivers, targetCaregiverId, verifyPin } = useCaregiver();
  const { t, locale } = useLanguage();

  const [selectedId, setSelectedId] = useState<string>("");
  const [pin, setPin] = useState<string>("");
  const [error, setError] = useState<string>("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    if (isPinModalOpen) {
      setPin("");
      setError("");
      if (targetCaregiverId) {
        setSelectedId(targetCaregiverId);
      } else if (allCaregivers.length > 0) {
        setSelectedId(allCaregivers[0].id);
      }
    }
  }, [isPinModalOpen, targetCaregiverId, allCaregivers]);

  if (!isPinModalOpen) return null;

  const handleDigit = (digit: string) => {
    if (pin.length >= 4 || isSubmitting) return;
    if (typeof window !== "undefined" && "vibrate" in navigator) {
      navigator.vibrate?.(35);
    }
    const newPin = pin + digit;
    setPin(newPin);
    setError("");

    if (newPin.length === 4) {
      submitPin(newPin);
    }
  };

  const handleDelete = () => {
    if (pin.length > 0 && !isSubmitting) {
      if (typeof window !== "undefined" && "vibrate" in navigator) {
        navigator.vibrate?.(25);
      }
      setPin(pin.slice(0, -1));
      setError("");
    }
  };

  const handleClear = () => {
    setPin("");
    setError("");
  };

  const submitPin = async (pinToVerify: string) => {
    setIsSubmitting(true);
    const res = await verifyPin(selectedId, pinToVerify);
    setIsSubmitting(false);

    if (!res.success) {
      setError(t.incorrectPin);
      if (typeof window !== "undefined" && "vibrate" in navigator) {
        navigator.vibrate?.([60, 50, 60]);
      }
      setPin("");
    }
  };

  const selectedCaregiver = allCaregivers.find((c) => c.id === selectedId);

  return (
    <div className="fixed inset-0 z-50 flex items-end sm:items-center justify-center bg-slate-900/60 backdrop-blur-xs p-0 sm:p-4 animate-in fade-in duration-200">
      <div className="relative w-full max-w-md rounded-t-3xl sm:rounded-3xl bg-white p-6 shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800">
        {/* Close Button */}
        <button
          onClick={closePinModal}
          className="absolute top-4 end-4 rounded-full p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800"
        >
          <X className="h-5 w-5" />
        </button>

        {/* Modal Header */}
        <div className="text-center mb-4">
          <div className="inline-flex h-12 w-12 items-center justify-center rounded-2xl bg-emerald-50 text-emerald-600 dark:bg-emerald-950/60 dark:text-emerald-400 mb-2">
            <Lock className="h-6 w-6" />
          </div>
          <h3 className="text-lg font-bold text-slate-900 dark:text-white">
            {t.switchCaregiver}
          </h3>
          <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
            {t.enterPin}
          </p>
        </div>

        {/* Sibling Profile Selection Row */}
        <div className="mb-4">
          <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-2 text-center">
            {t.selectProfile}
          </label>
          <div className="grid grid-cols-4 gap-2">
            {allCaregivers.map((c) => {
              const isSelected = c.id === selectedId;
              return (
                <button
                  key={c.id}
                  onClick={() => {
                    setSelectedId(c.id);
                    setPin("");
                    setError("");
                  }}
                  className={`flex flex-col items-center gap-1.5 p-2 rounded-2xl border transition-all ${
                    isSelected
                      ? "border-emerald-600 bg-emerald-50/50 dark:bg-emerald-950/30 ring-2 ring-emerald-500/30"
                      : "border-slate-200 hover:border-slate-300 dark:border-slate-800 dark:hover:border-slate-700"
                  }`}
                >
                  <div
                    className="relative flex h-11 w-11 items-center justify-center rounded-full text-white font-bold text-sm shadow-xs"
                    style={{ backgroundColor: c.avatar_color }}
                  >
                    {c.name.slice(0, 1)}
                    {isSelected && (
                      <span className="absolute -top-1 -end-1 flex h-4 w-4 items-center justify-center rounded-full bg-emerald-600 text-white">
                        <CheckCircle2 className="h-3 w-3" />
                      </span>
                    )}
                  </div>
                  <span className="text-[11px] font-bold text-slate-800 dark:text-slate-200 truncate w-full text-center">
                    {c.name.split(" ")[0]}
                  </span>
                  <span className="text-[9px] font-semibold text-slate-400 dark:text-slate-500">
                    {c.role === "super_admin"
                      ? (locale === "ar" ? "مشرف" : "Super")
                      : c.role === "admin"
                      ? (locale === "ar" ? "أدوية" : "Admin")
                      : c.role === "viewer"
                      ? (locale === "ar" ? "مشاهد" : "Viewer")
                      : (locale === "ar" ? "رعاية" : "Care")}
                  </span>
                </button>
              );
            })}
          </div>
        </div>

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

        {/* Error message */}
        {error ? (
          <p className="text-center text-xs font-semibold text-rose-600 dark:text-rose-400 mb-3 animate-shake">
            {error}
          </p>
        ) : (
          <p className="text-center text-[11px] text-slate-400 mb-3">
            {t.pinHint}
          </p>
        )}

        {/* Oversized Mobile Touch Keypad */}
        <div className="grid grid-cols-3 gap-2.5 max-w-xs mx-auto">
          {["1", "2", "3", "4", "5", "6", "7", "8", "9"].map((digit) => (
            <button
              key={digit}
              onClick={() => handleDigit(digit)}
              disabled={isSubmitting}
              className="flex h-14 items-center justify-center rounded-2xl bg-slate-100 hover:bg-slate-200 active:bg-slate-300 dark:bg-slate-800 dark:hover:bg-slate-700 dark:active:bg-slate-600 text-xl font-bold text-slate-900 dark:text-white transition-transform active:scale-90"
            >
              {digit}
            </button>
          ))}
          <button
            onClick={handleClear}
            disabled={isSubmitting}
            className="flex h-14 items-center justify-center rounded-2xl bg-slate-100/60 hover:bg-slate-100 text-xs font-semibold text-slate-500 dark:bg-slate-800/60 dark:hover:bg-slate-800 dark:text-slate-400 active:scale-95"
          >
            C
          </button>
          <button
            onClick={() => handleDigit("0")}
            disabled={isSubmitting}
            className="flex h-14 items-center justify-center rounded-2xl bg-slate-100 hover:bg-slate-200 active:bg-slate-300 dark:bg-slate-800 dark:hover:bg-slate-700 text-xl font-bold text-slate-900 dark:text-white transition-transform active:scale-90"
          >
            0
          </button>
          <button
            onClick={handleDelete}
            disabled={isSubmitting}
            className="flex h-14 items-center justify-center rounded-2xl bg-slate-100/60 hover:bg-slate-100 text-slate-600 dark:bg-slate-800/60 dark:hover:bg-slate-800 dark:text-slate-300 active:scale-95"
            aria-label="Delete"
          >
            <Delete className="h-5 w-5" />
          </button>
        </div>
      </div>
    </div>
  );
}
