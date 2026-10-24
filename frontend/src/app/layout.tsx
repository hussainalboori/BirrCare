import type { Metadata, Viewport } from "next";
import { cookies } from "next/headers";
import "./fonts.css";
import "./globals.css";
import { LanguageProvider } from "@/context/LanguageContext";
import { LOCALE_COOKIE, type Locale } from "@/lib/i18n";
import { CaregiverProvider } from "@/context/CaregiverContext";
import { PinPadModal } from "@/components/PinPadModal";
import { SseToast } from "@/components/SseToast";
import { OutboxBar } from "@/components/OutboxBar";
import { SharedDeviceGuard } from "@/components/SharedDeviceGuard";
import { EmergencyCardKeeper } from "@/components/EmergencyCardKeeper";
import { AlertCenter, EmergencySetupBanner, TimezoneMismatchBanner } from "@/components/AlertCenter";
import { Navigation } from "@/components/Navigation";
import { ThemeKeeper } from "@/components/ThemePicker";
import { UpdateBanner } from "@/components/UpdateBanner";
import { NewVersionBanner } from "@/components/NewVersionBanner";
import { EmergencyDefaultProvider } from "@/lib/emergencyDefault";
import { cleanEmergencyNumber } from "@/lib/emergencyNumber";
import { THEME_COLORS, THEME_COOKIE, THEME_SCRIPT, parseThemeMode, themeClasses } from "@/lib/theme";

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

export async function generateViewport(): Promise<Viewport> {
  const mode = parseThemeMode((await cookies()).get(THEME_COOKIE)?.value);
  return {
    width: "device-width",
    initialScale: 1,
    // Pinch-zoom stays enabled (WCAG 1.4.4); iOS focus-zoom is avoided by >=16px form controls.
    viewportFit: "cover",
    interactiveWidget: "resizes-content",
    // Night: a black status bar too. Auto is corrected by the pre-paint script.
    themeColor: mode === "auto" ? THEME_COLORS.light : THEME_COLORS[mode],
  };
}

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
  // Issue #21: an explicit Light/Dark/Night choice is rendered by the server, so
  // the first paint is already black at night. Auto depends on this phone's clock
  // and is applied by THEME_SCRIPT in <head>, before the body paints.
  const themeMode = parseThemeMode(cookieStore.get(THEME_COOKIE)?.value);
  const themeClass = themeMode === "auto" ? undefined : themeClasses(themeMode) || undefined;

  return (
    <html lang={locale} dir={dir} className={themeClass} suppressHydrationWarning>
      <head>
        {/* A constant string (no user data): see THEME_SCRIPT in lib/theme.ts */}
        <script dangerouslySetInnerHTML={{ __html: THEME_SCRIPT }} />
      </head>
      <body className="min-h-dvh bg-slate-50 text-slate-900 antialiased dark:bg-slate-950 dark:text-slate-100 font-sans selection:bg-emerald-500 selection:text-white pb-[calc(5rem+var(--outbox-h,0px)+env(safe-area-inset-bottom))]">
        <LanguageProvider initialLocale={locale}>
          {/* Read from the server's settings at request time, not baked into the build. */}
          <EmergencyDefaultProvider value={cleanEmergencyNumber(process.env.EMERGENCY_NUMBER)}>
          <CaregiverProvider>
            <EmergencySetupBanner />
            <TimezoneMismatchBanner />
            <UpdateBanner />
            {/* The version this page was loaded from (read at request time, like the emergency number). */}
            <NewVersionBanner pageVersion={process.env.GIT_SHA || ""} />
            <AlertCenter />
            <SseToast />
            <OutboxBar />
            <SharedDeviceGuard />
            <EmergencyCardKeeper />
            <ThemeKeeper />
            <PinPadModal />
            {children}
            <Navigation />
          </CaregiverProvider>
          </EmergencyDefaultProvider>
        </LanguageProvider>
      </body>
    </html>
  );
}
