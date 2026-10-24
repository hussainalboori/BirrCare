"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { canTakeActions, isViewer } from "@/lib/permissions";
import { firstName } from "@/lib/actor";
import { isNetworkError, newRequestId } from "@/lib/outbox";
import { formatShortDate, formatTime } from "@/lib/datetime";
import { uploadBruisePhoto } from "@/lib/photo";
import {
  BLEEDING_SIGNS,
  BODY_PARTS,
  BRUISE_CHANGES,
  BRUISE_SIZES,
  bleedingAdvice,
  bleedingLabel,
  bodyPartLabel,
  changeLabel,
  isEmergencySign,
  sizeLabel,
} from "@/lib/bruises";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import { CrisisActions } from "@/components/CrisisActions";
import type { BruiseLog, SSEEvent } from "@/lib/types";
import { AlertTriangle, Camera, Check, Droplet, Loader2, Save, X } from "lucide-react";

type Choice = { key: string; ar: string; en: string };

function ChoiceGrid({
  items,
  value,
  onPick,
  ar,
  cols,
  testId,
}: {
  items: Choice[];
  value: string;
  onPick: (k: string) => void;
  ar: boolean;
  cols: string;
  testId: string;
}) {
  return (
    <div className={`grid gap-2 ${cols}`} data-testid={testId}>
      {items.map((c) => {
        const on = value === c.key;
        return (
          <button
            type="button"
            key={c.key}
            aria-pressed={on}
            onClick={() => onPick(on ? "" : c.key)}
            className={`flex min-h-12 items-center justify-center gap-1.5 rounded-2xl border-2 px-2 text-center text-sm font-bold leading-tight transition-all active:scale-95 ${
              on
                ? "border-violet-600 bg-violet-600 text-white"
                : "border-slate-200 bg-slate-50 text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
            }`}
          >
            {on && <Check className="h-4 w-4 shrink-0" />}
            <span>{ar ? c.ar : c.en}</span>
          </button>
        );
      })}
    </div>
  );
}

/**
 * Bruises and bleeding signs, for a patient on a strong blood thinner: where
 * on the body, how big, whether it is growing, and a photo to compare with
 * later. Any bleeding sign alerts every sibling.
 */
