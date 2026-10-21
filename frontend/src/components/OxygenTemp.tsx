"use client";

import React from "react";
import { classifySpO2, classifyTemperature, SPO2_META, TEMP_META } from "@/lib/clinical";
import { CrisisActions } from "./CrisisActions";
import { Ltr } from "./Ltr";
import { Wind, Thermometer } from "lucide-react";

// Issue #22: pulse oximeter (SpO2 + pulse) and temperature fields, with their
// meaning shown while typing, and the badges for the readings list.

const input =
  "w-full min-h-14 rounded-xl border border-slate-200 bg-white px-3 font-black tabular-nums text-slate-900 focus:border-sky-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white";

export function SpO2Fields({
  ar,
  spo2,
  setSpo2,
  pulse,
  setPulse,
  withPulse,
  spo2Ref,
}: {
  ar: boolean;
  spo2: string;
  setSpo2: (v: string) => void;
  pulse: string;
  setPulse: (v: string) => void;
  withPulse: boolean;
  spo2Ref?: React.Ref<HTMLInputElement>;
}) {
  const n = Number(spo2);
  const status = spo2.trim() !== "" && Number.isInteger(n) && n >= 50 && n <= 100 ? classifySpO2(n) : null;
  const meta = status ? SPO2_META[status] : null;
  return (
    <div className="rounded-2xl border border-sky-100 bg-sky-50/30 p-4 dark:border-sky-950/60 dark:bg-sky-950/15">
      <p className="mb-2 flex items-center gap-1.5 text-xs font-bold text-slate-800 dark:text-slate-200">
        <Wind className="h-3.5 w-3.5 text-sky-600" />
        {ar ? "جهاز الأصبع (نسبة الأكسجين)" : "Pulse oximeter (oxygen)"}
      </p>
      <div className={`grid gap-3 ${withPulse ? "grid-cols-2" : "grid-cols-1"}`}>
        <label className="block">
          <span className="mb-1 block text-[11px] font-semibold text-slate-500">{ar ? "الأكسجين SpO₂ (%)" : "Oxygen SpO₂ (%)"}</span>
          <input
            ref={spo2Ref}
            type="number"
            inputMode="numeric"
            pattern="[0-9]*"
            min={50}
            max={100}
            dir="ltr"
            value={spo2}
            onChange={(e) => setSpo2(e.target.value)}
            placeholder="97"
            className={`${input} text-2xl`}
          />
        </label>
        {withPulse && (
          <label className="block">
            <span className="mb-1 block text-[11px] font-semibold text-slate-500">{ar ? "النبض (اختياري)" : "Pulse (optional)"}</span>
            <input
              type="number"
              inputMode="numeric"
              pattern="[0-9]*"
              min={30}
              max={220}
              dir="ltr"
              value={pulse}
              onChange={(e) => setPulse(e.target.value)}
              placeholder="80"
              className={`${input} text-2xl`}
            />
          </label>
        )}
      </div>
      {meta && status !== "NORMAL" && (
        <div data-testid="spo2-advice" className={`mt-3 space-y-2 rounded-xl p-3 ${status === "CRITICAL" ? "bg-rose-50 dark:bg-rose-950/60" : "bg-orange-50 dark:bg-orange-950/40"}`}>
          <p className="text-sm font-black text-slate-900 dark:text-white">
            <span className={`me-2 rounded-md px-2 py-0.5 text-xs font-bold ${meta.badgeClass}`}>{ar ? meta.ar : meta.en}</span>
            {ar ? meta.adviceAr : meta.adviceEn}
          </p>
          {status === "CRITICAL" && <CrisisActions compact />}
        </div>
      )}
    </div>
  );
}

export function TemperatureField({ ar, tempC, setTempC, tempRef }: { ar: boolean; tempC: string; setTempC: (v: string) => void; tempRef?: React.Ref<HTMLInputElement> }) {
  const n = Number(tempC);
  const status = tempC.trim() !== "" && n >= 30 && n <= 43 ? classifyTemperature(n) : null;
  const meta = status ? TEMP_META[status] : null;
  return (
    <div className="rounded-2xl border border-orange-100 bg-orange-50/30 p-4 dark:border-orange-950/60 dark:bg-orange-950/15">
      <label className="block">
        <span className="mb-2 flex items-center gap-1.5 text-xs font-bold text-slate-800 dark:text-slate-200">
          <Thermometer className="h-3.5 w-3.5 text-orange-600" />
          {ar ? "الحرارة (°م)" : "Temperature (°C)"}
        </span>
        <input
          ref={tempRef}
          type="number"
          inputMode="decimal"
          step="0.1"
          min={30}
          max={43}
          dir="ltr"
          value={tempC}
          onChange={(e) => setTempC(e.target.value)}
          placeholder="36.8"
          className={`${input} text-2xl`}
        />
      </label>
      {meta && status !== "NORMAL" && (
        <p data-testid="temp-advice" className="mt-3 rounded-xl bg-orange-50 p-3 text-sm font-black text-slate-900 dark:bg-orange-950/40 dark:text-white">
          <span className={`me-2 rounded-md px-2 py-0.5 text-xs font-bold ${meta.badgeClass}`}>{ar ? meta.ar : meta.en}</span>
          {ar ? meta.adviceAr : meta.adviceEn}
        </p>
      )}
    </div>
  );
}

/** List badges for a reading's oxygen and temperature. */
export function OxygenTempBadges({ ar, spo2, tempC }: { ar: boolean; spo2?: number; tempC?: number }) {
  if (spo2 == null && tempC == null) return null;
  const s = spo2 != null ? SPO2_META[classifySpO2(spo2)] : null;
  const t = tempC != null ? TEMP_META[classifyTemperature(tempC)] : null;
  return (
    <div className="my-2 flex flex-wrap items-center gap-2">
      {s && (
        <span className={`inline-flex items-center gap-1.5 rounded-xl px-3 py-1.5 text-sm font-black ${s.badgeClass}`} data-testid="spo2-badge">
          <Wind className="h-4 w-4" />
          <Ltr>SpO₂ {spo2}%</Ltr>
          <span className="font-bold">· {ar ? s.ar : s.en}</span>
        </span>
      )}
      {t && (
        <span className={`inline-flex items-center gap-1.5 rounded-xl px-3 py-1.5 text-sm font-black ${t.badgeClass}`} data-testid="temp-badge">
          <Thermometer className="h-4 w-4" />
          <Ltr>{tempC!.toFixed(1)} °C</Ltr>
          <span className="font-bold">· {ar ? t.ar : t.en}</span>
        </span>
      )}
    </div>
  );
}

