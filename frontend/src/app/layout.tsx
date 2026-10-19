import type { Metadata, Viewport } from "next";
import { Tajawal, Inter } from "next/font/google";
import "./globals.css";
import { LanguageProvider } from "@/context/LanguageContext";
import { CaregiverProvider } from "@/context/CaregiverContext";
import { PinPadModal } from "@/components/PinPadModal";
import { SseToast } from "@/components/SseToast";
import { Navigation } from "@/components/Navigation";

const tajawal = Tajawal({
  subsets: ["arabic", "latin"],
  weight: ["400", "500", "700"],
  variable: "--font-tajawal",
  display: "swap",
});

const inter = Inter({
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-inter",
  display: "swap",
});

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
  maximumScale: 1,
  userScalable: false,
  themeColor: "#059669",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="ar" dir="rtl" className={`${tajawal.variable} ${inter.variable}`}>
      <body className="min-h-screen bg-slate-50 text-slate-900 antialiased dark:bg-slate-950 dark:text-slate-100 font-sans selection:bg-emerald-500 selection:text-white pb-20">
        <LanguageProvider>
          <CaregiverProvider>
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
