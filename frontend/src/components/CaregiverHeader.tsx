"use client";

import React, { useState } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { ConnectionStatus } from "@/hooks/useLiveSync";
import { User, Users, Globe, ShieldAlert, Pill, Eye, Heart, ShieldCheck, Calendar, Clock, KeyRound } from "lucide-react";
import { ROLE_METAS, isSuperAdmin, canManageMedications, isViewer } from "@/lib/permissions";
import { UserManagementModal } from "./UserManagementModal";
import { MedicationCatalogModal } from "./MedicationCatalogModal";
import { ScheduleManagementModal } from "./ScheduleManagementModal";
import { ShiftManagementModal } from "./ShiftManagementModal";
import { ChangePinModal } from "./ChangePinModal";

interface CaregiverHeaderProps {
  connectionStatus?: ConnectionStatus;
  onMedicationsUpdated?: () => void;
}

export function CaregiverHeader({ connectionStatus = "connected", onMedicationsUpdated }: CaregiverHeaderProps) {
  const { currentCaregiver, openPinModal } = useCaregiver();
  const { locale, toggleLocale, t } = useLanguage();

  const [isUserModalOpen, setIsUserModalOpen] = useState(false);
  const [isMedModalOpen, setIsMedModalOpen] = useState(false);
  const [isScheduleModalOpen, setIsScheduleModalOpen] = useState(false);
  const [isShiftModalOpen, setIsShiftModalOpen] = useState(false);
  const [isChangePinOpen, setIsChangePinOpen] = useState(false);

  const role = currentCaregiver?.role || "caregiver";
  const roleMeta = ROLE_METAS[role] || ROLE_METAS.caregiver;

  return (
    <>
      <header className="sticky top-0 z-30 w-full border-b border-slate-200/80 bg-white/90 backdrop-blur-md dark:border-slate-800 dark:bg-slate-900/90 shadow-xs">
        <div className="mx-auto flex max-w-5xl items-center justify-between px-3 sm:px-6 py-2.5">
          {/* Left: Active Caregiver Avatar & Profile */}
          <div className="flex items-center gap-2.5 sm:gap-3">
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

            <div className="flex flex-col">
              <span className="text-[11px] font-medium text-slate-500 dark:text-slate-400">
                {t.activeCaregiver}
              </span>
              <div className="flex items-center gap-1.5 flex-wrap">
                <span className="text-sm font-bold text-slate-900 dark:text-white">
                  {currentCaregiver?.name || "..."}
                </span>

                {/* Role Badge */}
                <span
                  className={`inline-flex items-center gap-1 px-1.5 py-0.2 rounded-md text-[10px] font-bold border ${roleMeta.badgeClass}`}
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

          {/* Right: Management Buttons & Personas Switcher */}
          <div className="flex items-center gap-1.5 sm:gap-2">
            {/* Super Admin: User Management */}
            {isSuperAdmin(role) && (
              <button
                type="button"
                onClick={() => setIsUserModalOpen(true)}
                aria-label={t.userManagement}
                className="flex min-h-12 min-w-12 items-center justify-center gap-1 rounded-xl bg-purple-50 hover:bg-purple-100 px-2.5 py-1.5 text-xs font-bold text-purple-700 transition-colors active:scale-95 dark:bg-purple-950/60 dark:text-purple-300 dark:hover:bg-purple-900/60"
                title={locale === "ar" ? "إدارة المستخدمين والأدوار" : "Manage Users & Roles"}
              >
                <ShieldAlert className="h-4 w-4" />
                <span className="hidden md:inline">{t.userManagement}</span>
              </button>
            )}

            {/* Super Admin: Shift Management (24h / 4h shifts) */}
            {isSuperAdmin(role) && (
              <button
                type="button"
                onClick={() => setIsShiftModalOpen(true)}
                aria-label={t.manageShifts}
                className="flex min-h-12 min-w-12 items-center justify-center gap-1 rounded-xl bg-blue-50 hover:bg-blue-100 px-2.5 py-1.5 text-xs font-bold text-blue-700 transition-colors active:scale-95 dark:bg-blue-950/60 dark:text-blue-300 dark:hover:bg-blue-900/60"
                title={locale === "ar" ? "إدارة وتحديد فترات الورديات والمناوبين" : "Manage Work Shifts & Duty Schedule"}
              >
                <Clock className="h-4 w-4" />
                <span className="hidden md:inline">{t.manageShifts}</span>
              </button>
            )}

            {/* Admin or Super Admin: Schedule Management */}
            {canManageMedications(role) && (
              <button
                type="button"
                onClick={() => setIsScheduleModalOpen(true)}
                aria-label={t.manageSchedule}
                className="flex min-h-12 min-w-12 items-center justify-center gap-1 rounded-xl bg-emerald-50 hover:bg-emerald-100 px-2.5 py-1.5 text-xs font-bold text-emerald-800 transition-colors active:scale-95 dark:bg-emerald-950/60 dark:text-emerald-300 dark:hover:bg-emerald-900/60"
                title={locale === "ar" ? "إدارة مواعيد وأيام الأدوية" : "Manage Times & Days Schedule"}
              >
                <Calendar className="h-4 w-4" />
                <span className="hidden md:inline">{t.manageSchedule}</span>
              </button>
            )}

            {/* Admin or Super Admin: Medications Catalog */}
            {canManageMedications(role) && (
              <button
                type="button"
                onClick={() => setIsMedModalOpen(true)}
                aria-label={t.manageMedications}
                className="flex min-h-12 min-w-12 items-center justify-center gap-1 rounded-xl bg-amber-50 hover:bg-amber-100 px-2.5 py-1.5 text-xs font-bold text-amber-800 transition-colors active:scale-95 dark:bg-amber-950/60 dark:text-amber-300 dark:hover:bg-amber-900/60"
                title={locale === "ar" ? "إدارة الأدوية والجرعات" : "Prescriptions Catalog"}
              >
                <Pill className="h-4 w-4" />
                <span className="hidden md:inline">{t.manageMedications}</span>
              </button>
            )}

            {/* Change own PIN */}
            {currentCaregiver && (
              <button
                type="button"
                onClick={() => setIsChangePinOpen(true)}
                aria-label={locale === "ar" ? "تغيير رمز PIN" : "Change PIN"}
                title={locale === "ar" ? "تغيير رمز PIN" : "Change PIN"}
                className="flex min-h-12 min-w-12 items-center justify-center gap-1 rounded-xl bg-slate-100 hover:bg-slate-200 px-2.5 py-1.5 text-xs font-semibold text-slate-700 transition-colors active:scale-95 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
              >
                <KeyRound className="h-4 w-4" />
                <span className="hidden md:inline">{locale === "ar" ? "تغيير الرمز" : "Change PIN"}</span>
              </button>
            )}

            {/* Switch Profile Button */}
            <button
                type="button"
              onClick={() => openPinModal()}
              className="flex min-h-12 min-w-12 items-center justify-center gap-1.5 rounded-xl bg-slate-100 hover:bg-slate-200 px-2.5 sm:px-3 py-1.5 text-xs font-semibold text-slate-700 transition-colors active:scale-95 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
              aria-label={t.switchCaregiver}
            >
              <Users className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
              <span className="hidden sm:inline">{t.switchCaregiver}</span>
            </button>

            {/* Language Switcher */}
            <button
                type="button"
              onClick={toggleLocale}
              aria-label={locale === "ar" ? "Switch to English" : "التبديل إلى العربية"}
              className="flex min-h-12 min-w-12 items-center justify-center gap-1 rounded-xl border border-slate-200 px-2.5 py-1.5 text-xs font-semibold text-slate-600 hover:bg-slate-50 transition-colors active:scale-95 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
              title="Switch Language"
            >
              <Globe className="h-3.5 w-3.5" />
              <span>{locale === "ar" ? "EN" : "عربي"}</span>
            </button>
          </div>
        </div>

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

      <ShiftManagementModal
        isOpen={isShiftModalOpen}
        onClose={() => setIsShiftModalOpen(false)}
        onUpdated={onMedicationsUpdated}
      />
    </>
  );
}
