"use client";

import React, { useState, useEffect, useCallback } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { Shift, ShiftAssignment, Caregiver, CurrentDutyStatus, ShiftPreset } from "@/lib/types";
import { isSuperAdmin } from "@/lib/permissions";
import { fetchApi } from "@/lib/api";
import {
  X,
  Clock,
  Calendar,
  Users,
  Check,
  Plus,
  Trash2,
  Sparkles,
  ShieldAlert,
  Loader2,
  AlertCircle,
  CheckCircle2,
  ArrowRight,
  Sun,
  Moon,
} from "lucide-react";

interface ShiftManagementModalProps {
  isOpen: boolean;
  onClose: () => void;
  onUpdated?: () => void;
}

const PRESET_OPTIONS: {
  id: ShiftPreset;
  titleKey: "preset24h" | "preset4h" | "preset8h" | "preset12h";
  descKey: "preset24hDesc" | "preset4hDesc" | "preset8hDesc" | "preset12hDesc";
  badge: { ar: string; en: string };
  color: string;
}[] = [
  {
    id: "24h",
    titleKey: "preset24h",
    descKey: "preset24hDesc",
    badge: { ar: "وردية واحدة • 24 ساعة", en: "1 shift • 24h" },
    color: "border-emerald-500 bg-emerald-50/50 dark:bg-emerald-950/20",
  },
  {
    id: "4h",
    titleKey: "preset4h",
    descKey: "preset4hDesc",
    badge: { ar: "6 ورديات • 4 ساعات لكل منها", en: "6 shifts • 4h each" },
    color: "border-blue-500 bg-blue-50/50 dark:bg-blue-950/20",
  },
  {
    id: "8h",
    titleKey: "preset8h",
    descKey: "preset8hDesc",
    badge: { ar: "3 ورديات • 8 ساعات لكل منها", en: "3 shifts • 8h each" },
    color: "border-purple-500 bg-purple-50/50 dark:bg-purple-950/20",
  },
  {
    id: "12h",
    titleKey: "preset12h",
    descKey: "preset12hDesc",
    badge: { ar: "ورديتان • 12 ساعة لكل منها", en: "2 shifts • 12h each" },
    color: "border-amber-500 bg-amber-50/50 dark:bg-amber-950/20",
  },
];

const PRESET_COLORS = [
  "#059669", // Emerald
  "#2563eb", // Blue
  "#7c3aed", // Purple
  "#d97706", // Amber
  "#dc2626", // Red
  "#0891b2", // Cyan
  "#db2777", // Pink
];

