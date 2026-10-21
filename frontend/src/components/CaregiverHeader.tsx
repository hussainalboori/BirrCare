"use client";

import React, { useState } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { ConnectionStatus } from "@/hooks/useLiveSync";
import { User, Users, Globe, ShieldAlert, Pill, Eye, Heart, ShieldCheck, Calendar, Clock, KeyRound, ShieldOff, Menu, X, Tablet } from "lucide-react";
import { fetchApi } from "@/lib/api";
import type { LucideIcon } from "lucide-react";
import { ROLE_METAS, isSuperAdmin, canManageMedications, isViewer } from "@/lib/permissions";
import { UserManagementModal } from "./UserManagementModal";
import { MedicationCatalogModal } from "./MedicationCatalogModal";
import { ScheduleManagementModal } from "./ScheduleManagementModal";
import { ShiftManagementModal } from "./ShiftManagementModal";
import { ChangePinModal } from "./ChangePinModal";
import { TwoFactorModal } from "./TwoFactorModal";

interface CaregiverHeaderProps {
  connectionStatus?: ConnectionStatus;
  onMedicationsUpdated?: () => void;
}

export function CaregiverHeader({ connectionStatus = "connected", onMedicationsUpdated }: CaregiverHeaderProps) {
  const { currentCaregiver, openPinModal, refreshCaregiver } = useCaregiver();
  const { locale, toggleLocale, t } = useLanguage();

  const [isUserModalOpen, setIsUserModalOpen] = useState(false);
  const [isMedModalOpen, setIsMedModalOpen] = useState(false);
  const [isScheduleModalOpen, setIsScheduleModalOpen] = useState(false);
  const [isShiftModalOpen, setIsShiftModalOpen] = useState(false);
  const [isChangePinOpen, setIsChangePinOpen] = useState(false);
  const [isTwoFactorOpen, setIsTwoFactorOpen] = useState(false);

  const role = currentCaregiver?.role || "caregiver";
  const roleMeta = ROLE_METAS[role] || ROLE_METAS.caregiver;
  const needs2FA = isSuperAdmin(role) && !!currentCaregiver && !currentCaregiver.totp_enabled;
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const ar = locale === "ar";
  const shared = !!currentCaregiver?.shared_device;

  // Issue #17: a shared tablet signs each person out after 20 minutes without use.
  const [sharedError, setSharedError] = useState(false);
  const toggleShared = async () => {
    setSharedError(false);
    try {
      await fetchApi("/auth/device-mode", { method: "POST", body: JSON.stringify({ shared: !shared }) });
      await refreshCaregiver();
    } catch {
      setSharedError(true);
    }
  };

  const actions: { key: string; icon: LucideIcon; label: string; short: string; tone: string; onClick: () => void; hint?: string }[] = [
    ...(isSuperAdmin(role)
      ? [
          { key: "users", icon: ShieldAlert, label: t.userManagement, short: ar ? "المستخدمون" : "Users", tone: "bg-purple-50 hover:bg-purple-100 text-purple-700 dark:bg-purple-950/60 dark:text-purple-300 dark:hover:bg-purple-900/60", onClick: () => setIsUserModalOpen(true) },
          { key: "shifts", icon: Clock, label: t.manageShifts, short: ar ? "الورديات" : "Shifts", tone: "bg-blue-50 hover:bg-blue-100 text-blue-700 dark:bg-blue-950/60 dark:text-blue-300 dark:hover:bg-blue-900/60", onClick: () => setIsShiftModalOpen(true) },
        ]
      : []),
    ...(canManageMedications(role)
      ? [
          { key: "schedule", icon: Calendar, label: t.manageSchedule, short: ar ? "مواعيد الأدوية" : "Schedule", tone: "bg-emerald-50 hover:bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300 dark:hover:bg-emerald-900/60", onClick: () => setIsScheduleModalOpen(true) },
          { key: "meds", icon: Pill, label: t.manageMedications, short: ar ? "الأدوية" : "Medicines", tone: "bg-amber-50 hover:bg-amber-100 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300 dark:hover:bg-amber-900/60", onClick: () => setIsMedModalOpen(true) },
        ]
      : []),
    ...(currentCaregiver
      ? [{ key: "pin", icon: KeyRound, label: ar ? "تغيير الرمز السري" : "Change PIN", short: ar ? "تغيير الرمز" : "My PIN", tone: "bg-slate-100 hover:bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700", onClick: () => setIsChangePinOpen(true) }]
      : []),
    ...(isSuperAdmin(role)
      ? [{
          key: "2fa",
          icon: currentCaregiver?.totp_enabled ? ShieldCheck : ShieldOff,
          label: ar ? "تطبيق المصادقة (2FA)" : "Authenticator (2FA)",
          short: ar ? "المصادقة الثنائية" : "2-step login",
          hint: currentCaregiver?.totp_enabled ? (ar ? "مفعّل" : "On") : ar ? "غير مفعّل" : "Off",
          tone: currentCaregiver?.totp_enabled
            ? "bg-slate-100 hover:bg-slate-200 text-emerald-700 dark:bg-slate-800 dark:text-emerald-400 dark:hover:bg-slate-700"
            : "bg-amber-50 hover:bg-amber-100 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300",
          onClick: () => setIsTwoFactorOpen(true),
        }]
      : []),
    ...(currentCaregiver
      ? [{
          key: "shared",
          icon: Tablet,
          label: ar
            ? "جهاز مشترك: يُطلب الرمز بعد ٢٠ دقيقة بدون استخدام"
            : "Shared device: ask for the PIN after 20 min unused",
          short: ar ? `جهاز مشترك: ${shared ? "مفعّل" : "لا"}` : `Shared device: ${shared ? "On" : "Off"}`,
          hint: sharedError ? (ar ? "تعذر الحفظ" : "Not saved") : shared ? (ar ? "مفعّل" : "On") : ar ? "غير مفعّل" : "Off",
          tone: shared
            ? "bg-sky-50 hover:bg-sky-100 text-sky-800 dark:bg-sky-950/60 dark:text-sky-300"
            : "bg-slate-100 hover:bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700",
          onClick: toggleShared,
        }]
      : []),
    { key: "lang", icon: Globe, label: ar ? "English" : "العربية", short: ar ? "English" : "العربية", tone: "border border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800", onClick: toggleLocale },
  ];

  return (
    <>
      <header className="sticky top-0 z-30 w-full border-b border-slate-200/80 bg-white/90 backdrop-blur-md dark:border-slate-800 dark:bg-slate-900/90 shadow-xs">
        <div className="mx-auto flex max-w-3xl items-center justify-between gap-3 px-4 sm:px-6 py-2.5 lg:max-w-none lg:px-8">
          {/* Left: Active Caregiver Avatar & Profile */}
          <div className="flex min-w-0 flex-1 items-center gap-2.5 sm:gap-3">
            <div
              className="relative flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-white font-semibold text-sm shadow-sm ring-2 ring-white dark:ring-slate-800"
              style={{ backgroundColor: currentCaregiver?.avatar_color || "#059669" }}
            >
              {currentCaregiver ? (
                currentCaregiver.name.slice(0, 1)
              ) : (
                <User className="h-5 w-5" />
              )}
              {/* Live SSE pulse indicator */}
              <span
                className={`absolute -bottom-0.5 -end-0.5 h-3 w-3 rounded-full border-2 border-white dark:border-slate-900 ${
                  connectionStatus === "connected"
                    ? "bg-emerald-500 animate-pulse"
                    : connectionStatus === "connecting"
                    ? "bg-amber-500 animate-ping"
                    : "bg-rose-500"
                }`}
                role="img"
                aria-label={
                  connectionStatus === "connected"
                    ? t.liveConnected
                    : connectionStatus === "connecting"
                    ? t.liveReconnecting
                    : t.liveOffline
                }
                title={
                  connectionStatus === "connected"
                    ? t.liveConnected
                    : connectionStatus === "connecting"
                    ? t.liveReconnecting
                    : t.liveOffline
                }
              />
            </div>

            <div className="flex min-w-0 flex-col">
              <span className="hidden truncate text-[11px] font-medium text-slate-500 dark:text-slate-400 sm:block">
                {t.activeCaregiver}
              </span>
              <div className="flex min-w-0 flex-col items-start gap-0.5 sm:flex-row sm:items-center sm:gap-1.5">
                <span className="max-w-full truncate text-sm font-bold text-slate-900 dark:text-white">
                  {currentCaregiver?.name || "..."}
                </span>

                {/* Role Badge */}
                <span
                  className={`inline-flex shrink-0 items-center gap-1 whitespace-nowrap px-1.5 py-0.5 rounded-md text-[10px] font-bold border ${roleMeta.badgeClass}`}
                >
                  {role === "super_admin" ? (
                    <ShieldAlert className="h-3 w-3" />
                  ) : role === "admin" ? (
                    <Pill className="h-3 w-3" />
                  ) : role === "caregiver" ? (
                    <Heart className="h-3 w-3" />
                  ) : (
                    <Eye className="h-3 w-3" />
                  )}
                  <span>{locale === "ar" ? roleMeta.labelAr : roleMeta.labelEn}</span>
                </span>
              </div>
            </div>
          </div>

          {/* One row: name, then (from xl, where they fit) every labelled action and
              Switch. Below xl the actions live in the labelled Menu sheet. */}
          <div className="flex shrink-0 items-center gap-1.5 sm:gap-2">
            <div className="hidden xl:flex items-center gap-1.5">
              {actions.map((a) => (
                <button
                  key={a.key}
                  type="button"
                  onClick={a.onClick}
                  title={a.label}
                  className={`flex min-h-12 items-center gap-1.5 rounded-xl px-2.5 text-sm font-semibold transition-colors active:scale-95 ${a.tone}`}
                >
                  <a.icon className="h-4 w-4 shrink-0" />
                  <span className="whitespace-nowrap">{a.short}</span>
                </button>
              ))}
            </div>

            {/* Switch Profile Button: always visible, it's the most used action */}
            <button
              type="button"
              onClick={() => openPinModal()}
              className="flex min-h-12 min-w-12 items-center justify-center gap-1.5 rounded-xl bg-slate-100 hover:bg-slate-200 px-2.5 sm:px-3 py-1.5 text-xs font-semibold text-slate-700 transition-colors active:scale-95 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
              aria-label={t.switchCaregiver}
            >
              <Users className="h-4 w-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
              <span className="sm:hidden">{ar ? "تبديل" : "Switch"}</span>
              <span className="hidden sm:inline">{t.switchCaregiver}</span>
            </button>

            <button
              type="button"
              onClick={() => setIsMenuOpen(true)}
              aria-label={locale === "ar" ? "القائمة" : "Menu"}
              aria-expanded={isMenuOpen}
              className="relative flex min-h-12 min-w-12 items-center justify-center gap-1.5 rounded-xl border border-slate-200 px-3 text-xs font-semibold text-slate-700 active:scale-95 dark:border-slate-700 dark:text-slate-200 xl:hidden"
            >
              <Menu className="h-5 w-5 shrink-0" />
              <span>{ar ? "القائمة" : "Menu"}</span>
              {needs2FA && <span className="absolute top-2 end-2 h-2 w-2 rounded-full bg-amber-500" />}
            </button>
          </div>
        </div>

        {/* Super Admin without an authenticator: one leaked PIN = full access */}
        {needs2FA && (
          <button
            type="button"
            onClick={() => setIsTwoFactorOpen(true)}
            className="flex min-h-12 w-full items-center justify-center gap-2 border-t border-amber-300 bg-amber-50 px-4 text-xs font-bold text-amber-900 dark:border-amber-900 dark:bg-amber-950/60 dark:text-amber-200"
          >
            <ShieldOff className="h-4 w-4 shrink-0" />
            <span>{locale === "ar" ? "حساب المشرف العام محمي برمز سري فقط — فعّل تطبيق المصادقة" : "Super Admin is protected by a PIN only — turn on an authenticator app"}</span>
          </button>
        )}

        {/* Read-Only Notice Banner for Viewer */}
        {isViewer(role) && (
          <div className="bg-slate-100 border-t border-slate-200 px-4 py-1.5 text-center text-xs font-medium text-slate-600 dark:bg-slate-800/80 dark:border-slate-700 dark:text-slate-300 flex items-center justify-center gap-2">
            <Eye className="h-3.5 w-3.5 text-slate-500" />
            <span>{t.readOnlyNotice}</span>
          </div>
        )}
      </header>

      {/* Modals */}
      <UserManagementModal
        isOpen={isUserModalOpen}
        onClose={() => setIsUserModalOpen(false)}
      />

      <ScheduleManagementModal
        isOpen={isScheduleModalOpen}
        onClose={() => setIsScheduleModalOpen(false)}
        onUpdated={onMedicationsUpdated}
      />

      <MedicationCatalogModal
        isOpen={isMedModalOpen}
        onClose={() => setIsMedModalOpen(false)}
        onUpdated={onMedicationsUpdated}
      />

      <ChangePinModal isOpen={isChangePinOpen} onClose={() => setIsChangePinOpen(false)} />
      <TwoFactorModal isOpen={isTwoFactorOpen} onClose={() => setIsTwoFactorOpen(false)} />

      {/* Mobile menu: bottom sheet within thumb reach */}
      {isMenuOpen && (
        <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-900/60 backdrop-blur-xs xl:hidden" onClick={() => setIsMenuOpen(false)}>
          <div
            role="dialog"
            aria-modal="true"
            aria-label={ar ? "القائمة" : "Menu"}
            className="max-h-[85dvh] w-full max-w-md overflow-y-auto rounded-t-3xl bg-white p-4 pb-[calc(1rem+env(safe-area-inset-bottom))] shadow-2xl dark:bg-slate-900"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="mb-2 flex items-center justify-between">
              <span className="ps-2 text-sm font-bold text-slate-500 dark:text-slate-400">{ar ? "القائمة" : "Menu"}</span>
              <button type="button" onClick={() => setIsMenuOpen(false)} aria-label={ar ? "إغلاق" : "Close"} className="flex min-h-12 min-w-12 items-center justify-center rounded-full text-slate-400">
                <X className="h-5 w-5" />
              </button>
            </div>
            <div className="space-y-2">
              {actions.map((a) => (
                <button
                  key={a.key}
                  type="button"
                  onClick={() => {
                    setIsMenuOpen(false);
                    a.onClick();
                  }}
                  className={`flex min-h-14 w-full items-center gap-3 rounded-2xl px-4 text-start text-sm font-bold active:scale-[0.98] ${a.tone}`}
                >
                  <a.icon className="h-5 w-5 shrink-0" />
                  <span className="flex-1">{a.label}</span>
                  {a.hint && <span className="text-xs font-semibold opacity-80">{a.hint}</span>}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}

      <ShiftManagementModal
        isOpen={isShiftModalOpen}
        onClose={() => setIsShiftModalOpen(false)}
        onUpdated={onMedicationsUpdated}
      />
    </>
  );
}