export function BruiseCard() {
  const { locale } = useLanguage();
  const { currentCaregiver, openPinModal } = useCaregiver();
  const ar = locale === "ar";

  const [list, setList] = useState<BruiseLog[] | null>(null);
  const [bodyPart, setBodyPart] = useState("");
  const [size, setSize] = useState("");
  const [change, setChange] = useState("");
  const [signs, setSigns] = useState<string[]>([]);
  const [notes, setNotes] = useState("");
  const [photoUrl, setPhotoUrl] = useState("");
  const [photoBusy, setPhotoBusy] = useState(false);
  const [saving, setSaving] = useState(false);
  const inFlightRef = useRef(false);
  const requestIdRef = useRef<string | null>(null);
  const [error, setError] = useState("");
  const [savedSigns, setSavedSigns] = useState<string[] | null>(null);
  const [zoom, setZoom] = useState<string | null>(null);

  const load = useCallback(async () => {
    const res = await fetchApi<BruiseLog[]>("/bruises?days=30").catch(() => undefined);
    if (res !== undefined) setList(Array.isArray(res) ? res : []);
  }, []);

  useEffect(() => {
    if (!currentCaregiver) return;
    load();
    const onEvent = (e: Event) => {
      if ((e as CustomEvent<SSEEvent>).detail?.type === "BRUISE_LOGGED") load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    window.addEventListener(RESYNC_EVENT, load);
    return () => {
      window.removeEventListener("birrcare:sse_event", onEvent);
      window.removeEventListener(RESYNC_EVENT, load);
    };
  }, [currentCaregiver, load]);

  const toggleSign = (k: string) => setSigns((prev) => (prev.includes(k) ? prev.filter((x) => x !== k) : [...prev, k]));
  const viewer = isViewer(currentCaregiver?.role);

  const save = async () => {
    setError("");
    setSavedSigns(null);
    if (!bodyPart && signs.length === 0) {
      setError(ar ? "اختر مكان الكدمة، أو علامة نزيف" : "Choose where the bruise is, or a bleeding sign");
      return;
    }
    if (!currentCaregiver) {
      openPinModal();
      return;
    }
    if (!canTakeActions(currentCaregiver.role)) {
      setError(ar ? "حسابك للمشاهدة فقط" : "Your account is view-only");
      return;
    }
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    setSaving(true);
    // The same id on a retry, so a slow network can't record it twice.
    requestIdRef.current = requestIdRef.current || newRequestId();
    try {
      await fetchApi("/bruises", {
        method: "POST",
        body: JSON.stringify({
          body_part: bodyPart,
          size: bodyPart ? size : "",
          change: bodyPart ? change : "",
          bleeding_signs: signs,
          photo_url: photoUrl,
          notes: notes.trim(),
          client_request_id: requestIdRef.current,
        }),
      });
      setSavedSigns(signs);
      setBodyPart("");
      setSize("");
      setChange("");
      setSigns([]);
      setNotes("");
      setPhotoUrl("");
      requestIdRef.current = null;
      load();
    } catch (err) {
      setError(
        isNetworkError(err)
          ? ar ? "لا يوجد اتصال — لم يُحفظ. حاول مرة أخرى." : "No connection — not saved. Try again."
          : (err as Error).message || (ar ? "تعذر الحفظ" : "Could not save"),
      );
    } finally {
      inFlightRef.current = false;
      setSaving(false);
    }
  };

  const hasSigns = signs.length > 0;
  const emergency = signs.some(isEmergencySign);

  return (
    <section
      data-testid="bruise-card"
      className="mb-6 rounded-3xl border border-slate-200 bg-white p-4 shadow-sm sm:p-5 dark:border-slate-800 dark:bg-slate-900"
    >
      <h2 className="flex items-center gap-2 text-lg font-black text-slate-900 dark:text-white">
        <Droplet className="h-5 w-5 text-violet-600 dark:text-violet-400" />
        {ar ? "الكدمات والنزيف" : "Bruises & bleeding"}
      </h2>
      <p className="mb-4 mt-1 text-sm text-slate-600 dark:text-slate-300">
        {ar
          ? "مع مميّع الدم تظهر الكدمات بسهولة. سجّل كل كدمة مع صورة لنقارن حجمها لاحقاً."
          : "On a blood thinner, bruises come easily. Record each one with a photo so we can compare its size later."}
      </p>

      {!viewer && (
        <div className="mb-5 space-y-4">
          <div>
            <p className="mb-2 text-sm font-bold text-slate-800 dark:text-slate-200">{ar ? "١. أين الكدمة؟" : "1. Where is the bruise?"}</p>
            <ChoiceGrid items={BODY_PARTS} value={bodyPart} onPick={setBodyPart} ar={ar} cols="grid-cols-2 sm:grid-cols-3 lg:grid-cols-4" testId="bruise-body-parts" />
          </div>

          {bodyPart && (
            <>
              <div>
                <p className="mb-2 text-sm font-bold text-slate-800 dark:text-slate-200">{ar ? "٢. حجمها" : "2. Size"}</p>
                <ChoiceGrid items={BRUISE_SIZES} value={size} onPick={setSize} ar={ar} cols="grid-cols-1 sm:grid-cols-3" testId="bruise-sizes" />
              </div>
              <div>
                <p className="mb-2 text-sm font-bold text-slate-800 dark:text-slate-200">{ar ? "٣. هل هي جديدة؟" : "3. Is it new?"}</p>
                <ChoiceGrid items={BRUISE_CHANGES} value={change} onPick={setChange} ar={ar} cols="grid-cols-2 sm:grid-cols-4" testId="bruise-changes" />
              </div>
              <div>
                <p className="mb-2 text-sm font-bold text-slate-800 dark:text-slate-200">{ar ? "٤. صورة (مهمة للمقارنة)" : "4. Photo (helps to compare)"}</p>
                {photoUrl ? (
                  <div className="flex items-center gap-3">
                    <img src={photoUrl} alt={ar ? "صورة الكدمة" : "Bruise photo"} className="h-24 w-24 rounded-xl border border-slate-200 object-cover dark:border-slate-700" />
                    <button
                      type="button"
                      onClick={() => setPhotoUrl("")}
                      className="flex min-h-12 items-center gap-1.5 rounded-xl border border-slate-200 px-3 text-sm font-bold text-slate-700 dark:border-slate-700 dark:text-slate-200"
                    >
                      <X className="h-4 w-4" />
                      {ar ? "احذف الصورة" : "Remove photo"}
                    </button>
                  </div>
                ) : (
                  <label
                    data-testid="bruise-photo-button"
                    className={`flex min-h-14 w-full cursor-pointer items-center justify-center gap-2 rounded-2xl border-2 border-dashed border-violet-400 px-3 text-sm font-bold text-violet-900 hover:bg-violet-50 dark:text-violet-200 dark:hover:bg-violet-950/40 ${photoBusy ? "pointer-events-none opacity-60" : ""}`}
                  >
                    {photoBusy ? <Loader2 className="h-5 w-5 animate-spin" /> : <Camera className="h-5 w-5" />}
                    <span>{photoBusy ? (ar ? "جارٍ رفع الصورة…" : "Uploading photo…") : ar ? "صوّر الكدمة" : "Take a photo of the bruise"}</span>
                    <input
                      type="file"
                      accept="image/*"
                      capture="environment"
                      data-testid="bruise-photo-input"
                      className="sr-only"
                      onChange={async (e) => {
                        const file = e.target.files?.[0];
                        e.target.value = "";
                        if (!file) return;
                        setError("");
                        setPhotoBusy(true);
                        try {
                          setPhotoUrl(await uploadBruisePhoto(file));
                        } catch {
                          setError(ar ? "تعذر رفع الصورة، جرّب مرة أخرى" : "Could not upload the photo, try again");
                        } finally {
                          setPhotoBusy(false);
                        }
                      }}
                    />
                  </label>
                )}
              </div>
            </>
          )}

          <div className="rounded-2xl border border-rose-200 bg-rose-50/60 p-3 dark:border-rose-900/60 dark:bg-rose-950/20">
            <p className="mb-2 flex items-center gap-1.5 text-sm font-bold text-rose-800 dark:text-rose-200">
              <AlertTriangle className="h-4 w-4" />
              {ar ? "علامات نزيف؟ اختر إن وُجدت" : "Any bleeding signs? Tick if so"}
            </p>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2" data-testid="bleeding-signs">
              {BLEEDING_SIGNS.map((s) => (
                <label
                  key={s.key}
                  className="flex min-h-12 cursor-pointer items-center gap-2 rounded-xl border border-rose-200 bg-white px-3 dark:border-rose-900/60 dark:bg-slate-900"
                >
                  <input
                    type="checkbox"
                    checked={signs.includes(s.key)}
                    onChange={() => toggleSign(s.key)}
                    className="h-5 w-5 shrink-0 rounded-sm text-rose-600 focus:ring-rose-500"
                  />
                  <span className="text-sm font-bold text-slate-800 dark:text-slate-200">{ar ? s.ar : s.en}</span>
                </label>
              ))}
            </div>
            {hasSigns && (
              <div role="alert" className={`mt-3 rounded-xl p-3 text-sm font-bold ${emergency ? "bg-rose-600 text-white" : "bg-amber-100 text-amber-900 dark:bg-amber-950/60 dark:text-amber-200"}`}>
                {bleedingAdvice(signs, ar)}
                <p className="mt-1 font-semibold">{ar ? "سيصل تنبيه لكل الإخوة عند الحفظ." : "Every sibling gets an alert when you save."}</p>
              </div>
            )}
            {hasSigns && (
              <div className="mt-3">
                <CrisisActions compact />
              </div>
            )}
          </div>

          <textarea
            rows={2}
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            placeholder={ar ? "ملاحظة (اختياري) — مثال: ضربت يدها في الباب" : "Note (optional) — e.g. bumped her hand on the door"}
            aria-label={ar ? "ملاحظة" : "Note"}
            className="w-full rounded-2xl border border-slate-200 bg-slate-50 p-3 text-base text-slate-900 focus:border-violet-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white dark:focus:bg-slate-900"
          />

          {error && (
            <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm font-bold text-rose-800 dark:bg-rose-950/40 dark:text-rose-200">
              {error}
            </p>
          )}
          {savedSigns && (
            <p role="status" className="rounded-xl bg-emerald-50 p-3 text-sm font-bold text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200">
              {savedSigns.length > 0
                ? ar ? "حُفظ ووصل تنبيه لكل الإخوة." : "Saved, and every sibling was alerted."
                : ar ? "حُفظت الكدمة." : "Bruise saved."}
            </p>
          )}

          <button
            type="button"
            onClick={save}
            disabled={saving || photoBusy}
            aria-busy={saving}
            className="flex min-h-14 w-full items-center justify-center gap-2 rounded-2xl bg-violet-600 px-5 text-base font-black text-white shadow-sm transition-all hover:bg-violet-700 active:scale-95 disabled:opacity-50"
          >
            {saving ? <Loader2 className="h-5 w-5 animate-spin" /> : <Save className="h-5 w-5" />}
            <span>
              {saving
                ? ar ? "جارٍ الحفظ…" : "Saving…"
                : currentCaregiver
                  ? ar ? `احفظ الكدمة / النزيف باسم ${firstName(currentCaregiver.name)}` : `Save bruise / bleeding as ${firstName(currentCaregiver.name)}`
                  : ar ? "احفظ الكدمة / النزيف" : "Save bruise / bleeding"}
            </span>
          </button>
        </div>
      )}

      <h3 className="mb-2 text-sm font-black text-slate-900 dark:text-white">{ar ? "آخر ٣٠ يوماً" : "Last 30 days"}</h3>
      {list === null ? (
        <div className="h-16 animate-pulse rounded-2xl bg-slate-200 dark:bg-slate-800" />
      ) : list.length === 0 ? (
        <p className="rounded-2xl border border-dashed border-slate-300 p-4 text-center text-sm font-semibold text-slate-600 dark:border-slate-700 dark:text-slate-300">
          {ar ? "لا كدمات ولا نزيف مسجّل" : "No bruises or bleeding recorded"}
        </p>
      ) : (
        <ol className="space-y-2" data-testid="bruise-list">
          {list.map((b) => {
            const bleeding = b.bleeding_signs.length > 0;
            return (
              <li
                key={b.id}
                data-testid="bruise-item"
                className={`flex gap-3 rounded-2xl border p-3 ${
                  bleeding ? "border-rose-300 bg-rose-50 dark:border-rose-900 dark:bg-rose-950/40" : "border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900"
                }`}
              >
                {b.photo_url && (
                  <button type="button" onClick={() => setZoom(b.photo_url!)} className="shrink-0" aria-label={ar ? "كبّر الصورة" : "Open the photo"}>
                    <img src={b.photo_url} alt="" className="h-20 w-20 rounded-xl object-cover" />
                  </button>
                )}
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-black text-slate-900 dark:text-white">
                    {b.body_part ? bodyPartLabel(b.body_part, ar) : ar ? "علامة نزيف" : "Bleeding sign"}
                    {b.size && <span className="font-semibold text-slate-600 dark:text-slate-300"> · {sizeLabel(b.size, ar)}</span>}
                    {b.change && <span className="font-semibold text-slate-600 dark:text-slate-300"> · {changeLabel(b.change, ar)}</span>}
                  </p>
                  {bleeding && (
                    <div className="mt-1 flex flex-wrap gap-1.5">
                      {b.bleeding_signs.map((s) => (
                        <span key={s} className="rounded-full bg-rose-600 px-2.5 py-0.5 text-xs font-bold text-white">
                          {bleedingLabel(s, ar)}
                        </span>
                      ))}
                    </div>
                  )}
                  {b.notes && <p className="mt-1 whitespace-pre-wrap text-sm text-slate-800 dark:text-slate-200">{b.notes}</p>}
                  <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                    <bdi>{firstName(b.caregiver_name)}</bdi> · <bdi>{formatShortDate(b.created_at, locale)}</bdi> • <bdi>{formatTime(b.created_at, locale)}</bdi>
                  </p>
                </div>
              </li>
            );
          })}
        </ol>
      )}

      {zoom && (
        <div
          role="dialog"
          aria-modal="true"
          aria-label={ar ? "صورة الكدمة" : "Bruise photo"}
          className="fixed inset-0 z-50 flex flex-col items-center justify-center gap-3 bg-black/85 p-4"
          onClick={() => setZoom(null)}
        >
          <img src={zoom} alt={ar ? "صورة الكدمة" : "Bruise photo"} className="max-h-[80vh] max-w-full rounded-2xl object-contain" />
          <button type="button" onClick={() => setZoom(null)} className="flex min-h-12 items-center gap-2 rounded-2xl bg-white px-5 text-base font-bold text-slate-900">
            <X className="h-5 w-5" />
            {ar ? "إغلاق" : "Close"}
          </button>
        </div>
      )}
    </section>
  );
}