export function ShiftManagementModal({
  isOpen,
  onClose,
  onUpdated,
}: ShiftManagementModalProps) {
  const { currentCaregiver } = useCaregiver();
  const { locale, t } = useLanguage();

  const [activeTab, setActiveTab] = useState<"presets" | "slots" | "schedule">("presets");
  const [shifts, setShifts] = useState<Shift[]>([]);
  const [caregivers, setCaregivers] = useState<Caregiver[]>([]);
  const [assignments, setAssignments] = useState<ShiftAssignment[]>([]);
  const [dutyStatus, setDutyStatus] = useState<CurrentDutyStatus | null>(null);

  const [selectedDate, setSelectedDate] = useState<string>(() => {
    return new Date().toISOString().split("T")[0];
  });

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  // Custom shift create form
  const [newShiftNameAr, setNewShiftNameAr] = useState("");
  const [newShiftNameEn, setNewShiftNameEn] = useState("");
  const [newShiftStart, setNewShiftStart] = useState("08:00");
  const [newShiftEnd, setNewShiftEnd] = useState("12:00");
  const [newShiftColor, setNewShiftColor] = useState("#2563eb");
  const [showAddForm, setShowAddForm] = useState(false);

  const loadData = useCallback(async () => {
    if (!isOpen) return;
    setLoading(true);
    setError(null);
    try {
      const [shiftsData, cgData, dutyData] = await Promise.all([
        fetchApi<Shift[]>("/shifts"),
        fetchApi<Caregiver[]>("/caregivers"),
        fetchApi<CurrentDutyStatus>("/shifts/current-duty"),
      ]);
      setShifts(shiftsData || []);
      setCaregivers(cgData?.filter((c) => c.is_active && c.role !== "viewer") || []);
      setDutyStatus(dutyData || null);

      // Load assignments for selected date
      const assignData = await fetchApi<ShiftAssignment[]>(
        `/shifts/assignments?date=${selectedDate}`
      );
      setAssignments(assignData || []);
    } catch (err: any) {
      setError(err?.message || "Failed to load shift data");
    } finally {
      setLoading(false);
    }
  }, [isOpen, selectedDate]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  // Handle preset application
  const handleApplyPreset = async (preset: ShiftPreset) => {
    setSaving(true);
    setError(null);
    setSuccessMsg(null);
    try {
      const updatedShifts = await fetchApi<Shift[]>("/shifts/presets", {
        method: "POST",
        body: JSON.stringify({ preset }),
      });
      setShifts(updatedShifts || []);
      setSuccessMsg(
        locale === "ar"
          ? "تم تطبيق نمط الورديات بنجاح!"
          : "Shift preset applied successfully!"
      );
      await loadData();
      onUpdated?.();
    } catch (err: any) {
      setError(err?.message || "Failed to apply preset");
    } finally {
      setSaving(false);
    }
  };

  // Handle custom shift creation
  const handleCreateShift = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newShiftNameAr.trim() || !newShiftNameEn.trim()) {
      setError(locale === "ar" ? "يرجى ملء اسم الوردية" : "Please enter shift names");
      return;
    }

    setSaving(true);
    setError(null);
    setSuccessMsg(null);
    try {
      await fetchApi<Shift>("/shifts", {
        method: "POST",
        body: JSON.stringify({
          name_ar: newShiftNameAr.trim(),
          name_en: newShiftNameEn.trim(),
          start_time: newShiftStart,
          end_time: newShiftEnd,
          color: newShiftColor,
          display_order: shifts.length + 1,
        }),
      });

      setNewShiftNameAr("");
      setNewShiftNameEn("");
      setShowAddForm(false);
      setSuccessMsg(
        locale === "ar"
          ? "تمت إضافة الوردية المخصصة بنجاح!"
          : "Custom shift created successfully!"
      );
      await loadData();
      onUpdated?.();
    } catch (err: any) {
      setError(err?.message || "Failed to create shift");
    } finally {
      setSaving(false);
    }
  };

  // Handle shift deletion
  const handleDeleteShift = async (shiftId: string) => {
    if (shifts.length <= 1) {
      setError(
        locale === "ar"
          ? "يجب الإبقاء على وردية واحدة على الأقل"
          : "At least one shift must remain active"
      );
      return;
    }

    setSaving(true);
    setError(null);
    try {
      await fetchApi(`/shifts/${shiftId}`, { method: "DELETE" });
      await loadData();
      onUpdated?.();
    } catch (err: any) {
      setError(err?.message || "Failed to delete shift");
    } finally {
      setSaving(false);
    }
  };

  // Handle assigning caregiver to shift on selectedDate
  const handleAssignCaregiver = async (shiftId: string, caregiverId: string) => {
    setSaving(true);
    setError(null);
    try {
      await fetchApi("/shifts/assignments", {
        method: "POST",
        body: JSON.stringify({
          shift_id: shiftId,
          caregiver_id: caregiverId,
          date: selectedDate,
        }),
      });
      setSuccessMsg(t.assignSuccess);
      await loadData();
      onUpdated?.();
    } catch (err: any) {
      setError(err?.message || "Failed to assign caregiver");
    } finally {
      setSaving(false);
    }
  };

  if (!isOpen) return null;

  const isSuper = isSuperAdmin(currentCaregiver?.role);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-sm animate-in fade-in duration-200">
      <div
        className="w-full max-w-3xl max-h-[90vh] flex flex-col rounded-3xl bg-white shadow-2xl border border-slate-100 overflow-hidden dark:bg-slate-900 dark:border-slate-800"
        role="dialog"
        aria-modal="true"
        aria-labelledby="shift-modal-title"
      >
        {/* Header */}
        <div className="flex items-center justify-between p-5 border-b border-slate-100 dark:border-slate-800 bg-gradient-to-r from-purple-50/60 via-white to-blue-50/40 dark:from-purple-950/20 dark:via-slate-900 dark:to-blue-950/10">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-2xl bg-purple-600 text-white shadow-md shadow-purple-500/20">
              <Clock className="h-6 w-6" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2
                  id="shift-modal-title"
                  className="text-lg font-bold text-slate-900 dark:text-white"
                >
                  {t.manageShifts}
                </h2>
                <span className="inline-flex items-center gap-1 rounded-full bg-purple-100 px-2.5 py-0.5 text-[10px] font-bold text-purple-700 dark:bg-purple-900/50 dark:text-purple-300">
                  <ShieldAlert className="h-3 w-3" />
                  <span>{locale === "ar" ? "مشرف عام فقط" : "Super Admin"}</span>
                </span>
              </div>
              <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
                {t.manageShiftsSubtitle}
              </p>
            </div>
          </div>

          <button
            type="button"
            onClick={onClose}
            className="flex h-10 w-10 items-center justify-center rounded-full text-slate-400 hover:bg-slate-100 hover:text-slate-600 transition-colors dark:hover:bg-slate-800 dark:hover:text-slate-300"
            aria-label={locale === "ar" ? "إغلاق" : "Close"}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Access Guard */}
        {!isSuper && (
          <div className="p-6 text-center">
            <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-rose-50 text-rose-600 dark:bg-rose-950/40 dark:text-rose-400 mb-3">
              <ShieldAlert className="h-8 w-8" />
            </div>
            <h3 className="text-base font-bold text-slate-900 dark:text-white">
              {locale === "ar" ? "صلاحية غير كافية" : "Access Restricted"}
            </h3>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-1 max-w-sm mx-auto">
              {locale === "ar"
                ? "إدارة الورديات وتحديد فترات العمل مقتصرة فقط على المشرف العام."
                : "Shift configuration and duty scheduling are restricted to the Super Admin."}
            </p>
          </div>
        )}

        {isSuper && (
          <>
            {/* Tabs */}
            <div className="flex border-b border-slate-100 bg-slate-50/60 px-5 dark:border-slate-800 dark:bg-slate-800/40">
              <button
                type="button"
                onClick={() => setActiveTab("presets")}
                className={`flex items-center gap-2 py-3 px-4 text-xs font-bold border-b-2 transition-all ${
                  activeTab === "presets"
                    ? "border-purple-600 text-purple-700 dark:border-purple-400 dark:text-purple-300"
                    : "border-transparent text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200"
                }`}
              >
                <Sparkles className="h-4 w-4" />
                <span>{t.tabPresets}</span>
              </button>

              <button
                type="button"
                onClick={() => setActiveTab("slots")}
                className={`flex items-center gap-2 py-3 px-4 text-xs font-bold border-b-2 transition-all ${
                  activeTab === "slots"
                    ? "border-purple-600 text-purple-700 dark:border-purple-400 dark:text-purple-300"
                    : "border-transparent text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200"
                }`}
              >
                <Clock className="h-4 w-4" />
                <span>{t.tabCustomShifts} ({shifts.length})</span>
              </button>

              <button
                type="button"
                onClick={() => setActiveTab("schedule")}
                className={`flex items-center gap-2 py-3 px-4 text-xs font-bold border-b-2 transition-all ${
                  activeTab === "schedule"
                    ? "border-purple-600 text-purple-700 dark:border-purple-400 dark:text-purple-300"
                    : "border-transparent text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200"
                }`}
              >
                <Calendar className="h-4 w-4" />
                <span>{t.tabDutySchedule}</span>
              </button>
            </div>

            {/* Notifications */}
            {error && (
              <div className="mx-5 mt-4 flex items-center gap-2 rounded-xl bg-rose-50 p-3 text-xs font-semibold text-rose-700 border border-rose-200 dark:bg-rose-950/40 dark:border-rose-900/60 dark:text-rose-300">
                <AlertCircle className="h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
            )}
            {successMsg && (
              <div className="mx-5 mt-4 flex items-center gap-2 rounded-xl bg-emerald-50 p-3 text-xs font-semibold text-emerald-700 border border-emerald-200 dark:bg-emerald-950/40 dark:border-emerald-900/60 dark:text-emerald-300">
                <CheckCircle2 className="h-4 w-4 shrink-0" />
                <span>{successMsg}</span>
              </div>
            )}

            {/* Tab Contents */}
            <div className="flex-1 overflow-y-auto p-5">
              {loading ? (
                <div className="flex flex-col items-center justify-center py-16 gap-3">
                  <Loader2 className="h-8 w-8 animate-spin text-purple-600" />
                  <span className="text-xs font-semibold text-slate-500">
                    {locale === "ar" ? "جارٍ تحميل الورديات..." : "Loading shifts..."}
                  </span>
                </div>
              ) : activeTab === "presets" ? (
                /* TAB 1: Presets (24h, 4h, 8h, 12h shifts) */
                <div className="space-y-4">
                  <div className="rounded-2xl bg-amber-50/70 p-4 border border-amber-200/60 dark:bg-amber-950/20 dark:border-amber-900/40">
                    <h3 className="text-xs font-bold text-amber-900 dark:text-amber-200 flex items-center gap-2">
                      <Sparkles className="h-4 w-4 text-amber-600" />
                      <span>{t.shiftPresets}</span>
                    </h3>
                    <p className="text-[11px] text-amber-800/80 dark:text-amber-300/80 mt-1">
                      {locale === "ar"
                        ? "يمكنك الآن التبديل بضغطة زر واحدة بين وردية الـ 24 ساعة الحالية، أو تفعيل نمط ورديات الـ 4 ساعات (6 ورديات يومياً) عند الحاجة مستقبلاً."
                        : "Easily toggle with 1 click between the current 24-hour shift model or 4-hour shifts (6 daily windows) for future staffing."}
                    </p>
                  </div>

                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3.5">
                    {PRESET_OPTIONS.map((p) => {
                      const isCurrentPreset =
                        (p.id === "24h" && shifts.length === 1 && shifts[0]?.duration_hours === 24) ||
                        (p.id === "4h" && shifts.length === 6 && shifts[0]?.duration_hours === 4) ||
                        (p.id === "8h" && shifts.length === 3 && shifts[0]?.duration_hours === 8) ||
                        (p.id === "12h" && shifts.length === 2 && shifts[0]?.duration_hours === 12);

                      return (
                        <div
                          key={p.id}
                          className={`relative flex flex-col justify-between p-4 rounded-2xl border-2 transition-all ${
                            isCurrentPreset
                              ? "border-purple-600 bg-purple-50/40 dark:bg-purple-950/30 shadow-md ring-2 ring-purple-500/20"
                              : "border-slate-200 hover:border-purple-300 dark:border-slate-700 bg-white dark:bg-slate-800/60"
                          }`}
                        >
                          <div>
                            <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
                              <span className="text-sm font-extrabold text-slate-900 dark:text-white">
                                {t[p.titleKey]}
                              </span>
                              <bdi className="shrink-0 rounded-md bg-slate-100 dark:bg-slate-700 px-2 py-0.5 text-[11px] font-bold text-slate-600 dark:text-slate-200">
                                {p.badge[locale]}
                              </bdi>
                            </div>
                            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4 leading-relaxed">
                              {t[p.descKey]}
                            </p>
                          </div>

                          <div className="pt-2 border-t border-slate-100 dark:border-slate-800">
                            {isCurrentPreset ? (
                              <div className="flex items-center justify-center gap-1.5 py-2 text-xs font-bold text-emerald-700 dark:text-emerald-400">
                                <Check className="h-4 w-4" />
                                <span>{locale === "ar" ? "النمط المفعل حالياً" : "Current Active Model"}</span>
                              </div>
                            ) : (
                              <button
                                type="button"
                                disabled={saving}
                                onClick={() => handleApplyPreset(p.id)}
                                className="w-full flex min-h-12 items-center justify-center gap-1.5 rounded-xl bg-purple-600 hover:bg-purple-700 px-3 py-2 text-xs font-bold text-white transition-all active:scale-95 disabled:opacity-50"
                              >
                                {saving ? (
                                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                                ) : (
                                  <Sparkles className="h-3.5 w-3.5" />
                                )}
                                <span>{t.applyPreset}</span>
                              </button>
                            )}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              ) : activeTab === "slots" ? (
                /* TAB 2: Shift Slots List & Custom Add */
                <div className="space-y-4">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-bold text-slate-800 dark:text-slate-200">
                      {t.activeShiftsList} ({shifts.length})
                    </span>
                    <button
                      type="button"
                      onClick={() => setShowAddForm(!showAddForm)}
                      className="flex items-center gap-1 text-xs font-bold text-purple-700 dark:text-purple-300 hover:underline"
                    >
                      <Plus className="h-3.5 w-3.5" />
                      <span>{t.addCustomShift}</span>
                    </button>
                  </div>

                  {/* Add Custom Shift Form */}
                  {showAddForm && (
                    <form
                      onSubmit={handleCreateShift}
                      className="p-4 rounded-2xl border border-purple-200 bg-purple-50/50 dark:border-purple-900/60 dark:bg-purple-950/20 space-y-3 animate-in fade-in duration-200"
                    >
                      <h4 className="text-xs font-bold text-purple-900 dark:text-purple-200 flex items-center gap-1.5">
                        <Plus className="h-4 w-4" />
                        <span>{t.addCustomShift}</span>
                      </h4>

                      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <div>
                          <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                            {t.shiftNameAr} *
                          </label>
                          <input
                            type="text"
                            required
                            value={newShiftNameAr}
                            onChange={(e) => setNewShiftNameAr(e.target.value)}
                            placeholder="مثال: وردية المساء الأولى"
                            className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 focus:border-purple-500 focus:ring-1 focus:ring-purple-500 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                          />
                        </div>

                        <div>
                          <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                            {t.shiftNameEn} *
                          </label>
                          <input
                            type="text"
                            required
                            value={newShiftNameEn}
                            onChange={(e) => setNewShiftNameEn(e.target.value)}
                            placeholder="e.g. Evening Shift A"
                            className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 focus:border-purple-500 focus:ring-1 focus:ring-purple-500 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                          />
                        </div>
                      </div>

                      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                        <div>
                          <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                            {t.shiftStartTime}
                          </label>
                          <input
                            type="time"
                            required
                            value={newShiftStart}
                            onChange={(e) => setNewShiftStart(e.target.value)}
                            className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base font-mono text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                          />
                        </div>

                        <div>
                          <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                            {t.shiftEndTime}
                          </label>
                          <input
                            type="time"
                            required
                            value={newShiftEnd}
                            onChange={(e) => setNewShiftEnd(e.target.value)}
                            className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base font-mono text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                          />
                        </div>

                        <div>
                          <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                            {t.shiftColor}
                          </label>
                          <div className="flex items-center gap-1.5 pt-1">
                            {PRESET_COLORS.map((c) => (
                              <button
                                type="button"
                                key={c}
                                onClick={() => setNewShiftColor(c)}
                                style={{ backgroundColor: c }}
                                className={`h-7 w-7 rounded-full border-2 transition-transform ${
                                  newShiftColor === c
                                    ? "scale-110 border-white ring-2 ring-purple-600"
                                    : "border-transparent opacity-80 hover:opacity-100"
                                }`}
                              />
                            ))}
                          </div>
                        </div>
                      </div>

                      <div className="flex justify-end gap-2 pt-2">
                        <button
                          type="button"
                          onClick={() => setShowAddForm(false)}
                          className="min-h-10 px-3 py-1.5 text-xs font-semibold text-slate-600 hover:bg-slate-100 rounded-xl dark:text-slate-300 dark:hover:bg-slate-800"
                        >
                          {locale === "ar" ? "إلغاء" : "Cancel"}
                        </button>
                        <button
                          type="submit"
                          disabled={saving}
                          className="min-h-10 flex items-center gap-1 rounded-xl bg-purple-600 hover:bg-purple-700 px-4 py-1.5 text-xs font-bold text-white transition-all active:scale-95 disabled:opacity-50"
                        >
                          {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                          <span>{t.saveShift}</span>
                        </button>
                      </div>
                    </form>
                  )}

                  {/* Active Shifts Table / List */}
                  <div className="space-y-2">
                    {shifts.map((s, idx) => (
                      <div
                        key={s.id}
                        className="flex items-center justify-between p-3.5 rounded-2xl border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900/60 shadow-sm"
                      >
                        <div className="flex items-center gap-3">
                          <span
                            className="h-8 w-2 rounded-full"
                            style={{ backgroundColor: s.color || "#2563eb" }}
                          />
                          <div>
                            <div className="flex items-center gap-2">
                              <span className="text-xs font-bold text-slate-900 dark:text-white">
                                {locale === "ar" ? s.name_ar : s.name_en}
                              </span>
                              <span className="text-[10px] text-slate-400 font-normal">
                                ({locale === "ar" ? s.name_en : s.name_ar})
                              </span>
                            </div>
                            <div className="flex items-center gap-2 mt-0.5 text-[11px] text-slate-500 dark:text-slate-400">
                              <Clock className="h-3 w-3" />
                              <span className="font-mono">
                                {s.start_time} - {s.end_time}
                              </span>
                              <span>•</span>
                              <span className="font-semibold text-purple-700 dark:text-purple-300">
                                {s.duration_hours} {locale === "ar" ? "ساعات" : "hours"}
                              </span>
                            </div>
                          </div>
                        </div>

                        <div className="flex items-center gap-2">
                          <button
                            type="button"
                            onClick={() => handleDeleteShift(s.id)}
                            disabled={shifts.length <= 1 || saving}
                            title={locale === "ar" ? "حذف أو تعطيل الوردية" : "Delete Shift"}
                            className="flex h-9 w-9 items-center justify-center rounded-xl text-slate-400 hover:bg-rose-50 hover:text-rose-600 transition-colors disabled:opacity-30 dark:hover:bg-rose-950/30"
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
                /* TAB 3: Duty Scheduler */
                <div className="space-y-4">
                  {/* Date Picker & Current Duty Banner */}
                  <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4 rounded-2xl border border-slate-200 bg-slate-50/60 dark:border-slate-800 dark:bg-slate-800/40">
                    <div>
                      <span className="text-[11px] font-semibold text-slate-500 dark:text-slate-400 block mb-1">
                        {locale === "ar" ? "تاريخ المناوبة" : "Schedule Date"}
                      </span>
                      <div className="flex items-center gap-2">
                        <input
                          type="date"
                          value={selectedDate}
                          onChange={(e) => setSelectedDate(e.target.value)}
                          className="rounded-xl border border-slate-300 bg-white px-3 py-1.5 text-base font-semibold text-slate-900 shadow-sm focus:border-purple-500 focus:ring-1 focus:ring-purple-500 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                        />
                        <button
                          type="button"
                          onClick={() => setSelectedDate(new Date().toISOString().split("T")[0])}
                          className="rounded-xl border border-slate-200 bg-white hover:bg-slate-50 px-2.5 py-1.5 text-xs font-bold text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
                        >
                          {locale === "ar" ? "اليوم" : "Today"}
                        </button>
                      </div>
                    </div>

                    {/* Live On-Duty Badge */}
                    {dutyStatus && (
                      <div className="flex items-center gap-2.5 p-3 rounded-xl bg-white border border-slate-200 dark:bg-slate-900 dark:border-slate-700 shadow-sm">
                        <div className="h-2.5 w-2.5 rounded-full bg-emerald-500 animate-pulse" />
                        <div>
                          <span className="text-[10px] font-bold uppercase tracking-wider text-slate-400">
                            {t.onDutyNow}
                          </span>
                          <div className="flex items-center gap-1.5 text-xs font-bold text-slate-900 dark:text-white">
                            <span>
                              {dutyStatus.active_caregiver?.name || t.noCaregiverAssigned}
                            </span>
                            {dutyStatus.active_shift && (
                              <span className="text-[10px] font-normal text-purple-600 dark:text-purple-400">
                                ({locale === "ar"
                                  ? dutyStatus.active_shift.name_ar
                                  : dutyStatus.active_shift.name_en})
                              </span>
                            )}
                          </div>
                        </div>
                      </div>
                    )}
                  </div>

                  {/* Shift Assignments List for selected date */}
                  <div className="space-y-3">
                    {shifts.map((s) => {
                      const assignment = assignments.find((a) => a.shift_id === s.id);
                      const assignedCg = caregivers.find(
                        (c) => c.id === assignment?.caregiver_id
                      );

                      return (
                        <div
                          key={s.id}
                          className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4 rounded-2xl border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900 shadow-sm"
                        >
                          <div className="flex items-center gap-3">
                            <span
                              className="h-10 w-2.5 rounded-full"
                              style={{ backgroundColor: s.color || "#2563eb" }}
                            />
                            <div>
                              <div className="flex items-center gap-2">
                                <span className="text-xs font-bold text-slate-900 dark:text-white">
                                  {locale === "ar" ? s.name_ar : s.name_en}
                                </span>
                                <span className="rounded-md bg-purple-50 text-purple-700 dark:bg-purple-950 dark:text-purple-300 px-1.5 py-0.5 text-[10px] font-bold font-mono">
                                  {s.start_time} - {s.end_time}
                                </span>
                              </div>
                              <span className="text-[11px] text-slate-500 dark:text-slate-400">
                                {s.duration_hours} {locale === "ar" ? "ساعات عمل" : "hours duration"}
                              </span>
                            </div>
                          </div>

                          {/* Caregiver Selector */}
                          <div className="flex items-center gap-2">
                            <Users className="h-4 w-4 text-slate-400" />
                            <select
                              value={assignment?.caregiver_id || ""}
                              onChange={(e) =>
                                handleAssignCaregiver(s.id, e.target.value)
                              }
                              disabled={saving}
                              aria-label={t.assignCaregiver}
                              className="min-h-10 rounded-xl border border-slate-300 bg-white px-3 py-1.5 text-base font-semibold text-slate-900 focus:border-purple-500 focus:ring-1 focus:ring-purple-500 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                            >
                              <option value="">
                                -- {t.noCaregiverAssigned} --
                              </option>
                              {caregivers.map((cg) => (
                                <option key={cg.id} value={cg.id}>
                                  {cg.name}
                                </option>
                              ))}
                            </select>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>

            {/* Footer */}
            <div className="flex items-center justify-between p-4 border-t border-slate-100 bg-slate-50/60 dark:border-slate-800 dark:bg-slate-900">
              <span className="text-[11px] text-slate-500 dark:text-slate-400">
                {locale === "ar"
                  ? "تنعكس جميع التعديلات فورياً لدى كافة مقدمي الرعاية عبر المزامنة الحية."
                  : "All shift adjustments sync in real-time across all caregiver screens."}
              </span>

              <button
                type="button"
                onClick={onClose}
                className="flex min-h-10 items-center justify-center rounded-xl bg-slate-900 hover:bg-slate-800 px-4 py-2 text-xs font-bold text-white transition-all active:scale-95 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-slate-200"
              >
                {locale === "ar" ? "تم / إغلاق" : "Done / Close"}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
