"use client";

import React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useLanguage } from "@/context/LanguageContext";
import { Pill, Activity, ClipboardList, ClipboardCheck, FileSpreadsheet, Syringe } from "lucide-react";

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
      href: "/report",
      label: locale === "ar" ? "التقرير" : "Report",
      icon: ClipboardCheck,
      active: pathname.startsWith("/report"),
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

  // Every item has the same box: a 32px icon tile on top and a two-line label area,
  // in six equal columns, so icons and labels line up whether a label wraps or not.
  // Labels are never clipped: at 375px a long Arabic word ("المؤشرات") may run a
  // pixel or two into the gap rather than lose a letter.
  const itemClass = "flex min-h-14 min-w-0 flex-col items-center justify-start gap-1 rounded-2xl pt-1.5 pb-1 active:scale-95";
  const labelClass = "min-h-[2.4em] w-full text-center text-xs leading-[1.2]";

  return (
    <nav className="fixed bottom-0 start-0 end-0 z-40 border-t border-slate-200/80 bg-white/95 backdrop-blur-md dark:border-slate-800 dark:bg-slate-900/95 pb-safe">
      <div className="mx-auto grid max-w-xl grid-cols-6 items-start gap-px px-0.5 py-1.5">
        {links.map((link) => {
          const Icon = link.icon;
          return (
            <Link
              key={link.href}
              href={link.href}
              aria-current={link.active ? "page" : undefined}
              className={`${itemClass} transition-colors ${
                link.active
                  ? "font-bold text-emerald-700 dark:text-emerald-400"
                  : "font-medium text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-200"
              }`}
            >
              <span
                className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-xl ${
                  link.active ? "bg-emerald-50 dark:bg-emerald-950/60" : ""
                }`}
              >
                <Icon className={`h-5 w-5 ${link.active ? "stroke-[2.5]" : "stroke-[1.75]"}`} />
              </span>
              <span className={labelClass}>{link.label}</span>
            </Link>
          );
        })}
      </div>
    </nav>
  );
}
