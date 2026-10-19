"use client";

import React, { useState, useEffect } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { Medication } from "@/lib/types";
import { canManageMedications } from "@/lib/permissions";
import { fetchApi } from "@/lib/api";
import {
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
  { id: "morning", labelAr: "الصباح", labelEn: "Morning" },
  { id: "afternoon", labelAr: "الظهيرة", labelEn: "Afternoon" },
  { id: "evening", labelAr: "المساء", labelEn: "Evening" },
  { id: "bedtime", labelAr: "قبل النوم", labelEn: "Bedtime" },
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
  const [selectedSlots, setSelectedSlots] = useState<string[]>(["morning"]);

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
    setSelectedSlots(["morning"]);
    setShowForm(true);
  };

  const handleOpenEdit = (med: Medication) => {
    setEditingMed(med);
    setNameAr(med.name_ar);
    setNameEn(med.name_en);
    setDosage(med.dosage);
    setInstAr(med.instructions_ar || "");
    setInstEn(med.instructions_en || "");
    setSelectedSlots(med.frequency_slots || ["morning"]);
    setShowForm(true);
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

  const handleSaveMedication = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!nameAr.trim() || !dosage.trim() || selectedSlots.length === 0) return;

    setIsSubmitting(true);
    try {
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
            frequency_slots: selectedSlots,
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
            frequency_slots: selectedSlots,
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

    try {
      await fetchApi(`/medications/${med.id}`, { method: "DELETE" });
      await loadMeds();
      onUpdated?.();
    } catch (err: any) {
      alert(err.message || "Failed to delete");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 backdrop-blur-xs p-4 animate-in fade-in duration-200">
      <div className="relative w-full max-w-2xl max-h-[90vh] flex flex-col rounded-3xl bg-white shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800 overflow-hidden">
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
            onClick={onClose}
            className="rounded-full p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800"
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
                  <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "الاسم بالعربية *" : "Arabic Name *"}
                  </label>
                  <input
                    type="text"
                    required
                    placeholder="مثال: كلوبيدوغريل (بلافيكس)"
                    value={nameAr}
                    onChange={(e) => setNameAr(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs font-bold text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>

                <div>
                  <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "الاسم بالإنجليزية" : "English Name"}
                  </label>
                  <input
                    type="text"
                    placeholder="e.g. Clopidogrel (Plavix)"
                    value={nameEn}
                    onChange={(e) => setNameEn(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs font-bold text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>

                <div>
                  <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "الجرعة المقررة *" : "Prescribed Dosage *"}
                  </label>
                  <input
                    type="text"
                    required
                    placeholder="مثال: 75mg قرص واحد"
                    value={dosage}
                    onChange={(e) => setDosage(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs font-bold text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
              </div>

              {/* FREQUENCY SLOTS SELECTION */}
              <div className="mb-3">
                <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                  {locale === "ar" ? "فترات اليوم المقررة للجرعة *" : "Scheduled Daily Time Slots *"}
                </label>
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                  {ALL_SLOTS.map((slot) => {
                    const isChecked = selectedSlots.includes(slot.id);
                    return (
                      <button
                        type="button"
                        key={slot.id}
                        onClick={() => handleToggleSlot(slot.id)}
                        className={`flex items-center justify-between p-2 rounded-xl border text-xs font-bold transition-all ${
                          isChecked
                            ? "border-amber-500 bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200"
                            : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 dark:border-slate-800 dark:bg-slate-800 dark:text-slate-300"
                        }`}
                      >
                        <span className="flex items-center gap-1.5">
                          <Clock className="h-3.5 w-3.5" />
                          <span>{locale === "ar" ? slot.labelAr : slot.labelEn}</span>
                        </span>
                        {isChecked && <Check className="h-3.5 w-3.5 text-amber-600" />}
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* INSTRUCTIONS */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-4">
                <div>
                  <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "تعليمات الاستخدام (عربي)" : "Instructions (Arabic)"}
                  </label>
                  <input
                    type="text"
                    placeholder="مثال: يؤخذ بعد الإفطار مع كوب ماء"
                    value={instAr}
                    onChange={(e) => setInstAr(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>

                <div>
                  <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "تعليمات الاستخدام (إنجليزي)" : "Instructions (English)"}
                  </label>
                  <input
                    type="text"
                    placeholder="e.g. Take after breakfast with water"
                    value={instEn}
                    onChange={(e) => setInstEn(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
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
                    <div className="flex-1">
                      <div className="flex items-center gap-2 mb-1">
                        <span className="text-sm font-bold text-slate-900 dark:text-white">
                          {name}
                        </span>
                        <span className="rounded-md bg-amber-100 px-2 py-0.5 text-[11px] font-bold text-amber-800 dark:bg-amber-950/80 dark:text-amber-300">
                          {med.dosage}
                        </span>
                      </div>

                      {inst && (
                        <p className="text-xs text-slate-500 dark:text-slate-400 mb-2 leading-relaxed">
                          {inst}
                        </p>
                      )}

                      {/* Scheduled slots */}
                      <div className="flex items-center gap-1.5 flex-wrap">
                        {med.frequency_slots?.map((s) => {
                          const slotDef = ALL_SLOTS.find((item) => item.id === s);
                          return (
                            <span
                              key={s}
                              className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-slate-100 dark:bg-slate-700 text-[10px] font-semibold text-slate-600 dark:text-slate-300"
                            >
                              <Clock className="h-3 w-3" />
                              <span>{locale === "ar" ? slotDef?.labelAr || s : slotDef?.labelEn || s}</span>
                            </span>
                          );
                        })}
                      </div>
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1.5 ms-3">
                      <button
                        onClick={() => handleOpenEdit(med)}
                        title={locale === "ar" ? "تعديل الدواء والجرعة" : "Edit Medication"}
                        className="rounded-xl border border-slate-200 p-2 text-slate-600 hover:bg-slate-50 hover:text-amber-600 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-700 transition-colors"
                      >
                        <Edit2 className="h-4 w-4" />
                      </button>
                      <button
                        onClick={() => handleDeleteMedication(med)}
                        title={locale === "ar" ? "تعطيل الدواء" : "Deactivate Prescription"}
                        className="rounded-xl border border-slate-200 p-2 text-slate-400 hover:border-rose-300 hover:text-rose-600 dark:border-slate-700 dark:hover:text-rose-400 transition-colors"
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
