"use client";

import React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useLanguage } from "@/context/LanguageContext";
import { Pill, Activity, ClipboardList, FileSpreadsheet, Syringe, HeartPulse } from "lucide-react";

export function Navigation() {
  const pathname = usePathname();
  const { t, locale } = useLanguage();

  const links = [
    {
      href: "/",
      label: t.navMeds,
      icon: Pill,
      active: pathname === "/",
    },
    {
      href: "/vitals",
      label: t.navVitals,
      icon: Activity,
      active: pathname.startsWith("/vitals"),
    },
    {
      href: "/insulin",
      label: locale === "ar" ? "الإنسولين" : "Insulin",
      icon: Syringe,
      active: pathname.startsWith("/insulin"),
    },
    {
      href: "/handovers",
      label: t.navHandovers,
      icon: ClipboardList,
      active: pathname.startsWith("/handovers"),
    },
    {
      href: "/export",
      label: t.navDoctorExport,
      icon: FileSpreadsheet,
      active: pathname.startsWith("/export"),
    },
  ];

  // Quick BP (issue #11): straight to the two blood-pressure fields from any page.
  const quickBP = (
    <Link
      key="quick-bp"
      href="/vitals?quick=bp"
      onClick={(e) => {
        if (pathname.startsWith("/vitals")) {
          e.preventDefault();
          window.dispatchEvent(new Event("birrcare:quick-bp"));
        }
      }}
      className="flex min-h-12 min-w-12 flex-col items-center justify-center gap-1 rounded-2xl px-1.5 py-1.5 font-bold text-rose-700 active:scale-95 dark:text-rose-300"
    >
      <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-rose-600 text-white shadow-sm">
        <HeartPulse className="h-5 w-5 stroke-[2.5]" />
      </div>
      <span className="text-[11px] leading-none">{locale === "ar" ? "قياس الضغط" : "Quick BP"}</span>
    </Link>
  );

  return (
    <nav className="fixed bottom-0 start-0 end-0 z-40 border-t border-slate-200/80 bg-white/95 backdrop-blur-md dark:border-slate-800 dark:bg-slate-900/95 pb-safe">
      <div className="mx-auto flex max-w-md items-center justify-around px-2 py-2">
        {links.flatMap((link, i) => {
          const Icon = link.icon;
          return [i === 2 ? quickBP : null, (
            <Link
              key={link.href}
              href={link.href}
              aria-current={link.active ? "page" : undefined}
              className={`flex min-h-12 min-w-12 flex-col items-center justify-center gap-1 rounded-2xl px-2 py-1.5 transition-all active:scale-95 ${
                link.active
                  ? "text-emerald-600 dark:text-emerald-400 font-bold"
                  : "text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200 font-medium"
              }`}
            >
              <div
                className={`flex h-8 w-8 items-center justify-center rounded-xl transition-all ${
                  link.active
                    ? "bg-emerald-50 dark:bg-emerald-950/60 shadow-xs"
                    : "bg-transparent"
                }`}
              >
                <Icon className={`h-5 w-5 ${link.active ? "stroke-[2.5]" : "stroke-[1.75]"}`} />
              </div>
              <span className="text-[11px] leading-none">{link.label}</span>
            </Link>
          )];
        })}
      </div>
    </nav>
  );
}
