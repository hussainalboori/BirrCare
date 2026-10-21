import { BPStatus, AlertnessLevel } from "./types";

export interface BPClassification {
  status: BPStatus;
  badgeClass: string;
  bgLightClass: string;
  labelAr: string;
  labelEn: string;
  actionAr: string;
  actionEn: string;
}

/** Low BP thresholds (issue #13): LOW when systolic < sys or diastolic < dia. The
 * doctor's values come from /settings/clinical (setBPLowThresholds at app start). */
let bpLow = { sys: 90, dia: 60 };
export function setBPLowThresholds(sys: number, dia: number) {
  if (Number.isFinite(sys) && Number.isFinite(dia)) bpLow = { sys, dia };
}
export function getBPLowThresholds() {
  return bpLow;
}

/**
 * Order: CRISIS, STAGE_2, LOW, ELEVATED, NORMAL, so a high systolic is reported as
 * high even with a low diastolic. Must match CalculateBPStatus in the Go backend;
 * both are tested against clinical/bp-cases.json.
 */
export function classifyBloodPressure(systolic: number, diastolic: number): BPClassification {
  if (systolic >= 180 || diastolic >= 120) {
    return {
      status: "CRISIS",
      badgeClass: "bg-rose-600 text-white animate-pulse shadow-rose-500/40 shadow-lg",
      bgLightClass: "bg-rose-500/10 border-rose-500/30 text-rose-700 dark:text-rose-400",
      labelAr: "أزمة ضغط دم — طوارئ فورية",
      labelEn: "Hypertensive Crisis — Emergency",
      actionAr: "اتصل بالإسعاف أو الطبيب المعالج فوراً",
      actionEn: "Contact emergency medical services or attending physician immediately",
    };
  }

  if (systolic >= 140 || diastolic >= 90) {
    return {
      status: "STAGE_2",
      // Orange per AGENTS.md §4.3 (distinct from the yellow "Elevated" badge)
      badgeClass: "bg-orange-600 text-white shadow-orange-500/30 shadow-md",
      bgLightClass: "bg-orange-500/10 border-orange-500/30 text-orange-700 dark:text-orange-400",
      labelAr: "ارتفاع ملحوظ (المرحلة الثانية)",
      labelEn: "Hypertension Stage 2",
      actionAr: "تأكد من إعطاء أدوية الضغط في موعدها واسترح 15 دقيقة وأعد القياس",
      actionEn: "Ensure antihypertensives are taken, rest 15 min and re-measure",
    };
  }

  if (systolic < bpLow.sys || diastolic < bpLow.dia) {
    return {
      status: "LOW",
      badgeClass: "bg-rose-700 text-white shadow-rose-500/30 shadow-md",
      bgLightClass: "bg-rose-500/10 border-rose-500/30 text-rose-800 dark:text-rose-300",
      labelAr: "ضغط منخفض",
      labelEn: "Low blood pressure",
      actionAr: "مدّدها على ظهرها، أعد القياس بعد 5 دقائق، واتصل بالطبيب",
      actionEn: "Lay her down, re-measure in 5 minutes, and call the doctor",
    };
  }

  if (systolic >= 120 || diastolic >= 80) {
    return {
      status: "ELEVATED",
      badgeClass: "bg-yellow-500 text-slate-900 shadow-yellow-500/20 shadow",
      bgLightClass: "bg-yellow-500/10 border-yellow-500/30 text-yellow-700 dark:text-yellow-400",
      labelAr: "مرتفع قليلاً",
      labelEn: "Elevated Blood Pressure",
      actionAr: "راقب القياس في الفترات القادمة",
      actionEn: "Continue regular monitoring over upcoming intervals",
    };
  }

  return {
    status: "NORMAL",
    badgeClass: "bg-emerald-600 text-white shadow-emerald-500/20 shadow",
    bgLightClass: "bg-emerald-500/10 border-emerald-500/30 text-emerald-700 dark:text-emerald-400",
    labelAr: "طبيعي ومستقر",
    labelEn: "Normal & Stable",
    actionAr: "الضغط ضمن الحدود الطبيعية",
    actionEn: "Blood pressure is in the normal range",
  };
}

export type BloodSugarTiming = "fasting" | "post_meal" | "random";

export interface SugarClassification {
  status: "LOW" | "NORMAL" | "ELEVATED" | "VERY_HIGH";
  badgeClass: string;
  bgLightClass: string;
  labelAr: string;
  labelEn: string;
  actionAr: string;
  actionEn: string;
}

