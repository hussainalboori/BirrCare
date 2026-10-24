"use client";

import React, { useState, useEffect } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { Medication } from "@/lib/types";
import { canManageMedications } from "@/lib/permissions";
import { fetchApi } from "@/lib/api";
import { autoIntervalHours } from "@/lib/clinical";
import { uploadMedicinePhoto } from "@/lib/photo";
import {
  Camera,
  X,
  Pill,
  Plus,
  Edit2,
  Trash2,
  Check,
  Clock,
  Loader2,
  AlertCircle,
  FileText,
} from "lucide-react";

interface MedicationCatalogModalProps {
  isOpen: boolean;
  onClose: () => void;
  onUpdated?: () => void;
}

const ALL_SLOTS = [
  { id: "morning", labelAr: "الصباح", labelEn: "Morning", defaultTime: "08:00" },
  { id: "afternoon", labelAr: "الظهيرة", labelEn: "Afternoon", defaultTime: "14:00" },
  { id: "evening", labelAr: "المساء", labelEn: "Evening", defaultTime: "20:00" },
  { id: "bedtime", labelAr: "قبل النوم", labelEn: "Bedtime", defaultTime: "22:30" },
];

const DAYS_LIST = [
  { id: "sun", labelAr: "الأحد", labelEn: "Sun" },
  { id: "mon", labelAr: "الإثنين", labelEn: "Mon" },
  { id: "tue", labelAr: "الثلاثاء", labelEn: "Tue" },
  { id: "wed", labelAr: "الأربعاء", labelEn: "Wed" },
  { id: "thu", labelAr: "الخميس", labelEn: "Thu" },
  { id: "fri", labelAr: "الجمعة", labelEn: "Fri" },
  { id: "sat", labelAr: "السبت", labelEn: "Sat" },
];

const ALL_DAY_IDS = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];

// The box front and back, the leaflet, the pill… (the server allows 6).
const MAX_PHOTOS = 6;

const PILL_PRESETS = [
  { id: "aspirin", url: "/meds/aspirin.svg", labelAr: "أسبرين (أصفر)", labelEn: "Aspirin (Yellow)" },
  { id: "clopidogrel", url: "/meds/clopidogrel.svg", labelAr: "بلافيكس (وردي)", labelEn: "Plavix (Pink)" },
  { id: "amlodipine", url: "/meds/amlodipine.svg", labelAr: "أملوديبين (ثماني)", labelEn: "Amlodipine (Octagon)" },
  { id: "atorvastatin", url: "/meds/atorvastatin.svg", labelAr: "ليبيتور (بيضاوي)", labelEn: "Lipitor (Oval)" },
  { id: "citicoline", url: "/meds/citicoline.svg", labelAr: "سيتيكولين (كبسولة)", labelEn: "Citicoline (Caplet)" },
  { id: "generic", url: "/meds/pill-default.svg", labelAr: "كبسولة عامة", labelEn: "Generic Pill" },
];

