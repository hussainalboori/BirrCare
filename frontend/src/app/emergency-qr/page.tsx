"use client";

import React, { useCallback, useEffect, useState } from "react";
import QRCode from "qrcode";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLiveSync } from "@/hooks/useLiveSync";
import { fetchApi } from "@/lib/api";
import { canManageMedications } from "@/lib/permissions";
import { emergencyPath, storeEmergencyToken, useEmergencyToken } from "@/lib/emergencyLink";
import { Printer, Siren, QrCode, RefreshCw, Save, Loader2 } from "lucide-react";

// Issue #19: the QR code for the fridge or the front door, and (admins) the
// patient details the paramedics' card shows.

interface Profile {
  name: string;
  dob: string;
  diagnosis_ar: string;
  diagnosis_en: string;
  stroke_date: string;
  allergies_ar: string;
  allergies_en: string;
  blood_type: string;
  baseline_bp: string;
  notes: string;
}

const EMPTY: Profile = { name: "", dob: "", diagnosis_ar: "", diagnosis_en: "", stroke_date: "", allergies_ar: "", allergies_en: "", blood_type: "", baseline_bp: "", notes: "" };

export default function EmergencyQrPage() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const { status } = useLiveSync({});
  const ar = locale === "ar";
  const isAdmin = canManageMedications(currentCaregiver?.role);
  const token = useEmergencyToken();
  const [qr, setQr] = useState<string | null>(null);
  const [url, setUrl] = useState("");
  const [profile, setProfile] = useState<Profile>(EMPTY);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");
  const [confirmNew, setConfirmNew] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetchApi<{ profile: Profile }>("/patient");
      setProfile({ ...EMPTY, ...res.profile });
    } catch {
      // offline: the QR from this phone still shows
    }
    try {
      storeEmergencyToken((await fetchApi<{ token: string }>("/patient/emergency-link")).token);
    } catch (err) {
      if ((err as { status?: number }).status === 404) storeEmergencyToken(null);
    }
  }, []);

  useEffect(() => {
    if (currentCaregiver) load();
  }, [currentCaregiver, load]);

  useEffect(() => {
    if (!token) {
      setQr(null);
      return;
    }
    const full = `${window.location.origin}${emergencyPath(token)}`;
    setUrl(full);
    QRCode.toDataURL(full, { margin: 2, width: 640, errorCorrectionLevel: "M" }).then(setQr, () => setQr(null));
  }, [token]);

  const saveProfile = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setMessage("");
    try {
      await fetchApi("/patient", { method: "PUT", body: JSON.stringify(profile) });
      setMessage(ar ? "حُفظت البيانات. ستظهر في البطاقة فوراً." : "Saved. The card shows it right away.");
    } catch (err) {
      setMessage((err as Error).message || (ar ? "تعذر الحفظ" : "Could not save"));
    } finally {
      setSaving(false);
    }
  };

  const makeLink = async () => {
    setConfirmNew(false);
    setMessage("");
    try {
      storeEmergencyToken((await fetchApi<{ token: string }>("/patient/emergency-link", { method: "POST" })).token);
      setMessage(ar ? "رمز QR جديد جاهز. اطبعه وضعه مكان القديم." : "New QR code ready. Print it and replace the old one.");
    } catch (err) {
      setMessage((err as Error).message || (ar ? "تعذر إنشاء الرمز" : "Could not make the QR code"));
    }
  };

  const field = (key: keyof Profile, label: string, opts: { type?: string; dir?: "rtl" | "ltr"; long?: boolean } = {}) => (
    <label className="block">
      <span className="mb-1 block text-sm font-bold text-slate-700 dark:text-slate-200">{label}</span>
      {opts.long ? (
        <textarea
          rows={4}
          dir={opts.dir}
          value={profile[key]}
          onChange={(e) => setProfile((p) => ({ ...p, [key]: e.target.value }))}
          className="w-full rounded-xl border border-slate-200 bg-slate-50 p-3 text-base dark:border-slate-700 dark:bg-slate-800"
        />
      ) : (
        <input
          type={opts.type || "text"}
          dir={opts.dir}
          value={profile[key]}
          onChange={(e) => setProfile((p) => ({ ...p, [key]: e.target.value }))}
          className="min-h-12 w-full rounded-xl border border-slate-200 bg-slate-50 px-3 text-base dark:border-slate-700 dark:bg-slate-800"
        />
      )}
    </label>
  );

  return (
    <div className="min-h-screen">
      <CaregiverHeader connectionStatus={status} />
      <main className="mx-auto max-w-3xl px-4 py-5 sm:px-6">
        <h1 className="no-print mb-1 flex items-center gap-2 text-xl font-black text-slate-900 dark:text-white">
          <QrCode className="h-6 w-6 text-rose-700" />
          {ar ? "بطاقة الطوارئ للمسعفين" : "Emergency card for paramedics"}
        </h1>
        <p className="no-print mb-4 text-sm text-slate-600 dark:text-slate-300">
          {ar
            ? "يفتحها أي شخص بمسح الرمز، بدون تسجيل دخول: الأدوية والجرعات، آخر ضغط، الحساسية وأرقام الاتصال. ضعها على الثلاجة أو باب البيت. يصل تنبيه للعائلة كلما فُتحت."
            : "Anyone who scans it can open it without logging in: medicines and doses, last BP, allergies and phone numbers. Put it on the fridge or the front door. The family is alerted whenever it is opened."}
        </p>

        {token && qr ? (
          <section className="mb-6 rounded-3xl border-4 border-rose-700 bg-white p-4 text-center text-slate-900">
            <p className="text-2xl font-black text-rose-700">EMERGENCY MEDICAL CARD</p>
            <p className="text-2xl font-black text-rose-700" dir="rtl" lang="ar">
              بطاقة طبية للطوارئ
            </p>
            <img src={qr} alt={ar ? "رمز QR لبطاقة الطوارئ" : "QR code for the emergency card"} className="mx-auto my-3 w-full max-w-xs" />
            <p className="text-lg font-bold">Scan with the phone camera — no login needed</p>
            <p className="text-lg font-bold" dir="rtl" lang="ar">
              امسح الرمز بكاميرا الهاتف — لا يحتاج تسجيل دخول
            </p>
          </section>
        ) : (
          <p className="mb-6 rounded-2xl border border-dashed border-slate-300 p-4 text-center text-sm font-bold text-slate-600 dark:border-slate-700 dark:text-slate-300">
            {isAdmin
              ? ar ? "لا يوجد رمز بعد. أدخل بيانات المريضة ثم اضغط «إنشاء رمز QR»." : "No QR code yet. Fill in the patient details, then tap “Make the QR code”."
              : ar ? "لم يُعدّ المشرف بطاقة الطوارئ بعد." : "An admin hasn't set up the emergency card yet."}
          </p>
        )}

        <div className="no-print mb-6 grid grid-cols-1 gap-2 sm:grid-cols-2">
          {token && (
            <>
              <a href={emergencyPath(token)} className="flex min-h-14 items-center justify-center gap-2 rounded-2xl bg-rose-700 text-base font-black text-white">
                <Siren className="h-5 w-5" />
                {ar ? "افتح البطاقة" : "Open the card"}
              </a>
              <button type="button" onClick={() => window.print()} className="flex min-h-14 items-center justify-center gap-2 rounded-2xl border-2 border-slate-300 text-base font-bold text-slate-800 dark:border-slate-600 dark:text-slate-100">
                <Printer className="h-5 w-5" />
                {ar ? "اطبع رمز QR" : "Print the QR code"}
              </button>
            </>
          )}
        </div>
        {token && url && (
          <p dir="ltr" className="no-print mb-6 break-all text-start text-xs text-slate-500">
            {url}
          </p>
        )}

        {message && (
          <p role="status" className="no-print mb-4 rounded-xl bg-emerald-50 p-3 text-sm font-bold text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200">
            {message}
          </p>
        )}

        {isAdmin && (
          <div className="no-print space-y-6">
            <form onSubmit={saveProfile} className="space-y-3 rounded-3xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
              <h2 className="text-base font-black text-slate-900 dark:text-white">{ar ? "بيانات المريضة على البطاقة" : "Patient details on the card"}</h2>
              <p className="text-xs text-slate-500 dark:text-slate-400">
                {ar ? "الأدوية وآخر القراءات تُضاف تلقائياً. الأدوية المعلّمة «حرجة» تظهر كمميّع للدم." : "Medicines and the latest readings are added automatically. Medicines marked “critical” show as blood thinners."}
              </p>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                {field("name", ar ? "الاسم" : "Name")}
                {field("dob", ar ? "تاريخ الميلاد" : "Date of birth", { type: "date" })}
                {field("diagnosis_ar", ar ? "التشخيص (عربي)" : "Diagnosis (Arabic)", { dir: "rtl" })}
                {field("diagnosis_en", ar ? "التشخيص (إنجليزي)" : "Diagnosis (English)", { dir: "ltr" })}
                {field("stroke_date", ar ? "تاريخ الجلطة" : "Stroke date", { type: "date" })}
                {field("blood_type", ar ? "فصيلة الدم" : "Blood type", { dir: "ltr" })}
                {field("allergies_ar", ar ? "الحساسية (عربي)" : "Allergies (Arabic)", { dir: "rtl" })}
                {field("allergies_en", ar ? "الحساسية (إنجليزي)" : "Allergies (English)", { dir: "ltr" })}
                {field("baseline_bp", ar ? "الضغط المعتاد (مثل 130/80)" : "Usual BP (e.g. 130/80)", { dir: "ltr" })}
              </div>
              {field("notes", ar ? "ملاحظات للمسعفين" : "Notes for paramedics", { long: true })}
              <button type="submit" disabled={saving} className="flex min-h-12 w-full items-center justify-center gap-2 rounded-xl bg-emerald-600 text-base font-bold text-white disabled:opacity-50">
                {saving ? <Loader2 className="h-5 w-5 animate-spin" /> : <Save className="h-5 w-5" />}
                {ar ? "احفظ البيانات" : "Save details"}
              </button>
            </form>

            <section className="space-y-2 rounded-3xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
              {!token ? (
                <button type="button" onClick={makeLink} className="flex min-h-14 w-full items-center justify-center gap-2 rounded-2xl bg-rose-700 text-base font-black text-white">
                  <QrCode className="h-5 w-5" />
                  {ar ? "إنشاء رمز QR" : "Make the QR code"}
                </button>
              ) : !confirmNew ? (
                <button type="button" onClick={() => setConfirmNew(true)} className="flex min-h-12 w-full items-center justify-center gap-2 rounded-xl border-2 border-slate-300 text-sm font-bold text-slate-700 dark:border-slate-600 dark:text-slate-200">
                  <RefreshCw className="h-4 w-4" />
                  {ar ? "رمز جديد (إذا ضاع القديم أو انتشر)" : "New QR code (if the old one was lost or shared)"}
                </button>
              ) : (
                <div className="space-y-2 rounded-2xl bg-amber-50 p-3 dark:bg-amber-950/40">
                  <p className="text-sm font-bold text-amber-900 dark:text-amber-200">
                    {ar ? "الرمز القديم سيتوقف عن العمل فوراً، في كل مكان طُبع فيه. متأكد؟" : "The old QR code stops working at once, wherever it is printed. Sure?"}
                  </p>
                  <div className="grid grid-cols-2 gap-2">
                    <button type="button" onClick={() => setConfirmNew(false)} className="min-h-12 rounded-xl border border-slate-300 text-sm font-bold">
                      {ar ? "إلغاء" : "Cancel"}
                    </button>
                    <button type="button" onClick={makeLink} className="min-h-12 rounded-xl bg-rose-700 text-sm font-black text-white">
                      {ar ? "نعم، رمز جديد" : "Yes, new QR code"}
                    </button>
                  </div>
                </div>
              )}
              <p className="text-xs text-slate-500 dark:text-slate-400">
                {ar
                  ? "ليعمل الرمز بدون تسجيل دخول، يجب إضافة سياسة Bypass في Cloudflare Access للمسار /emergency/* فقط (انظر docs/DEPLOYMENT.md)."
                  : "For the QR code to open without a login, add a Cloudflare Access Bypass policy for /emergency/* only (see docs/DEPLOYMENT.md)."}
              </p>
            </section>
          </div>
        )}
      </main>
    </div>
  );
}
