import type { Metadata, Viewport } from "next";
import { cookies } from "next/headers";
import "./fonts.css";
import "./globals.css";
import { LanguageProvider } from "@/context/LanguageContext";
import { LOCALE_COOKIE, type Locale } from "@/lib/i18n";
import { CaregiverProvider } from "@/context/CaregiverContext";
import { PinPadModal } from "@/components/PinPadModal";
import { SseToast } from "@/components/SseToast";
import { CrisisBar, EmergencySetupBanner } from "@/components/CrisisBar";
import { Navigation } from "@/components/Navigation";

export const metadata: Metadata = {
  title: "رعاية بر — Birrcare | Family Care Coordination",
  description: "Single source of truth for stroke recovery medication, vitals, and handovers.",
  manifest: "/manifest.json",
  appleWebApp: {
    capable: true,
    statusBarStyle: "default",
    title: "Birrcare",
  },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  // Pinch-zoom stays enabled (WCAG 1.4.4); iOS focus-zoom is avoided by >=16px form controls.
  viewportFit: "cover",
  interactiveWidget: "resizes-content",
  themeColor: "#059669",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  // Resolve language server-side from the cookie so <html dir/lang> is correct in the
  // first paint (no RTL→LTR flash, no hydration mismatch).
  const cookieStore = await cookies();
  const locale: Locale = cookieStore.get(LOCALE_COOKIE)?.value === "en" ? "en" : "ar";
  const dir = locale === "ar" ? "rtl" : "ltr";

  return (
    <html lang={locale} dir={dir}>
      <body className="min-h-dvh bg-slate-50 text-slate-900 antialiased dark:bg-slate-950 dark:text-slate-100 font-sans selection:bg-emerald-500 selection:text-white pb-[calc(5rem+env(safe-area-inset-bottom))]">
        <LanguageProvider initialLocale={locale}>
          <CaregiverProvider>
            <EmergencySetupBanner />
            <CrisisBar />
            <SseToast />
            <PinPadModal />
            {children}
            <Navigation />
          </CaregiverProvider>
        </LanguageProvider>
      </body>
    </html>
  );
}
