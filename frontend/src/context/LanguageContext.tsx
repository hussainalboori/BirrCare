"use client";

import React, { createContext, useContext, useEffect, useState, useCallback } from "react";
import { Locale, LOCALE_COOKIE, translations } from "@/lib/i18n";

interface LanguageContextType {
  locale: Locale;
  dir: "rtl" | "ltr";
  t: (typeof translations)["ar"];
  setLocale: (loc: Locale) => void;
  toggleLocale: () => void;
}

const LanguageContext = createContext<LanguageContextType | undefined>(undefined);

function persistLocale(loc: Locale) {
  document.cookie = `${LOCALE_COOKIE}=${loc}; path=/; max-age=31536000; SameSite=Lax`;
  try {
    localStorage.setItem(LOCALE_COOKIE, loc);
  } catch {
    // storage may be unavailable (private mode)
  }
}

function applyToDocument(loc: Locale) {
  document.documentElement.setAttribute("dir", loc === "ar" ? "rtl" : "ltr");
  document.documentElement.setAttribute("lang", loc);
}

/**
 * `initialLocale` comes from the server (cookie), so the first client render
 * matches the SSR markup and the <html dir> set by the root layout.
 */
export function LanguageProvider({
  children,
  initialLocale = "ar",
}: {
  children: React.ReactNode;
  initialLocale?: Locale;
}) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale);

  // One-time migration: older clients stored the choice only in localStorage.
  useEffect(() => {
    let saved: string | null = null;
    try {
      saved = localStorage.getItem(LOCALE_COOKIE);
    } catch {
      return;
    }
    const hasCookie = document.cookie.split("; ").some((c) => c.startsWith(`${LOCALE_COOKIE}=`));
    if (!hasCookie && (saved === "ar" || saved === "en") && saved !== initialLocale) {
      persistLocale(saved);
      applyToDocument(saved);
      setLocaleState(saved);
    }
  }, [initialLocale]);

  const setLocale = useCallback((newLoc: Locale) => {
    setLocaleState(newLoc);
    persistLocale(newLoc);
    applyToDocument(newLoc);
  }, []);

  const toggleLocale = useCallback(() => {
    setLocale(locale === "ar" ? "en" : "ar");
  }, [locale, setLocale]);

  const dir = locale === "ar" ? "rtl" : "ltr";
  const t = translations[locale];

  return (
    <LanguageContext.Provider value={{ locale, dir, t, setLocale, toggleLocale }}>
      {children}
    </LanguageContext.Provider>
  );
}

export function useLanguage() {
  const ctx = useContext(LanguageContext);
  if (!ctx) {
    throw new Error("useLanguage must be used within a LanguageProvider");
  }
  return ctx;
}