export function classifyBloodSugar(sugar: number, timing: BloodSugarTiming = "random"): SugarClassification {
  if (sugar < 3.9) {
    return {
      status: "LOW",
      badgeClass: "bg-rose-600 text-white animate-pulse shadow-rose-500/40 shadow-lg",
      bgLightClass: "bg-rose-500/10 border-rose-500/30 text-rose-700 dark:text-rose-400",
      labelAr: "هبوط حاد في السكر (طوارئ)",
      labelEn: "Hypoglycemia Alert (Low)",
      actionAr: "أعطِ المريض عصير سكري فوراً وافحص مجدداً بعد 15 دقيقة",
      actionEn: "Administer fast-acting sugar/juice immediately, recheck in 15 min",
    };
  }

  const isFasting = timing === "fasting";
  const elevatedThreshold = isFasting ? 7.0 : 10.0;
  const normalThreshold = isFasting ? 5.6 : 7.8;

  if (sugar >= 13.9) {
    return {
      status: "VERY_HIGH",
      badgeClass: "bg-rose-600 text-white shadow-rose-500/30 shadow-md",
      bgLightClass: "bg-rose-500/10 border-rose-500/30 text-rose-700 dark:text-rose-400",
      labelAr: "ارتفاع سكر حاد",
      labelEn: "Severe Hyperglycemia",
      actionAr: "مستوى مرتفع جداً يضر الأوعية الدموية في الدماغ، راجع الطبيب",
      actionEn: "Critically high glucose, consult doctor or administer correctional insulin",
    };
  }

  if (sugar > elevatedThreshold) {
    return {
      status: "ELEVATED",
      badgeClass: "bg-amber-600 text-white shadow-amber-500/30 shadow-md",
      bgLightClass: "bg-amber-500/10 border-amber-500/30 text-amber-700 dark:text-amber-400",
      labelAr: "مرتفع",
      labelEn: "Elevated Glucose",
      actionAr: "تأكد من إعطاء أدوية السكر وشرب الماء الوافر",
      actionEn: "Ensure diabetes medication was administered with adequate hydration",
    };
  }

  if (sugar > normalThreshold) {
    return {
      status: "ELEVATED",
      badgeClass: "bg-yellow-500 text-slate-900 shadow-yellow-500/20 shadow",
      bgLightClass: "bg-yellow-500/10 border-yellow-500/30 text-yellow-700 dark:text-yellow-400",
      labelAr: "أعلى من المستهدف قليلاً",
      labelEn: "Slightly High",
      actionAr: "راقب القياس في الفترات القادمة",
      actionEn: "Continue monitoring over upcoming meals",
    };
  }

  return {
    status: "NORMAL",
    badgeClass: "bg-emerald-600 text-white shadow-emerald-500/20 shadow",
    bgLightClass: "bg-emerald-500/10 border-emerald-500/30 text-emerald-700 dark:text-emerald-400",
    labelAr: "طبيعي ومثالي",
    labelEn: "Normal Glucose",
    actionAr: "مستوى السكر في الحدود الآمنة للتعافي العصبي",
    actionEn: "Glucose within safe stroke recovery target range",
  };
}

export interface AlertnessMeta {
  level: AlertnessLevel;
  badgeClass: string;
  labelAr: string;
  labelEn: string;
  descAr: string;
  descEn: string;
}

export const ALERTNESS_METAS: Record<AlertnessLevel, AlertnessMeta> = {
  alert: {
    level: "alert",
    badgeClass: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30",
    labelAr: "يقظ ومدرك",
    labelEn: "Alert & Responsive",
    descAr: "يتواصل بالعينين ويستجيب للكلام بشكل طبيعي",
    descEn: "Good eye contact and normal verbal response",
  },
  confused: {
    level: "confused",
    badgeClass: "bg-orange-600 text-white border border-orange-700",
    labelAr: "مشوشة / بطيئة الاستجابة",
    labelEn: "Confused / slow to respond",
    descAr: "إذا كان هذا جديداً: افحص F.A.S.T. الآن واتصل بالطبيب",
    descEn: "If this is new: do the F.A.S.T. check now and call the doctor",
  },
  restless: {
    level: "restless",
    badgeClass: "bg-orange-500/15 text-orange-600 dark:text-orange-400 border border-orange-500/30",
    labelAr: "مضطرب / قلق",
    labelEn: "Restless / Agitated",
    descAr: "حركة مستمرة أو انزعاج غير مبرر",
    descEn: "Constant motor agitation or unexplained discomfort",
  },
  drowsy: {
    level: "drowsy",
    badgeClass: "bg-rose-700 text-white border border-rose-800",
    labelAr: "صعب إيقاظها",
    labelEn: "Hard to wake",
    descAr: "اتصل بالطبيب الآن، وبالإسعاف إذا كان هذا جديداً",
    descEn: "Call the doctor now, emergency if this is new",
  },
};

/** Below this (mmol/L) correction insulin is never given; the backend enforces the same. */
export const HYPO_THRESHOLD = 3.9;

