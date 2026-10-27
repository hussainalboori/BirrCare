"use client";

import React, { useState, useEffect, useCallback } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { PairedDevice, TrustStatus } from "@/lib/types";
import { X, Tablet, ShieldCheck, ShieldAlert, Trash2, CheckCircle2, Laptop } from "lucide-react";

interface DevicePairingModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function DevicePairingModal({ isOpen, onClose }: DevicePairingModalProps) {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const { trustStatus, checkTrustStatus } = useCaregiver();

  const [devices, setDevices] = useState<PairedDevice[]>([]);
  const [loading, setLoading] = useState(false);
  const [pairingName, setPairingName] = useState("");
  const [pairingPin, setPairingPin] = useState("");
  const [pairingLoading, setPairingLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const devList = await fetchApi<PairedDevice[]>("/auth/paired-devices").catch(() => []);
      setDevices(devList || []);
    } catch (err: any) {
      setError(err?.message || (ar ? "تعذر تحميل البيانات" : "Failed loading data"));
    } finally {
      setLoading(false);
    }
  }, [ar]);

  useEffect(() => {
    if (isOpen) {
      loadData();
      checkTrustStatus();
    }
  }, [isOpen, loadData, checkTrustStatus]);

  if (!isOpen) return null;

  const handlePairCurrentDevice = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!pairingName.trim() || pairingPin.length !== 4) return;
    setPairingLoading(true);
    setError(null);
    setSuccess(null);
    try {
      await fetchApi("/auth/pair-device", {
        method: "POST",
        // The admin's own PIN: a paired device skips the PIN for everyone.
        body: JSON.stringify({ device_name: pairingName.trim(), pin: pairingPin }),
      });
      setSuccess(ar ? "تم اعتماد هذا الجهاز بنجاح كجهاز منزلي" : "Device paired successfully as a trusted home device");
      setPairingName("");
      setPairingPin("");
      await loadData();
      await checkTrustStatus();
    } catch {
      setPairingPin("");
      setError(ar ? "تعذر اعتماد الجهاز: تأكد من رمزك السري" : "Could not pair the device: check your PIN");
    } finally {
      setPairingLoading(false);
    }
  };

  const handleRevokeDevice = async (id: string) => {
    if (!window.confirm(ar ? "هل أنت متأكد من إزالة هذا الجهاز المعتمد؟" : "Revoke this trusted device?")) {
      return;
    }
    setError(null);
    try {
      await fetchApi(`/auth/paired-devices/${id}`, { method: "DELETE" });
      await loadData();
      await checkTrustStatus();
    } catch (err: any) {
      setError(err?.message || (ar ? "فشل إلغاء اعتماد الجهاز" : "Failed to revoke device"));
    }
  };

  const getReasonLabel = (trust?: TrustStatus | null) => {
    if (!trust?.trusted) {
      return ar ? "خارج المنزل / غير معتمد (يلزم رمز المرور)" : "External / Untrusted (PIN Required)";
    }
    switch (trust.reason) {
      case "paired_device":
        return ar
          ? `جهاز منزلي معتمد (${trust.device_name || "جهاز سرير المريض"})`
          : `Paired Home Device (${trust.device_name || "Bedside Device"})`;
      case "trusted_ip":
        return ar ? "عنوان IP منزلي معتمد" : "Trusted Home IP";
      default:
        return ar ? "شبكة المنزل" : "Home Network";
    }
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="device-pairing-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-xs"
      onClick={onClose}
    >
      <div
        className="flex max-h-[90dvh] w-full max-w-lg flex-col overflow-hidden rounded-3xl bg-white shadow-2xl dark:bg-slate-900"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-slate-200 px-6 py-4 dark:border-slate-800">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-2xl bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-400">
              <Tablet className="h-5 w-5" />
            </div>
            <div>
              <h2 id="device-pairing-title" className="text-base font-bold text-slate-900 dark:text-white">
                {ar ? "أجهزة وشبكة المنزل (التبديل الذكي)" : "Home Devices & Smart Network"}
              </h2>
              <p className="text-xs text-slate-500 dark:text-slate-400">
                {ar
                  ? "التبديل بلمسة واحدة لمقدمي الرعاية على أجهزة المنزل المعتمدة؛ المشرفون يدخلون رمزهم دائماً"
                  : "1-tap switching for caregivers on trusted home devices; admins always enter their PIN"}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={ar ? "إغلاق" : "Close"}
            className="flex min-h-10 min-w-10 items-center justify-center rounded-full text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-6 space-y-6">
          {error && (
            <div className="flex items-center gap-2 rounded-xl bg-rose-50 p-3 text-xs font-semibold text-rose-800 dark:bg-rose-950/60 dark:text-rose-300">
              <ShieldAlert className="h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {success && (
            <div className="flex items-center gap-2 rounded-xl bg-emerald-50 p-3 text-xs font-semibold text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300">
              <CheckCircle2 className="h-4 w-4 shrink-0" />
              <span>{success}</span>
            </div>
          )}

          {/* Current Status Card */}
          <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 dark:border-slate-800 dark:bg-slate-800/40">
            <span className="text-xs font-bold text-slate-500 dark:text-slate-400">
              {ar ? "حالة هذا الجهاز الآن:" : "Current Device Status:"}
            </span>
            <div className="mt-2 flex items-center justify-between">
              <div className="flex items-center gap-2">
                {trustStatus?.trusted ? (
                  <CheckCircle2 className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
                ) : (
                  <ShieldAlert className="h-5 w-5 text-amber-500" />
                )}
                <div>
                  <div className="text-sm font-bold text-slate-900 dark:text-white">
                    {trustStatus?.trusted ? (ar ? "معتمد (تبديل بلمسة واحدة)" : "Trusted (1-Tap Switch)") : (ar ? "غير معتمد (يلزم رمز المرور)" : "Untrusted (PIN Required)")}
                  </div>
                  <div className="text-xs text-slate-500 dark:text-slate-400">
                    {getReasonLabel(trustStatus)}
                  </div>
                </div>
              </div>
              <button
                type="button"
                onClick={() => checkTrustStatus()}
                className="rounded-lg border border-slate-300 bg-white px-2.5 py-1 text-xs font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300"
              >
                {ar ? "تحديث" : "Refresh"}
              </button>
            </div>
          </div>

          {/* Pair This Device Section */}
          <div className="rounded-2xl border border-slate-200 p-4 dark:border-slate-800">
            <div className="flex items-center gap-2">
              <Laptop className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
              <h3 className="text-sm font-bold text-slate-900 dark:text-white">
                {ar ? "اعتماد هذا الجهاز كجهاز منزلي دائم" : "Pair This Device as a Home Device"}
              </h3>
            </div>
            <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
              {ar
                ? "يسمح للأجهزة الثابتة (مثل آيباد سرير المريض) بالتبديل السريع حتى لو تغيّر عنوان IP الخاص بالمودم المنزلي."
                : "Allows stationary devices (like a bedside tablet) to switch profiles instantly even if the home router dynamic IP changes."}
            </p>

            <form onSubmit={handlePairCurrentDevice} className="mt-3 flex flex-wrap gap-2">
              <input
                type="text"
                value={pairingName}
                onChange={(e) => setPairingName(e.target.value)}
                placeholder={ar ? "مثال: آيباد غرفة الوالد" : "e.g., Bedside iPad"}
                className="min-w-40 flex-1 rounded-xl border border-slate-200 bg-white px-3 py-2 text-base text-slate-900 placeholder-slate-400 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
              />
              <input
                type="password"
                inputMode="numeric"
                autoComplete="off"
                maxLength={4}
                data-testid="pairing-pin"
                aria-label={ar ? "رمزك السري (4 أرقام)" : "Your PIN (4 digits)"}
                value={pairingPin}
                onChange={(e) => setPairingPin(e.target.value.replace(/\D/g, "").slice(0, 4))}
                placeholder={ar ? "رمزك السري" : "Your PIN"}
                className="w-28 rounded-xl border border-slate-200 bg-white px-3 py-2 text-base text-slate-900 placeholder-slate-400 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
              />
              <button
                type="submit"
                disabled={pairingLoading || !pairingName.trim() || pairingPin.length !== 4}
                className="rounded-xl bg-emerald-600 px-4 py-2 text-xs font-bold text-white transition-opacity hover:bg-emerald-700 disabled:opacity-50"
              >
                {pairingLoading ? (ar ? "جارٍ الحفظ..." : "Pairing...") : (ar ? "اعتماد الجهاز" : "Pair Device")}
              </button>
            </form>
          </div>

          {/* Paired Devices List */}
          <div className="space-y-3">
            <h3 className="text-xs font-bold text-slate-500 dark:text-slate-400">
              {ar ? "الأجهزة المنزلية المعتمدة" : "Paired Home Devices"} ({devices.length})
            </h3>
            {loading ? (
              <div className="py-4 text-center text-xs text-slate-400">{ar ? "جارٍ التحميل..." : "Loading..."}</div>
            ) : devices.length === 0 ? (
              <div className="rounded-xl border border-dashed border-slate-200 p-4 text-center text-xs text-slate-400 dark:border-slate-800">
                {ar ? "لا توجد أجهزة معتمدة حالياً." : "No paired devices yet."}
              </div>
            ) : (
              <div className="space-y-2">
                {devices.map((dev) => (
                  <div
                    key={dev.id}
                    className="flex items-center justify-between rounded-xl border border-slate-200 bg-slate-50/50 p-3 dark:border-slate-800 dark:bg-slate-800/30"
                  >
                    <div className="flex items-center gap-2.5">
                      <Tablet className="h-4 w-4 text-slate-500 dark:text-slate-400" />
                      <div>
                        <div className="text-xs font-bold text-slate-800 dark:text-slate-200">{dev.device_name}</div>
                        <div className="text-xs text-slate-400">
                          {ar ? "أُضيف في:" : "Added:"} {dev.created_at.slice(0, 10)}
                        </div>
                      </div>
                    </div>
                    <button
                      type="button"
                      onClick={() => handleRevokeDevice(dev.id)}
                      title={ar ? "إلغاء الاعتماد" : "Revoke"}
                      className="flex h-8 w-8 items-center justify-center rounded-lg text-rose-500 hover:bg-rose-50 dark:hover:bg-rose-950/40"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
