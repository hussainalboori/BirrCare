"use client";

import React, { createContext, useContext, useEffect, useState } from "react";
import { Locale, translations } from "@/lib/i18n";

interface LanguageContextType {
  locale: Locale;
  dir: "rtl" | "ltr";
  t: (typeof translations)["ar"];
  setLocale: (loc: Locale) => void;
  toggleLocale: () => void;
}

const LanguageContext = createContext<LanguageContextType | undefined>(undefined);

export function LanguageProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>("ar");

  useEffect(() => {
    // Check cookie or localStorage
    const saved = localStorage.getItem("birrcare_lang") as Locale;
    if (saved === "ar" || saved === "en") {
      setLocaleState(saved);
      document.documentElement.setAttribute("dir", saved === "ar" ? "rtl" : "ltr");
      document.documentElement.setAttribute("lang", saved);
    } else {
      document.documentElement.setAttribute("dir", "rtl");
      document.documentElement.setAttribute("lang", "ar");
    }
  }, []);

  const setLocale = (newLoc: Locale) => {
    setLocaleState(newLoc);
    localStorage.setItem("birrcare_lang", newLoc);
    document.cookie = `birrcare_lang=${newLoc}; path=/; max-age=31536000; SameSite=Lax`;
    document.documentElement.setAttribute("dir", newLoc === "ar" ? "rtl" : "ltr");
    document.documentElement.setAttribute("lang", newLoc);
  };

  const toggleLocale = () => {
    setLocale(locale === "ar" ? "en" : "ar");
  };

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