/** The doctor's units for a reading, or null when it falls outside every row of the scale. */
export function lookupInsulinScale(scale: { min: number; max: number; units: number }[], sugar: number): number | null {
  const row = scale.find((r) => sugar >= r.min && sugar < r.max);
  return row ? row.units : null;
}

/**
 * Automatic minimum gap between two doses when the doctor gave none: ¾ of the
 * shortest gap between the medication's scheduled times (once daily → 18 h,
 * 08:00 + 20:00 → 9 h). Mirrors intervalHours() in the Go backend.
 */
export function autoIntervalHours(slots: string[], times: Record<string, string>): number {
  const mins = [...new Set(
    slots
      .map((s) => times[s])
      .filter(Boolean)
      .map((t) => {
        const [h, m] = t.split(":").map(Number);
        return h * 60 + m;
      })
      .filter((n) => !isNaN(n))
  )].sort((a, b) => a - b);
  let gap = 24 * 60;
  mins.forEach((v, i) => {
    const next = i === mins.length - 1 ? mins[0] + 24 * 60 : mins[i + 1];
    if (next - v > 0 && next - v < gap) gap = next - v;
  });
  return Math.round(((gap * 0.75) / 60) * 100) / 100;
}

// ── Oxygen and temperature (issue #22) ──────────────────────────────────────
// Must match CalculateSpO2Status / CalculateTempStatus in the Go backend; both are
// tested against clinical/vitals-bands.json. The doctor can move the thresholds
// (/settings/clinical); AlertCenter loads them.

export type SpO2Status = "NORMAL" | "LOW" | "CRITICAL";
export type TempStatus = "NORMAL" | "FEVER" | "LOW";

let oxyFever = { spo2Warn: 92, spo2Critical: 88, feverC: 38.0 };
export function setOxygenFeverThresholds(spo2Warn: number, spo2Critical: number, feverC: number) {
  if (spo2Critical > 0 && spo2Warn > spo2Critical && feverC > 0) oxyFever = { spo2Warn, spo2Critical, feverC };
}
export function getOxygenFeverThresholds() {
  return oxyFever;
}

export function classifySpO2(spo2: number): SpO2Status {
  if (spo2 <= oxyFever.spo2Critical) return "CRITICAL";
  if (spo2 <= oxyFever.spo2Warn) return "LOW";
  return "NORMAL";
}

export function classifyTemperature(c: number): TempStatus {
  if (c >= oxyFever.feverC) return "FEVER";
  if (c < 35.0) return "LOW";
  return "NORMAL";
}

/** Badge colours and words. Crisis red only for a critical SpO2. */
export const SPO2_META: Record<SpO2Status, { ar: string; en: string; adviceAr?: string; adviceEn?: string; badgeClass: string }> = {
  NORMAL: { ar: "طبيعي", en: "Normal", badgeClass: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300" },
  LOW: {
    ar: "منخفض",
    en: "Low",
    adviceAr: "أعد القياس بعد دقائق بإصبع دافئ، واتصل بالطبيب إن بقي منخفضاً.",
    adviceEn: "Re-check in a few minutes on a warm finger; call the doctor if it stays low.",
    badgeClass: "bg-orange-100 text-orange-800 dark:bg-orange-950 dark:text-orange-300",
  },
  CRITICAL: {
    ar: "منخفض جداً — اتصل بالإسعاف",
    en: "Very low — call emergency",
    adviceAr: "اجلسها مستقيمة، تأكد أن الجهاز على إصبع دافئ، واتصل بالإسعاف إن تأكد الرقم.",
    adviceEn: "Sit her upright, check the clip is on a warm finger, and call emergency if it is confirmed.",
    badgeClass: "bg-rose-600 text-white animate-pulse",
  },
};

export const TEMP_META: Record<TempStatus, { ar: string; en: string; adviceAr?: string; adviceEn?: string; badgeClass: string }> = {
  NORMAL: { ar: "طبيعية", en: "Normal", badgeClass: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300" },
  FEVER: {
    ar: "حمّى",
    en: "Fever",
    adviceAr: "اتصل بالطبيب، خاصة مع سعال أو صعوبة بلع أو أكسجين منخفض.",
    adviceEn: "Call the doctor, especially with a cough, trouble swallowing or low oxygen.",
    badgeClass: "bg-orange-100 text-orange-800 dark:bg-orange-950 dark:text-orange-300",
  },
  LOW: {
    ar: "منخفضة",
    en: "Low",
    adviceAr: "دفّئها وأعد القياس، واتصل بالطبيب إن بقيت منخفضة.",
    adviceEn: "Warm her up and re-check; call the doctor if it stays low.",
    badgeClass: "bg-orange-100 text-orange-800 dark:bg-orange-950 dark:text-orange-300",
  },
};
