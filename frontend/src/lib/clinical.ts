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
    actionAr: "الضغط في الحدود المثالية لمريض الجلطة",
    actionEn: "Pressure is within ideal recovery target range",
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
    badgeClass: "bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30",
    labelAr: "مشوش / بطء استجابة",
    labelEn: "Confused / Disoriented",
    descAr: "صعوبة في تمييز الوقت أو الوجوه أو نطق الكلمات",
    descEn: "Difficulty orienting time, people or slurred speech",
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
    badgeClass: "bg-blue-500/15 text-blue-600 dark:text-blue-400 border border-blue-500/30",
    labelAr: "نعاس مفرط / خمول",
    labelEn: "Drowsy / Lethargic",
    descAr: "صعوبة في البقاء مستيقظاً خارج أوقات النوم",
    descEn: "Hard to awaken outside regular sleeping hours",
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
