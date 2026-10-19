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
      badgeClass: "bg-amber-600 text-white shadow-amber-500/30 shadow-md",
      bgLightClass: "bg-amber-500/10 border-amber-500/30 text-amber-700 dark:text-amber-400",
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
