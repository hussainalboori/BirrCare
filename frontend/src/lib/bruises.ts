/**
 * Bruises and bleeding signs, for a patient on a blood thinner. The keys must
 * match the backend lists (backend/internal/handlers/bruises.go).
 */
export interface Choice {
  key: string;
  ar: string;
  en: string;
}

export const BODY_PARTS: Choice[] = [
  { key: "arm_left", ar: "الذراع اليسرى", en: "Left arm" },
  { key: "arm_right", ar: "الذراع اليمنى", en: "Right arm" },
  { key: "hand_left", ar: "اليد اليسرى", en: "Left hand" },
  { key: "hand_right", ar: "اليد اليمنى", en: "Right hand" },
  { key: "leg_left", ar: "الرجل اليسرى", en: "Left leg" },
  { key: "leg_right", ar: "الرجل اليمنى", en: "Right leg" },
  { key: "belly", ar: "البطن (مكان الإبر)", en: "Belly (injection area)" },
  { key: "chest", ar: "الصدر", en: "Chest" },
  { key: "back", ar: "الظهر", en: "Back" },
  { key: "face_head", ar: "الوجه أو الرأس", en: "Face or head" },
  { key: "other", ar: "مكان آخر", en: "Somewhere else" },
];

export const BRUISE_SIZES: Choice[] = [
  { key: "small", ar: "صغيرة (بحجم عملة)", en: "Small (coin size)" },
  { key: "medium", ar: "متوسطة (بحجم الكف)", en: "Medium (palm size)" },
  { key: "large", ar: "كبيرة (أكبر من الكف)", en: "Large (bigger than a palm)" },
];

export const BRUISE_CHANGES: Choice[] = [
  { key: "new", ar: "جديدة", en: "New" },
  { key: "same", ar: "كما هي", en: "Same as before" },
  { key: "bigger", ar: "تكبر", en: "Getting bigger" },
  { key: "smaller", ar: "تصغر / تختفي", en: "Getting smaller" },
];

export interface BleedingSign extends Choice {
  /** Emergency now (call the ambulance), rather than calling the doctor today. */
  emergency?: boolean;
}

export const BLEEDING_SIGNS: BleedingSign[] = [
  { key: "gums", ar: "نزيف من اللثة", en: "Bleeding gums" },
  { key: "nose", ar: "نزيف من الأنف (رعاف)", en: "Nosebleed" },
  { key: "urine", ar: "بول وردي أو أحمر", en: "Pink or red urine" },
  { key: "stool", ar: "براز أسود أو فيه دم", en: "Black or bloody stool", emergency: true },
  { key: "vomit", ar: "استفراغ دم أو مثل البن", en: "Vomiting blood or 'coffee grounds'", emergency: true },
  { key: "head_knock", ar: "سقطت أو ضربت رأسها", en: "A fall or a knock to the head", emergency: true },
  { key: "cut_wont_stop", ar: "جرح لا يتوقف نزيفه", en: "A cut that won't stop bleeding", emergency: true },
];

const label = (list: Choice[], key: string | undefined, ar: boolean) => {
  const c = list.find((x) => x.key === key);
  return c ? (ar ? c.ar : c.en) : key || "";
};

export const bodyPartLabel = (key: string | undefined, ar: boolean) => label(BODY_PARTS, key, ar);
export const sizeLabel = (key: string | undefined, ar: boolean) => label(BRUISE_SIZES, key, ar);
export const changeLabel = (key: string | undefined, ar: boolean) => label(BRUISE_CHANGES, key, ar);
export const bleedingLabel = (key: string, ar: boolean) => label(BLEEDING_SIGNS, key, ar);
export const isEmergencySign = (key: string) => !!BLEEDING_SIGNS.find((s) => s.key === key)?.emergency;

/** What to do; the app never says anything about the medicine itself. */
export function bleedingAdvice(signs: string[], ar: boolean): string {
  if (signs.some(isEmergencySign)) {
    return ar
      ? "تأخذ مميّعاً للدم: هذه علامة خطيرة. اتصل بالإسعاف الآن. لا توقف أي دواء ولا تعطِ جرعة إضافية بدون الطبيب."
      : "She takes a blood thinner: this is a serious sign. Call emergency now. Don't stop or add any medicine without the doctor.";
  }
  return ar
    ? "تأخذ مميّعاً للدم: اتصل بالطبيب اليوم وأخبره. إذا لم يتوقف النزيف خلال ١٠ دقائق من الضغط عليه، اتصل بالإسعاف."
    : "She takes a blood thinner: call the doctor today and tell them. If the bleeding doesn't stop after 10 minutes of pressure, call emergency.";
}
