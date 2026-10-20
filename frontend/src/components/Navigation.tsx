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

  // Every item has the same box: a 32px icon tile on top and a two-line label area,
  // in six equal columns, so icons and labels line up whether a label wraps or not.
  const itemClass = "flex min-h-14 min-w-0 flex-col items-center justify-start gap-1 rounded-2xl px-0.5 pt-1.5 pb-1 active:scale-95";
  const labelClass = "line-clamp-2 min-h-[2.4em] w-full text-center text-[11px] leading-[1.2]";

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
      className={`${itemClass} font-bold text-rose-700 dark:text-rose-300`}
    >
      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl bg-rose-600 text-white">
        <HeartPulse className="h-5 w-5 stroke-[2.5]" />
      </span>
      <span className={labelClass}>{locale === "ar" ? "قياس الضغط" : "Quick BP"}</span>
    </Link>
  );

  return (
    <nav className="fixed bottom-0 start-0 end-0 z-40 border-t border-slate-200/80 bg-white/95 backdrop-blur-md dark:border-slate-800 dark:bg-slate-900/95 pb-safe">
      <div className="mx-auto grid max-w-lg grid-cols-6 items-start gap-0.5 px-1 py-1.5">
        {links.flatMap((link, i) => {
          const Icon = link.icon;
          return [
            i === 2 ? quickBP : null,
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
            </Link>,
          ];
        })}
      </div>
    </nav>
  );
}
