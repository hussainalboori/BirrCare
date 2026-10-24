/**
 * Issue #18: quick-tap handover observations. The keys must match the backend
 * list (backend/internal/handlers/handover_tags.go); "urgent" tags mark the note
 * urgent on the server too.
 */
export interface HandoverTag {
  key: string;
  ar: string;
  en: string;
  urgent?: boolean;
  /** Kept for old notes' labels, but no longer offered as a chip. */
  hidden?: boolean;
}

export const HANDOVER_TAGS: HandoverTag[] = [
  { key: "ate_well", ar: "أكلت جيداً", en: "Ate well" },
  { key: "ate_half", ar: "أكلت النصف", en: "Ate half" },
  { key: "low_appetite", ar: "شهية ضعيفة", en: "Low appetite" },
  { key: "swallowing_difficulty", ar: "صعوبة في البلع", en: "Swallowing difficulty", urgent: true },
  { key: "needs_rest", ar: "تحتاج راحة", en: "Needs rest" },
  { key: "good_mood", ar: "مزاج جيد", en: "Good mood" },
  { key: "agitated", ar: "منفعلة", en: "Agitated" },
  { key: "pain", ar: "تشعر بألم", en: "Pain", urgent: true },
  { key: "slept_well", ar: "نامت جيداً", en: "Slept well" },
  { key: "poor_sleep", ar: "نوم سيئ", en: "Poor sleep" },
  { key: "bowel_movement", ar: "دخلت الحمام (براز)", en: "Bowel movement" },
  { key: "repositioned", ar: "غيّرنا وضعيتها", en: "Turned / repositioned", hidden: true },
];

const BY_KEY = new Map(HANDOVER_TAGS.map((t) => [t.key, t]));

/** The chips to offer (she walks and moves by herself: no repositioning). */
export const TAG_CHOICES = HANDOVER_TAGS.filter((t) => !t.hidden);

export function tagLabel(key: string, ar: boolean): string {
  const t = BY_KEY.get(key);
  return t ? (ar ? t.ar : t.en) : key;
}

export function isUrgentTag(key: string): boolean {
  return !!BY_KEY.get(key)?.urgent;
}