export function MedicationCatalogModal({ isOpen, onClose, onUpdated }: MedicationCatalogModalProps) {
  const { currentCaregiver } = useCaregiver();
  const { locale } = useLanguage();

  const [meds, setMeds] = useState<Medication[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Form state
  const [showForm, setShowForm] = useState(false);
  const [editingMed, setEditingMed] = useState<Medication | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Form values
  const [nameAr, setNameAr] = useState("");
  const [nameEn, setNameEn] = useState("");
  const [dosage, setDosage] = useState("");
  const [instAr, setInstAr] = useState("");
  const [instEn, setInstEn] = useState("");
  const [imageUrl, setImageUrl] = useState("/meds/pill-default.svg");
  // Every uploaded photo (box front, back, the pill); imageUrl is the main one.
  const [photos, setPhotos] = useState<string[]>([]);
  const [photoBusy, setPhotoBusy] = useState(false);
  const [photoError, setPhotoError] = useState("");
  const [selectedSlots, setSelectedSlots] = useState<string[]>(["morning"]);
  const [daysOfWeek, setDaysOfWeek] = useState<string[]>(ALL_DAY_IDS);
  // Doctor's minimum hours between doses; "" = automatic.
  const [minHours, setMinHours] = useState("");
  // Anticoagulant / antiplatelet: listed first in the doctor report.
  const [isCritical, setIsCritical] = useState(false);
  // Stock (issue #20): count the pills in the box and warn before it runs out.
  const [trackStock, setTrackStock] = useState(false);
  const [stockCount, setStockCount] = useState("");
  const [stockTouched, setStockTouched] = useState(false);
  const [unitsPerDose, setUnitsPerDose] = useState("1");
  const [thresholdDays, setThresholdDays] = useState("4");
  // PRN (issue #25)
  const [isPRN, setIsPRN] = useState(false);
  const [prnMax24h, setPrnMax24h] = useState("4");
  const [prnMinHours, setPrnMinHours] = useState("4");
  const [slotTimes, setSlotTimes] = useState<Record<string, string>>({
    morning: "08:00",
    afternoon: "14:00",
    evening: "20:00",
    bedtime: "22:30",
  });

  const loadMeds = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchApi<Medication[]>("/medications");
      setMeds(data || []);
    } catch (err: any) {
      setError(err.message || "Failed to load medications");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (isOpen && canManageMedications(currentCaregiver?.role)) {
      loadMeds();
    }
  }, [isOpen, currentCaregiver]);

  if (!isOpen) return null;

  const handleOpenAdd = () => {
    setEditingMed(null);
    setNameAr("");
    setNameEn("");
    setDosage("");
    setInstAr("");
    setInstEn("");
    setImageUrl("/meds/pill-default.svg");
    setPhotos([]);
    setSelectedSlots(["morning"]);
    setDaysOfWeek(ALL_DAY_IDS);
    setMinHours("");
    setIsCritical(false);
    setTrackStock(false);
    setStockCount("");
    setStockTouched(false);
    setUnitsPerDose("1");
    setThresholdDays("4");
    setIsPRN(false);
    setPrnMax24h("4");
    setPrnMinHours("4");
    setSlotTimes({
      morning: "08:00",
      afternoon: "14:00",
      evening: "20:00",
      bedtime: "22:30",
    });
    setShowForm(true);
  };

  const handleOpenEdit = (med: Medication) => {
    setEditingMed(med);
    setNameAr(med.name_ar);
    setNameEn(med.name_en);
    setDosage(med.dosage);
    setInstAr(med.instructions_ar || "");
    setInstEn(med.instructions_en || "");
    setImageUrl(med.image_url || "/meds/pill-default.svg");
    setPhotos(med.photo_urls || []);
    setMinHours(med.min_hours_between == null ? "" : String(med.min_hours_between));
    setIsCritical(!!med.is_critical);
    setTrackStock(med.stock_units != null);
    setStockCount(med.stock_units != null ? String(med.stock_units) : "");
    setStockTouched(false);
    setUnitsPerDose(String(med.units_per_dose || 1));
    setThresholdDays(String(med.refill_threshold_days || 4));
    setIsPRN(!!med.is_prn);
    setPrnMax24h(med.prn_max_per_24h != null ? String(med.prn_max_per_24h) : "4");
    setPrnMinHours(med.prn_min_hours != null ? String(med.prn_min_hours) : "4");
    setSelectedSlots(med.frequency_slots || ["morning"]);
    setDaysOfWeek(med.days_of_week && med.days_of_week.length > 0 ? med.days_of_week : ALL_DAY_IDS);
    const times: Record<string, string> = {};
    ALL_SLOTS.forEach((s) => {
      times[s.id] = med.slot_times?.[s.id] || s.defaultTime;
    });
    setSlotTimes(times);
    setShowForm(true);
  };

  const handleToggleDay = (dayId: string) => {
    if (daysOfWeek.includes(dayId)) {
      if (daysOfWeek.length > 1) {
        setDaysOfWeek(daysOfWeek.filter((d) => d !== dayId));
      }
    } else {
      setDaysOfWeek([...daysOfWeek, dayId]);
    }
  };

  const handleToggleSlot = (slotId: string) => {
    if (selectedSlots.includes(slotId)) {
      if (selectedSlots.length > 1) {
        setSelectedSlots(selectedSlots.filter((s) => s !== slotId));
      }
    } else {
      setSelectedSlots([...selectedSlots, slotId]);
    }
  };

  // Only send the count when it was typed (or tracking is new): otherwise saving
  // an edit would undo doses given while this form was open.
  const stockPayload = () => {
    if (!trackStock) return editingMed?.stock_units != null ? { track_stock: false } : {};
    const p: Record<string, number> = {
      units_per_dose: Number(unitsPerDose) > 0 ? Number(unitsPerDose) : 1,
      refill_threshold_days: Number(thresholdDays) >= 1 ? Math.round(Number(thresholdDays)) : 4,
    };
    if (stockTouched || editingMed?.stock_units == null) p.stock_units = Number(stockCount) >= 0 ? Number(stockCount) : 0;
    return p;
  };

  const handleSaveMedication = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!nameAr.trim() || !dosage.trim() || (!isPRN && selectedSlots.length === 0)) return;

    setIsSubmitting(true);
    try {
      const prnPayload = isPRN
        ? {
            is_prn: true,
            prn_max_per_24h: prnMax24h.trim() !== "" ? Number(prnMax24h) : 4,
            prn_min_hours: prnMinHours.trim() !== "" ? Number(prnMinHours) : 4,
            frequency_slots: [],
          }
        : {
            is_prn: false,
            frequency_slots: selectedSlots,
            days_of_week: daysOfWeek,
            slot_times: slotTimes,
            min_hours_between: minHours.trim() === "" ? null : Number(minHours),
          };

      if (editingMed) {
        // Edit medication
        await fetchApi(`/medications/${editingMed.id}`, {
          method: "PUT",
          body: JSON.stringify({
            name_ar: nameAr.trim(),
            name_en: nameEn.trim() || nameAr.trim(),
            dosage: dosage.trim(),
            instructions_ar: instAr.trim() || undefined,
            instructions_en: instEn.trim() || undefined,
            image_url: imageUrl.trim() || undefined,
            photo_urls: photos,
            is_critical: isCritical,
            ...prnPayload,
            ...stockPayload(),
          }),
        });
      } else {
        // Add new medication
        await fetchApi("/medications", {
          method: "POST",
          body: JSON.stringify({
            name_ar: nameAr.trim(),
            name_en: nameEn.trim() || nameAr.trim(),
            dosage: dosage.trim(),
            instructions_ar: instAr.trim() || undefined,
            instructions_en: instEn.trim() || undefined,
            image_url: imageUrl.trim() || undefined,
            photo_urls: photos,
            is_critical: isCritical,
            ...prnPayload,
            ...stockPayload(),
          }),
        });
      }

      setShowForm(false);
      setEditingMed(null);
      await loadMeds();
      onUpdated?.();
    } catch (err: any) {
      alert(err.message || "Failed to save prescription");
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleDeleteMedication = async (med: Medication) => {
    const medName = locale === "ar" ? med.name_ar : med.name_en;
    if (!confirm(locale === "ar" ? `هل تريد تعطيل دواء ${medName}؟` : `Deactivate ${medName}?`)) return;

    // Destructive Admin Settings Guard:
    // Prompt for admin PIN to protect against accidental medication deletion on shared/unlocked bedside tablets.
    const pin = window.prompt(
      locale === "ar"
        ? `لحماية سلامة المريض، يرجى إدخال الرمز السري للمشرف لتعطيل ${medName}:`
        : `For patient safety, please enter your Admin PIN to deactivate ${medName}:`
    );
    if (!pin) return;

    try {
      await fetchApi(`/medications/${med.id}`, {
        method: "DELETE",
        headers: { "X-Admin-PIN": pin },
      });
      await loadMeds();
      onUpdated?.();
    } catch (err: any) {
      alert(err.message || (locale === "ar" ? "فشل التعطيل: الرمز السري غير صحيح" : "Failed to deactivate: Invalid PIN"));
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 backdrop-blur-xs p-4 animate-in fade-in duration-200">
      <div role="dialog" aria-modal="true" className="relative w-full max-w-2xl max-h-[90dvh] flex flex-col rounded-3xl bg-white shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800 overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between p-5 border-b border-slate-100 dark:border-slate-800">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-2xl bg-amber-100 text-amber-700 dark:bg-amber-950/60 dark:text-amber-300">
              <Pill className="h-5 w-5" />
            </div>
            <div>
              <h3 className="text-base font-bold text-slate-900 dark:text-white">
                {locale === "ar" ? "إدارة الأدوية والجرعات" : "Medications & Dosages Catalog"}
              </h3>
              <p className="text-xs text-slate-500 dark:text-slate-400">
                {locale === "ar"
                  ? "تعديل الوصفات الطبية، الجرعات، ومواعيد الوجبات (Admin / Super Admin)"
                  : "Add or edit stroke recovery medications, dosages, and daily schedules"}
              </p>
            </div>
          </div>

          <button
            type="button"
            onClick={onClose}
            aria-label={locale === "ar" ? "إغلاق" : "Close"}
            className="flex min-h-12 min-w-12 shrink-0 items-center justify-center rounded-full p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Content Body */}
        <div className="flex-1 overflow-y-auto p-5">
          {error && (
            <div className="mb-4 rounded-2xl bg-rose-50 p-3 text-xs font-semibold text-rose-700 dark:bg-rose-950/40 dark:text-rose-300 border border-rose-200 dark:border-rose-900 flex items-center gap-2">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {/* Action Bar */}
          {!showForm && (
            <div className="mb-4 flex items-center justify-between">
              <span className="text-xs font-bold text-slate-500 dark:text-slate-400">
                {locale === "ar" ? `الأدوية النشطة (${meds.length})` : `Active Prescriptions (${meds.length})`}
              </span>
              <button
                onClick={handleOpenAdd}
                className="flex items-center gap-1.5 rounded-xl bg-amber-600 hover:bg-amber-700 px-3.5 py-2 text-xs font-bold text-white shadow-amber-500/20 shadow-sm transition-all active:scale-95"
              >
                <Plus className="h-4 w-4" />
                <span>{locale === "ar" ? "إضافة دواء جديد" : "Add Medication"}</span>
              </button>
            </div>
          )}

          {/* ADD / EDIT FORM */}
          {showForm && (
            <form
              onSubmit={handleSaveMedication}
              className="mb-6 rounded-2xl border border-amber-200 bg-amber-50/40 p-4 dark:border-amber-900/60 dark:bg-amber-950/20 animate-in fade-in duration-150"
            >
              <div className="flex items-center justify-between mb-3">
                <h4 className="text-xs font-bold text-amber-900 dark:text-amber-200 flex items-center gap-1.5">
                  <Pill className="h-4 w-4 text-amber-600" />
                  <span>
                    {editingMed
                      ? locale === "ar"
                        ? `تعديل الدواء: ${editingMed.name_ar}`
                        : `Edit Medication: ${editingMed.name_en}`
                      : locale === "ar"
                      ? "إضافة وصفة طبية جديدة"
                      : "Add New Prescription"}
                  </span>
                </h4>
                <button
                  type="button"
                  onClick={() => {
                    setShowForm(false);
                    setEditingMed(null);
                  }}
                  className="text-xs font-semibold text-slate-500 hover:text-slate-700"
                >
                  {locale === "ar" ? "إلغاء" : "Cancel"}
                </button>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-3">
                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "الاسم بالعربية *" : "Arabic Name *"}
                  </label>
                  <input
                    type="text"
                    required
                    placeholder="مثال: كلوبيدوغريل (بلافيكس)"
                    value={nameAr}
                    onChange={(e) => setNameAr(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-base font-bold text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>

                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "الاسم بالإنجليزية" : "English Name"}
                  </label>
                  <input
                    type="text"
                    placeholder="e.g. Clopidogrel (Plavix)"
                    value={nameEn}
                    onChange={(e) => setNameEn(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-base font-bold text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>

                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "الجرعة المقررة *" : "Prescribed Dosage *"}
                  </label>
                  <input
                    type="text"
                    required
                    placeholder="مثال: 75mg قرص واحد"
                    value={dosage}
                    onChange={(e) => setDosage(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-base font-bold text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
              </div>

              {/* PRN (AS-NEEDED) TOGGLE */}
              <div className="mb-4 rounded-xl border border-blue-200 bg-blue-50/70 p-3 dark:border-blue-900 dark:bg-blue-950/30">
                <label className="flex min-h-12 items-center gap-3 text-sm font-semibold text-blue-950 dark:text-blue-200 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={isPRN}
                    onChange={(e) => setIsPRN(e.target.checked)}
                    className="h-5 w-5 rounded border-slate-300 text-blue-600 focus:ring-blue-500"
                  />
                  <span>
                    {locale === "ar"
                      ? "دواء عند الحاجة (PRN) — مثل خافض الحرارة ومسكّن الصداع"
                      : "As-needed medicine (PRN) — e.g. fever reducers, headache pain relief"}
                  </span>
                </label>

                {isPRN && (
                  <div className="mt-3 grid grid-cols-1 sm:grid-cols-2 gap-3 pt-2 border-t border-blue-200/60 dark:border-blue-900/60">
                    <div>
                      <label className="block text-xs font-bold text-slate-700 dark:text-slate-200 mb-1">
                        {locale === "ar" ? "الحد الأقصى للجرعات خلال 24 ساعة *" : "Max doses per 24 hours *"}
                      </label>
                      <input
                        type="number"
                        min="1"
                        max="24"
                        value={prnMax24h}
                        onChange={(e) => setPrnMax24h(e.target.value)}
                        className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base font-bold text-slate-900 focus:border-blue-500 focus:outline-none dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                        placeholder="4"
                        required
                      />
                      <span className="text-xs text-slate-500 dark:text-slate-400">
                        {locale === "ar" ? "مثال: 4 جرعات في 24 ساعة" : "Example: 4 doses in 24h"}
                      </span>
                    </div>

                    <div>
                      <label className="block text-xs font-bold text-slate-700 dark:text-slate-200 mb-1">
                        {locale === "ar" ? "أقل فاصل زمني بين الجرعات (ساعات) *" : "Min interval between doses (hours) *"}
                      </label>
                      <input
                        type="number"
                        min="0"
                        max="72"
                        step="0.5"
                        value={prnMinHours}
                        onChange={(e) => setPrnMinHours(e.target.value)}
                        className="w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base font-bold text-slate-900 focus:border-blue-500 focus:outline-none dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                        placeholder="4"
                        required
                      />
                      <span className="text-xs text-slate-500 dark:text-slate-400">
                        {locale === "ar" ? "مثال: لا يُعطى قبل مرور 4 ساعات" : "Example: cannot be repeated before 4h"}
                      </span>
                    </div>
                  </div>
                )}
              </div>

              {!isPRN && (
                <>
                  {/* FREQUENCY SLOTS & EXACT TIMES */}
                  <div className="mb-4">
                    <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1.5">
                      {locale === "ar" ? "فترات اليوم المقررة وتوقيت كل جرعة *" : "Scheduled Daily Time Slots & Clock Times *"}
                    </label>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                      {ALL_SLOTS.map((slot) => {
                        const isChecked = selectedSlots.includes(slot.id);
                        const currentSlotTime = slotTimes[slot.id] || slot.defaultTime;
                        return (
                          <div
                            key={slot.id}
                            className={`flex items-center justify-between p-2.5 rounded-xl border text-xs transition-all ${
                              isChecked
                                ? "border-amber-500/80 bg-amber-50/80 text-amber-950 dark:bg-amber-950/40 dark:text-amber-200"
                                : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 dark:border-slate-800 dark:bg-slate-800 dark:text-slate-400"
                            }`}
                          >
                            <button
                              type="button"
                              onClick={() => handleToggleSlot(slot.id)}
                              className="flex items-center gap-2 flex-1 text-start"
                            >
                              <div
                                className={`flex h-4 w-4 items-center justify-center rounded-md border ${
                                  isChecked
                                    ? "bg-amber-600 border-amber-600 text-white"
                                    : "border-slate-300 dark:border-slate-600"
                                }`}
                              >
                                {isChecked && <Check className="h-3 w-3 stroke-[3]" />}
                              </div>
                              <Clock className="h-3.5 w-3.5 text-amber-600 dark:text-amber-400" />
                              <span className="font-bold">{locale === "ar" ? slot.labelAr : slot.labelEn}</span>
                            </button>

                            {/* Clock Time input */}
                            {isChecked && (
                              <div className="flex items-center gap-1 bg-white dark:bg-slate-900 px-2 py-1 rounded-lg border border-amber-200 dark:border-amber-800">
                                <span className="text-xs text-slate-400 font-mono">⏰</span>
                                <input
                                  type="time"
                                  value={currentSlotTime}
                                  onChange={(e) =>
                                    setSlotTimes((prev) => ({
                                      ...prev,
                                      [slot.id]: e.target.value,
                                    }))
                                  }
                                  className="bg-transparent text-base font-mono font-bold text-slate-900 focus:outline-none dark:text-white"
                                />
                              </div>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  </div>

                  {/* MINIMUM INTERVAL (cross-midnight double-dose lock) */}
                  <div className="mb-4">
                    <label htmlFor="min-hours" className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1">
                      {locale === "ar" ? "أقل عدد ساعات بين جرعتين (حسب الطبيب)" : "Minimum hours between two doses (doctor's value)"}
                    </label>
                    <input
                      id="min-hours"
                      type="number"
                      inputMode="decimal"
                      min={0}
                      max={72}
                      step={0.5}
                      value={minHours}
                      onChange={(e) => setMinHours(e.target.value)}
                      placeholder={String(autoIntervalHours(selectedSlots, slotTimes))}
                      className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-base text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                    />
                    <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                      {locale === "ar"
                        ? `اتركه فارغاً للحساب التلقائي (${autoIntervalHours(selectedSlots, slotTimes)} ساعة لهذا الجدول). 0 يلغي القفل.`
                        : `Leave empty for automatic (${autoIntervalHours(selectedSlots, slotTimes)} h for this schedule). 0 turns the lock off.`}
                    </p>
                  </div>
                </>
              )}

              {/* CRITICAL MEDICATION (doctor report) */}
              <label className="mb-4 flex min-h-12 items-center gap-3 rounded-xl border border-rose-200 bg-rose-50 px-3 text-sm font-semibold text-rose-900 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-200">
                <input type="checkbox" checked={isCritical} onChange={(e) => setIsCritical(e.target.checked)} className="h-5 w-5" />
                {locale === "ar" ? "دواء حرج (مميّع للدم أو مضاد للصفائح): يظهر أولاً في تقرير الطبيب ومعلَّماً في بطاقة الطوارئ" : "Critical medicine (blood thinner / antiplatelet): listed first in the doctor report and flagged on the emergency card"}
              </label>

              {/* STOCK (issue #20) */}
              <div className="mb-4 rounded-xl border border-amber-200 bg-amber-50 p-3 dark:border-amber-900 dark:bg-amber-950/30">
                <label className="flex min-h-12 items-center gap-3 text-sm font-semibold text-amber-900 dark:text-amber-200">
                  <input type="checkbox" checked={trackStock} onChange={(e) => setTrackStock(e.target.checked)} className="h-5 w-5" />
                  {locale === "ar" ? "عُدّ الحبوب في العلبة ونبّهنا قبل أن تنفد" : "Count the pills in the box and warn us before it runs out"}
                </label>
                {trackStock && (
                  <div className="mt-2 grid grid-cols-1 gap-3 sm:grid-cols-3">
                    <label className="block">
                      <span className="mb-1 block text-xs font-bold text-slate-700 dark:text-slate-200">{locale === "ar" ? "عدد الحبوب في العلبة الآن" : "Pills in the box now"}</span>
                      <input
                        type="number"
                        inputMode="decimal"
                        min={0}
                        step="0.5"
                        value={stockCount}
                        onChange={(e) => {
                          setStockCount(e.target.value);
                          setStockTouched(true);
                        }}
                        className="min-h-12 w-full rounded-xl border border-slate-200 bg-white px-3 text-base dark:border-slate-700 dark:bg-slate-800"
                      />
                    </label>
                    <label className="block">
                      <span className="mb-1 block text-xs font-bold text-slate-700 dark:text-slate-200">{locale === "ar" ? "حبوب في كل جرعة" : "Pills per dose"}</span>
                      <input type="number" inputMode="decimal" min={0.25} step="0.25" value={unitsPerDose} onChange={(e) => setUnitsPerDose(e.target.value)} className="min-h-12 w-full rounded-xl border border-slate-200 bg-white px-3 text-base dark:border-slate-700 dark:bg-slate-800" />
                    </label>
                    <label className="block">
                      <span className="mb-1 block text-xs font-bold text-slate-700 dark:text-slate-200">{locale === "ar" ? "نبّهنا قبل النفاد بـ (أيام)" : "Warn this many days before"}</span>
                      <input type="number" inputMode="numeric" min={1} max={60} value={thresholdDays} onChange={(e) => setThresholdDays(e.target.value)} className="min-h-12 w-full rounded-xl border border-slate-200 bg-white px-3 text-base dark:border-slate-700 dark:bg-slate-800" />
                    </label>
                  </div>
                )}
              </div>

              {/* DAYS OF WEEK SELECTION */}
              <div className="mb-4">
                <div className="flex items-center justify-between mb-1.5">
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                    {locale === "ar" ? "أيام أخذ الدواء في الأسبوع *" : "Prescription Days of Week *"}
                  </label>
                  <button
                    type="button"
                    onClick={() => setDaysOfWeek(ALL_DAY_IDS)}
                    className={`text-xs font-bold px-2 py-0.5 rounded-md border transition-all ${
                      daysOfWeek.length === 7
                        ? "bg-amber-500 text-white border-amber-500"
                        : "bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300 border-slate-200 dark:border-slate-700"
                    }`}
                  >
                    {locale === "ar" ? "يومياً (طوال الأسبوع)" : "Daily (Every Day)"}
                  </button>
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {DAYS_LIST.map((day) => {
                    const isSelected = daysOfWeek.includes(day.id);
                    return (
                      <button
                        type="button"
                        key={day.id}
                        onClick={() => handleToggleDay(day.id)}
                        className={`flex min-h-9 min-w-10 items-center justify-center px-2.5 py-1 rounded-xl text-xs font-bold border transition-all active:scale-95 ${
                          isSelected
                            ? "bg-amber-600 text-white border-amber-600 shadow-2xs"
                            : "bg-slate-50 text-slate-500 hover:border-slate-300 dark:bg-slate-800 dark:text-slate-400 border-slate-200 dark:border-slate-700"
                        }`}
                      >
                        {locale === "ar" ? day.labelAr : day.labelEn}
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* INSTRUCTIONS */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-4">
                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "تعليمات الاستخدام (عربي)" : "Instructions (Arabic)"}
                  </label>
                  <input
                    type="text"
                    placeholder="مثال: يؤخذ بعد الإفطار مع كوب ماء"
                    value={instAr}
                    onChange={(e) => setInstAr(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-base text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>

                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "تعليمات الاستخدام (إنجليزي)" : "Instructions (English)"}
                  </label>
                  <input
                    type="text"
                    placeholder="e.g. Take after breakfast with water"
                    value={instEn}
                    onChange={(e) => setInstEn(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-base text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
              </div>

              {/* PILL APPEARANCE & IMAGE SELECTOR */}
              <div className="mb-4 rounded-xl border border-slate-200 dark:border-slate-700/80 bg-white/70 dark:bg-slate-800/50 p-3">
                <label className="block text-xs font-bold text-slate-800 dark:text-slate-200 mb-2">
                  {locale === "ar" ? "صورة الدواء (العلبة أو الحبة)" : "Medicine photo (box or pill)"}
                </label>

                <div className="flex items-center gap-3 mb-2.5">
                  {/* Live preview */}
                  <div className="h-20 w-20 shrink-0 rounded-xl border-2 border-amber-400 bg-slate-50 dark:bg-slate-900 p-1 shadow-xs flex items-center justify-center">
                    <img
                      src={imageUrl || "/meds/pill-default.svg"}
                      alt="Pill Preview"
                      className="h-full w-full object-contain"
                      onError={(e) => {
                        (e.currentTarget as HTMLImageElement).src = "/meds/pill-default.svg";
                      }}
                    />
                  </div>

                  {/* Preset quick buttons */}
                  <div className="flex-1 grid grid-cols-2 sm:grid-cols-3 gap-1.5">
                    {PILL_PRESETS.map((p) => {
                      const isSelected = imageUrl === p.url;
                      return (
                        <button
                          type="button"
                          key={p.id}
                          onClick={() => setImageUrl(p.url)}
                          className={`flex items-center gap-1.5 p-1.5 rounded-lg border text-xs font-semibold transition-all ${
                            isSelected
                              ? "border-amber-500 bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200 ring-1 ring-amber-500"
                              : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300"
                          }`}
                        >
                          <img src={p.url} alt="" className="h-4 w-4 shrink-0 object-contain" />
                          <span className="truncate">{locale === "ar" ? p.labelAr : p.labelEn}</span>
                        </button>
                      );
                    })}
                  </div>
                </div>

                {/* Photo of the box or the pill, from the camera or the gallery */}
                <label
                  data-testid="med-photo-button"
                  className={`mb-2 flex min-h-12 w-full cursor-pointer items-center justify-center gap-2 rounded-xl border-2 border-dashed border-amber-400 px-3 text-sm font-bold text-amber-900 hover:bg-amber-50 dark:text-amber-200 dark:hover:bg-amber-950/40 ${photoBusy ? "pointer-events-none opacity-60" : ""}`}
                >
                  {photoBusy ? <Loader2 className="h-5 w-5 animate-spin" /> : <Camera className="h-5 w-5" />}
                  <span>
                    {photoBusy
                      ? locale === "ar" ? "جارٍ رفع الصورة..." : "Uploading photo..."
                      : photos.length
                        ? locale === "ar" ? `أضف صورة أخرى (${photos.length} من ${MAX_PHOTOS})` : `Add another photo (${photos.length} of ${MAX_PHOTOS})`
                        : locale === "ar" ? "التقط أو اختر صوراً (العلبة، الخلف، الحبة)" : "Take or choose photos (box, back, the pill)"}
                  </span>
                  <input
                    type="file"
                    accept="image/*"
                    multiple
                    data-testid="med-photo-input"
                    className="sr-only"
                    onChange={async (e) => {
                      const files = Array.from(e.target.files || []);
                      e.target.value = "";
                      if (!files.length) return;
                      setPhotoError("");
                      const room = MAX_PHOTOS - photos.length;
                      if (files.length > room) {
                        setPhotoError(
                          locale === "ar" ? `حدّ الصور ${MAX_PHOTOS} لكل دواء` : `At most ${MAX_PHOTOS} photos per medicine`,
                        );
                      }
                      setPhotoBusy(true);
                      let failed = false;
                      for (const file of files.slice(0, Math.max(0, room))) {
                        try {
                          const url = await uploadMedicinePhoto(file);
                          setPhotos((prev) => (prev.includes(url) ? prev : [...prev, url]));
                          // The first real photo becomes the main one instead of a drawn pill.
                          setImageUrl((prev) => (!prev || prev.startsWith("/meds/") ? url : prev));
                        } catch {
                          failed = true;
                        }
                      }
                      if (failed) setPhotoError(locale === "ar" ? "تعذر رفع صورة، جرّب صورة أخرى" : "Could not upload a photo, try another one");
                      setPhotoBusy(false);
                    }}
                  />
                </label>
                {photoError && (
                  <p role="alert" className="mb-2 text-sm font-semibold text-rose-700 dark:text-rose-300">{photoError}</p>
                )}

                {photos.length > 0 && (
                  <ul className="mb-3 grid grid-cols-2 gap-2 sm:grid-cols-3" data-testid="med-photo-list">
                    {photos.map((url) => {
                      const isMain = imageUrl === url;
                      return (
                        <li key={url} className={`rounded-xl border-2 p-1.5 ${isMain ? "border-amber-500" : "border-slate-200 dark:border-slate-700"}`}>
                          <img src={url} alt="" className="mb-1.5 h-20 w-full rounded-lg object-cover" />
                          {isMain ? (
                            <p className="mb-1 text-center text-xs font-bold text-amber-800 dark:text-amber-300">
                              {locale === "ar" ? "الصورة الرئيسية" : "Main photo"}
                            </p>
                          ) : (
                            <button
                              type="button"
                              onClick={() => setImageUrl(url)}
                              className="mb-1 min-h-11 w-full rounded-lg bg-amber-50 text-xs font-bold text-amber-900 dark:bg-amber-950/50 dark:text-amber-200"
                            >
                              {locale === "ar" ? "اجعلها الرئيسية" : "Make it the main one"}
                            </button>
                          )}
                          <button
                            type="button"
                            onClick={() => {
                              const rest = photos.filter((p) => p !== url);
                              setPhotos(rest);
                              if (isMain) setImageUrl(rest[0] || "/meds/pill-default.svg");
                            }}
                            className="min-h-11 w-full rounded-lg border border-slate-200 text-xs font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200"
                          >
                            {locale === "ar" ? "احذف" : "Remove"}
                          </button>
                        </li>
                      );
                    })}
                  </ul>
                )}

                {/* Custom URL input */}
                <div>
                  <input
                    type="text"
                    placeholder={locale === "ar" ? "أو أدخل رابط صورة مخصص (URL)" : "Or enter custom image URL"}
                    value={imageUrl}
                    onChange={(e) => setImageUrl(e.target.value)}
                    className="w-full rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-base text-slate-800 placeholder:text-slate-400 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
              </div>

              {/* ACTIONS */}
              <div className="flex items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={() => {
                    setShowForm(false);
                    setEditingMed(null);
                  }}
                  className="rounded-xl px-4 py-2 text-xs font-semibold text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
                >
                  {locale === "ar" ? "إلغاء" : "Cancel"}
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="flex items-center gap-1.5 rounded-xl bg-amber-600 hover:bg-amber-700 px-4 py-2 text-xs font-bold text-white shadow-sm active:scale-95 disabled:opacity-50"
                >
                  {isSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
                  <span>{locale === "ar" ? "حفظ الدواء" : "Save Prescription"}</span>
                </button>
              </div>
            </form>
          )}

          {/* MEDICATIONS LIST */}
          {loading ? (
            <div className="space-y-3">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-20 w-full animate-pulse rounded-2xl bg-slate-100 dark:bg-slate-800" />
              ))}
            </div>
          ) : meds.length === 0 ? (
            <div className="text-center py-8 text-xs text-slate-500">
              {locale === "ar" ? "لا توجد أدوية مسجلة حالياً" : "No prescriptions recorded"}
            </div>
          ) : (
            <div className="space-y-3">
              {meds.map((med) => {
                const name = locale === "ar" ? med.name_ar : med.name_en;
                const inst = locale === "ar" ? med.instructions_ar : med.instructions_en;

                return (
                  <div
                    key={med.id}
                    className="flex items-start justify-between p-3.5 rounded-2xl border border-slate-200 bg-white hover:border-slate-300 dark:bg-slate-800/80 dark:border-slate-700 shadow-2xs transition-all"
                  >
                    <div className="flex items-start gap-3 flex-1 min-w-0">
                      {/* Pill Image Thumbnail */}
                      <div className="h-12 w-12 shrink-0 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900 p-1 flex items-center justify-center">
                        <img
                          src={med.image_url || "/meds/pill-default.svg"}
                          alt={name}
                          className="h-full w-full object-contain"
                          onError={(e) => {
                            (e.currentTarget as HTMLImageElement).src = "/meds/pill-default.svg";
                          }}
                        />
                      </div>

                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 mb-1 flex-wrap">
                          <span className="text-sm font-bold text-slate-900 dark:text-white">
                            {name}
                          </span>
                          <span className="rounded-md bg-amber-100 px-2 py-0.5 text-xs font-bold text-amber-800 dark:bg-amber-950/80 dark:text-amber-300">
                            {med.dosage}
                          </span>
                          {med.is_prn && (
                            <span className="rounded-md bg-blue-100 px-2 py-0.5 text-xs font-bold text-blue-800 dark:bg-blue-950/80 dark:text-blue-300 border border-blue-200/60 dark:border-blue-800/60">
                              {locale === "ar" ? "عند الحاجة (PRN)" : "PRN"}
                            </span>
                          )}
                        </div>

                        {inst && (
                          <p className="text-xs text-slate-500 dark:text-slate-400 mb-2 leading-relaxed">
                            {inst}
                          </p>
                        )}

                      {/* Scheduled days & slots */}
                      <div className="flex items-center gap-1.5 flex-wrap">
                        {/* Days badge */}
                        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-amber-50 dark:bg-amber-950/60 text-xs font-bold text-amber-800 dark:text-amber-300 border border-amber-200/60 dark:border-amber-800/60">
                          <Check className="h-3 w-3" />
                          <span>
                            {!med.days_of_week || med.days_of_week.length === 7
                              ? locale === "ar"
                                ? "يومياً"
                                : "Daily"
                              : `${locale === "ar" ? "أيام:" : "Days:"} ${med.days_of_week
                                  .map((d) => (locale === "ar" ? DAYS_LIST.find((x) => x.id === d)?.labelAr : DAYS_LIST.find((x) => x.id === d)?.labelEn) || d)
                                  .join("، ")}`}
                          </span>
                        </span>

                        {/* Slots and Times */}
                        {med.frequency_slots?.map((s) => {
                          const slotDef = ALL_SLOTS.find((item) => item.id === s);
                          const slotTime = med.slot_times?.[s] || slotDef?.defaultTime;
                          return (
                            <span
                              key={s}
                              className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-slate-100 dark:bg-slate-700 text-xs font-semibold text-slate-700 dark:text-slate-300"
                            >
                              <Clock className="h-3 w-3 text-slate-400" />
                              <span>{locale === "ar" ? slotDef?.labelAr || s : slotDef?.labelEn || s}</span>
                              {slotTime && (
                                <span className="font-mono font-bold text-slate-900 dark:text-white ms-0.5">
                                  {slotTime}
                                </span>
                              )}
                            </span>
                          );
                        })}
                      </div>
                    </div>
                  </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1.5 ms-3">
                      <button
                        onClick={() => handleOpenEdit(med)}
                        title={locale === "ar" ? "تعديل الدواء والجرعة" : "Edit Medication"}
                        aria-label={locale === "ar" ? "تعديل الدواء والجرعة" : "Edit Medication"}
                        className="flex min-h-12 min-w-12 items-center justify-center rounded-xl border border-slate-200 p-2 text-slate-600 hover:bg-slate-50 hover:text-amber-600 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-700 transition-colors"
                      >
                        <Edit2 className="h-4 w-4" />
                      </button>
                      <button
                        onClick={() => handleDeleteMedication(med)}
                        title={locale === "ar" ? "تعطيل الدواء" : "Deactivate Prescription"}
                        aria-label={locale === "ar" ? "تعطيل الدواء" : "Deactivate Prescription"}
                        className="flex min-h-12 min-w-12 items-center justify-center rounded-xl border border-slate-200 p-2 text-slate-400 hover:border-rose-300 hover:text-rose-600 dark:border-slate-700 dark:hover:text-rose-400 transition-colors"
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
