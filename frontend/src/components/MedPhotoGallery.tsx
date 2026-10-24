"use client";

import React, { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ChevronLeft, ChevronRight, Maximize2, Minus, Plus, X } from "lucide-react";

const FALLBACK = "/meds/pill-default.svg";

/** The main photo first, then the others (box, back, the pill…), without repeats. */
export function medPhotoList(imageUrl?: string, photoUrls?: string[]): string[] {
  const list = [imageUrl, ...(photoUrls || [])].filter((u): u is string => !!u);
  const unique = [...new Set(list)];
  return unique.length ? unique : [FALLBACK];
}

function PrevNext({ at, n, setI, ar, dark }: { at: number; n: number; setI: (i: number) => void; ar: boolean; dark?: boolean }) {
  // In Arabic the "previous" arrow points right.
  const Prev = ar ? ChevronRight : ChevronLeft;
  const Next = ar ? ChevronLeft : ChevronRight;
  const btn = dark
    ? "flex min-h-12 items-center gap-1 rounded-xl bg-white/15 px-3 text-sm font-bold text-white"
    : "flex min-h-12 items-center gap-1 rounded-xl border border-slate-200 px-3 text-sm font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200";
  return (
    <div className="flex items-center justify-center gap-2">
      <button type="button" onClick={() => setI((at - 1 + n) % n)} className={btn}>
        <Prev className="h-4 w-4" />
        {ar ? "السابقة" : "Previous"}
      </button>
      <span className={`min-w-14 text-center text-sm font-bold ${dark ? "text-white" : "text-slate-600 dark:text-slate-300"}`} aria-live="polite">
        {ar ? `${at + 1} من ${n}` : `${at + 1} of ${n}`}
      </span>
      <button type="button" onClick={() => setI((at + 1) % n)} className={btn}>
        {ar ? "التالية" : "Next"}
        <Next className="h-4 w-4" />
      </button>
    </div>
  );
}

/**
 * Every photo of a medicine, one at a time, with labelled Previous / Next
 * buttons and "2 of 3", and a full-screen view with zoom to read the box.
 */
export function MedPhotoGallery({ photos, alt, ar }: { photos: string[]; alt: string; ar: boolean }) {
  const [i, setI] = useState(0);
  const [full, setFull] = useState(false);
  const [zoom, setZoom] = useState(1);
  const n = photos.length;
  const at = Math.min(i, n - 1);
  const show = (k: number) => {
    setI(k);
    setZoom(1);
  };
  // Zooming in keeps the middle of the photo in view (not its edge).
  const areaRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = areaRef.current;
    if (!el || zoom <= 1) return;
    const centre = () => {
      el.scrollTop = (el.scrollHeight - el.clientHeight) / 2;
      // RTL pages scroll with negative values; the middle is the same distance.
      const span = el.scrollWidth - el.clientWidth;
      el.scrollLeft = getComputedStyle(el).direction === "rtl" ? -span / 2 : span / 2;
    };
    requestAnimationFrame(centre);
  }, [zoom, at]);
  const close = () => {
    setFull(false);
    setZoom(1);
  };

  return (
    <div data-testid="med-photo-gallery">
      <button
        type="button"
        onClick={() => setFull(true)}
        aria-label={ar ? "اعرض الصورة بملء الشاشة" : "Show the photo full screen"}
        className="mx-auto mb-3 flex h-64 w-full items-center justify-center rounded-2xl border border-slate-100 bg-slate-50 p-2 shadow-inner dark:border-slate-700 dark:bg-slate-800/80"
      >
        <img
          src={photos[at]}
          alt={n > 1 ? `${alt} (${at + 1}/${n})` : alt}
          className="h-full w-full object-contain drop-shadow-md"
          onError={(e) => {
            (e.currentTarget as HTMLImageElement).src = FALLBACK;
          }}
        />
      </button>
      {n > 1 && (
        <div className="mb-3">
          <PrevNext at={at} n={n} setI={show} ar={ar} />
        </div>
      )}
      <button
        type="button"
        onClick={() => setFull(true)}
        className="mx-auto mb-3 flex min-h-12 items-center gap-2 rounded-xl border border-emerald-300 bg-emerald-50 px-4 text-sm font-bold text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/50 dark:text-emerald-300"
      >
        <Maximize2 className="h-4 w-4" />
        {ar ? "ملء الشاشة" : "Full screen"}
      </button>

      {/* On <body>: inside the animated pop-up, "fixed" would not cover the screen. */}
      {full && createPortal(
        <div
          role="dialog"
          aria-modal="true"
          aria-label={alt}
          data-testid="med-photo-fullscreen"
          className="fixed inset-0 z-[60] flex flex-col bg-black"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex items-center justify-between gap-2 p-3">
            <p className="min-w-0 truncate text-base font-bold text-white">{alt}</p>
            <button type="button" onClick={close} className="flex min-h-12 shrink-0 items-center gap-1.5 rounded-xl bg-white px-4 text-sm font-black text-slate-900">
              <X className="h-5 w-5" />
              {ar ? "إغلاق" : "Close"}
            </button>
          </div>
          {/* Zoomed in, the photo scrolls: move around it with a finger. */}
          <div ref={areaRef} className={`min-h-0 flex-1 overflow-auto ${zoom > 1 ? "" : "flex items-center"}`} data-testid="med-photo-zoom-area">
            <img
              src={photos[at]}
              alt={n > 1 ? `${alt} (${at + 1}/${n})` : alt}
              style={zoom > 1 ? { width: `${zoom * 100}%`, maxWidth: "none" } : undefined}
              className={zoom > 1 ? "h-auto" : "h-full w-full object-contain"}
              onError={(e) => {
                (e.currentTarget as HTMLImageElement).src = FALLBACK;
              }}
            />
          </div>
          <div className="space-y-2 p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
            <div className="flex items-center justify-center gap-2">
              <button
                type="button"
                onClick={() => setZoom((z) => Math.max(1, z - 1))}
                disabled={zoom <= 1}
                className="flex min-h-12 items-center gap-1 rounded-xl bg-white/15 px-4 text-sm font-bold text-white disabled:opacity-40"
              >
                <Minus className="h-4 w-4" />
                {ar ? "تصغير" : "Zoom out"}
              </button>
              <button
                type="button"
                onClick={() => setZoom((z) => Math.min(4, z + 1))}
                disabled={zoom >= 4}
                className="flex min-h-12 items-center gap-1 rounded-xl bg-white/15 px-4 text-sm font-bold text-white disabled:opacity-40"
              >
                <Plus className="h-4 w-4" />
                {ar ? "تكبير" : "Zoom in"}
              </button>
            </div>
            {n > 1 && <PrevNext at={at} n={n} setI={show} ar={ar} dark />}
          </div>
        </div>,
        document.body,
      )}
    </div>
  );
}
