"use client";

import React, { useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { fetchApi } from "@/lib/api";
import { Ltr } from "./Ltr";
import { Phone, Siren } from "lucide-react";

interface Contacts {
  emergency_number?: string;
  doctor_name?: string;
  doctor_phone?: string;
}

// Fetched once per page load and shared by every crisis view.
let contactsPromise: Promise<Contacts> | null = null;
function loadContacts(): Promise<Contacts> {
  if (!contactsPromise) {
    contactsPromise = fetchApi<Contacts>("/contacts").catch(() => {
      contactsPromise = null; // retry next time instead of caching the failure
      return {};
    });
  }
  return contactsPromise;
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
export function CrisisActions({ compact = false }: { compact?: boolean }) {
  const { locale } = useLanguage();
  const [contacts, setContacts] = useState<Contacts>({});

  useEffect(() => {
    let alive = true;
    loadContacts().then((c) => alive && setContacts(c));
    return () => {
      alive = false;
    };
  }, []);

  const steps = STEPS[locale];
  const doctorLabel = contacts.doctor_name || (locale === "ar" ? "الطبيب" : "Doctor");

  return (
    <div className="space-y-3">
      {!compact && (
        <ol className="list-decimal space-y-1.5 ps-5 text-sm font-semibold leading-relaxed text-rose-900 dark:text-rose-200">
          {steps.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ol>
      )}

      <div className="grid gap-2 sm:grid-cols-2">
        {contacts.emergency_number && (
          <a
            href={`tel:${contacts.emergency_number}`}
            className="flex min-h-14 items-center justify-center gap-2 rounded-2xl bg-rose-600 px-4 text-base font-black text-white shadow-lg shadow-rose-500/30 active:scale-95"
          >
            <Siren className="h-5 w-5" />
            <span>{locale === "ar" ? "اتصل بالإسعاف" : "Call emergency"}</span>
            <Ltr>{contacts.emergency_number}</Ltr>
          </a>
        )}
        {contacts.doctor_phone && (
          <a
            href={`tel:${contacts.doctor_phone}`}
            className="flex min-h-14 items-center justify-center gap-2 rounded-2xl border-2 border-rose-600 bg-white px-4 text-base font-black text-rose-700 active:scale-95 dark:bg-slate-900 dark:text-rose-300"
          >
            <Phone className="h-5 w-5" />
            <span>
              {locale === "ar" ? "اتصل بـ" : "Call "}
              <bdi>{doctorLabel}</bdi>
            </span>
          </a>
        )}
      </div>
    </div>
  );
}
