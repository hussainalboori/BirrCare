"use client";

import React, { useState } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";

const FALLBACK = "/meds/pill-default.svg";

/** The main photo first, then the others (box, back, the pill…), without repeats. */
export function medPhotoList(imageUrl?: string, photoUrls?: string[]): string[] {
  const list = [imageUrl, ...(photoUrls || [])].filter((u): u is string => !!u);
  const unique = [...new Set(list)];
  return unique.length ? unique : [FALLBACK];
}

/**
 * Every photo of a medicine, one at a time, with labelled Previous / Next
 * buttons and "2 of 3", so the family can check the box and the pill.
 */
export function MedPhotoGallery({ photos, alt, ar }: { photos: string[]; alt: string; ar: boolean }) {
  const [i, setI] = useState(0);
  const n = photos.length;
  const at = Math.min(i, n - 1);
  // In Arabic the "previous" arrow points right.
  const Prev = ar ? ChevronRight : ChevronLeft;
  const Next = ar ? ChevronLeft : ChevronRight;

  return (
    <div data-testid="med-photo-gallery">
      <div className="mx-auto mb-3 flex h-56 w-full max-w-xs items-center justify-center rounded-2xl border border-slate-100 bg-slate-50 p-3 shadow-inner dark:border-slate-700 dark:bg-slate-800/80">
        <img
          src={photos[at]}
          alt={n > 1 ? `${alt} (${at + 1}/${n})` : alt}
          className="h-full w-full object-contain drop-shadow-md"
          onError={(e) => {
            (e.currentTarget as HTMLImageElement).src = FALLBACK;
          }}
        />
      </div>
      {n > 1 && (
        <div className="mb-3 flex items-center justify-center gap-2">
          <button
            type="button"
            onClick={() => setI((at - 1 + n) % n)}
            className="flex min-h-12 items-center gap-1 rounded-xl border border-slate-200 px-3 text-sm font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200"
          >
            <Prev className="h-4 w-4" />
            {ar ? "السابقة" : "Previous"}
          </button>
          <span className="min-w-14 text-center text-sm font-bold text-slate-600 dark:text-slate-300" aria-live="polite">
            {ar ? `${at + 1} من ${n}` : `${at + 1} of ${n}`}
          </span>
          <button
            type="button"
            onClick={() => setI((at + 1) % n)}
            className="flex min-h-12 items-center gap-1 rounded-xl border border-slate-200 px-3 text-sm font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200"
          >
            {ar ? "التالية" : "Next"}
            <Next className="h-4 w-4" />
          </button>
        </div>
      )}
    </div>
  );
}
