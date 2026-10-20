"use client";

import React, { useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { fetchApi } from "@/lib/api";
import { Ltr } from "./Ltr";
import { Phone, Siren } from "lucide-react";

export interface Contacts {
  emergency_number?: string;
  doctor_name?: string;
  doctor_phone?: string;
}

/**
 * Emergency number used when the server has none or can't be reached (offline,
 * Wi-Fi blip): the last number this phone received, else a build-time default
 * (NEXT_PUBLIC_DEFAULT_EMERGENCY, set from EMERGENCY_NUMBER when the image is
 * built), else 112 (the GSM emergency number most mobile networks route).
 * Calling must never depend on the network (issue #11).
 */
export const BUILD_EMERGENCY_NUMBER = process.env.NEXT_PUBLIC_DEFAULT_EMERGENCY || "112";
const CACHE_KEY = "birrcare_contacts";

function cachedContacts(): Contacts {
  try {
    return JSON.parse(localStorage.getItem(CACHE_KEY) || "{}") as Contacts;
  } catch {
    return {};
  }
}

// Fetched once per page load and shared by every crisis view.
let contactsPromise: Promise<Contacts | null> | null = null;
function loadContacts(): Promise<Contacts | null> {
  if (!contactsPromise) {
    contactsPromise = fetchApi<Contacts>("/contacts")
      .then((c) => {
        try {
          localStorage.setItem(CACHE_KEY, JSON.stringify(c ?? {}));
        } catch {}
        return c ?? {};
      })
      .catch(() => {
        contactsPromise = null; // retry next time instead of caching the failure
        return null;
      });
  }
  return contactsPromise;
}

/**
 * Contacts for the call buttons. Starts from this phone's cached copy so the
 * buttons render instantly and offline; `configured` is false only when the
 * server answered and has no EMERGENCY_NUMBER (for the admin setup banner).
 */
export function useContacts(): Contacts & { emergency: string; configured: boolean | null } {
  const [contacts, setContacts] = useState<Contacts>({});
  const [configured, setConfigured] = useState<boolean | null>(null);
  useEffect(() => {
    let alive = true;
    setContacts(cachedContacts());
    loadContacts().then((c) => {
      if (!alive || c === null) return;
      setContacts(c);
      setConfigured(!!c.emergency_number);
    });
    return () => {
      alive = false;
    };
  }, []);
  return { ...contacts, emergency: contacts.emergency_number || BUILD_EMERGENCY_NUMBER, configured };
}

const STEPS = {
  ar: [
    "اتصل بالإسعاف فوراً إذا ظهر ضعف أو خدر جديد، ميلان في الوجه، صعوبة في الكلام، تشوش، صداع شديد، ألم في الصدر أو ضيق في التنفس.",
    "بدون هذه الأعراض: أبقِها جالسة وهادئة، وأعد القياس بعد 5 دقائق.",
    "إذا بقي الضغط 180/120 أو أكثر: اتصل بالطبيب الآن.",
    "لا تعطِ جرعة ضغط إضافية إلا بتوجيه من الطبيب.",
  ],
  en: [
    "Call emergency now if there is new weakness or numbness, face drooping, trouble speaking, confusion, severe headache, chest pain or shortness of breath.",
    "No such symptoms: keep her seated and calm, and measure again in 5 minutes.",
    "Still 180/120 or higher: call the doctor now.",
    "Do not give an extra blood pressure dose unless the doctor tells you to.",
  ],
};

/**
 * What to do right now during a hypertensive crisis: four short steps and
 * oversized one-tap call buttons. Numbers come from EMERGENCY_NUMBER /
 * DOCTOR_PHONE in the backend .env.
 */
/** One-tap call to the doctor (DOCTOR_PHONE). Renders nothing when no number is set. */
export function DoctorCallButton() {
  const { locale } = useLanguage();
  const contacts = useContacts();
  if (!contacts.doctor_phone) return null;
  const doctorLabel = contacts.doctor_name || (locale === "ar" ? "الطبيب" : "Doctor");
  return (
    <a
      href={`tel:${contacts.doctor_phone}`}
      className="flex min-h-12 items-center justify-center gap-2 rounded-2xl border-2 border-orange-600 bg-white px-4 text-base font-black text-orange-800 active:scale-95 dark:bg-slate-900 dark:text-orange-300"
    >
      <Phone className="h-5 w-5" />
      <span>
        {locale === "ar" ? "اتصل بـ" : "Call "}
        <bdi>{doctorLabel}</bdi>
      </span>
    </a>
  );
}

/**
 * compact: call buttons only. calls={false}: the steps only (when the buttons are
 * already pinned elsewhere, e.g. the vitals form header).
 */
export function CrisisActions({ compact = false, calls = true }: { compact?: boolean; calls?: boolean }) {
  const { locale } = useLanguage();
  const contacts = useContacts();

  const steps = STEPS[locale];
  const doctorLabel = contacts.doctor_name || (locale === "ar" ? "الطبيب" : "Doctor");

  // Call buttons come first and are never hidden: the emergency one always has a
  // number (see useContacts), so calling never waits for a save or the network.
  return (
    <div className="space-y-3">
      {calls && (
        <div className="grid gap-2 sm:grid-cols-2">
          <a
            href={`tel:${contacts.emergency}`}
            className="flex min-h-16 items-center justify-center gap-2 rounded-2xl bg-rose-700 px-4 text-lg font-black text-white shadow-lg shadow-rose-500/30 active:scale-95"
          >
            <Siren className="h-6 w-6" />
            <span>{locale === "ar" ? "اتصل بالإسعاف" : "Call emergency"}</span>
            <Ltr>{contacts.emergency}</Ltr>
          </a>
          {contacts.doctor_phone && (
            <a
              href={`tel:${contacts.doctor_phone}`}
              className="flex min-h-16 items-center justify-center gap-2 rounded-2xl border-2 border-rose-700 bg-white px-4 text-lg font-black text-rose-800 active:scale-95 dark:bg-slate-900 dark:text-rose-200"
            >
              <Phone className="h-6 w-6" />
              <span>
                {locale === "ar" ? "اتصل بـ" : "Call "}
                <bdi>{doctorLabel}</bdi>
              </span>
            </a>
          )}
        </div>
      )}

      {!compact && (
        <ol className="list-decimal space-y-1.5 ps-5 text-sm font-semibold leading-relaxed text-rose-900 dark:text-rose-200">
          {steps.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ol>
      )}
    </div>
  );
}
