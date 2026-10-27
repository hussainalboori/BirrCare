/**
 * Issue #61: a small offline list of common stroke, heart and diabetes medicines,
 * with brand names, to suggest the active ingredient (lowercase INN) and type.
 * Only a suggestion: the admin confirms it. No external drug database.
 */

export type DrugClass = "" | "antiplatelet" | "anticoagulant" | "insulin" | "antihypertensive" | "statin" | "antidiabetic" | "anticonvulsant" | "painkiller" | "other";

export const DRUG_CLASSES: { value: DrugClass; en: string; ar: string }[] = [
  { value: "", en: "Not set", ar: "غير محدد" },
  { value: "antiplatelet", en: "Antiplatelet (blood thinner)", ar: "مضاد للصفائح (مميّع)" },
  { value: "anticoagulant", en: "Anticoagulant (blood thinner)", ar: "مضاد للتخثر (مميّع)" },
  { value: "insulin", en: "Insulin", ar: "إنسولين" },
  { value: "antidiabetic", en: "Diabetes tablet", ar: "حبوب سكري" },
  { value: "antihypertensive", en: "Blood pressure", ar: "ضغط الدم" },
  { value: "statin", en: "Cholesterol (statin)", ar: "الكوليسترول" },
  { value: "anticonvulsant", en: "Seizures", ar: "التشنجات" },
  { value: "painkiller", en: "Pain / fever", ar: "مسكن / خافض حرارة" },
  { value: "other", en: "Other", ar: "أخرى" },
];

const LIST: { inn: string; cls: DrugClass; names: string[] }[] = [
  { inn: "clopidogrel", cls: "antiplatelet", names: ["plavix", "clopex", "kalbiclop"] },
  { inn: "aspirin", cls: "antiplatelet", names: ["aspocid", "jusprin", "ecotrin", "aspirin protect", "اسبرين", "أسبرين"] },
  { inn: "ticagrelor", cls: "antiplatelet", names: ["brilinta"] },
  { inn: "apixaban", cls: "anticoagulant", names: ["eliquis", "إليكويس"] },
  { inn: "rivaroxaban", cls: "anticoagulant", names: ["xarelto"] },
  { inn: "dabigatran", cls: "anticoagulant", names: ["pradaxa"] },
  { inn: "edoxaban", cls: "anticoagulant", names: ["lixiana", "savaysa"] },
  { inn: "warfarin", cls: "anticoagulant", names: ["coumadin", "marevan", "وارفارين"] },
  { inn: "enoxaparin", cls: "anticoagulant", names: ["clexane", "lovenox"] },
  { inn: "atorvastatin", cls: "statin", names: ["lipitor", "atorva"] },
  { inn: "rosuvastatin", cls: "statin", names: ["crestor"] },
  { inn: "simvastatin", cls: "statin", names: ["zocor"] },
  { inn: "amlodipine", cls: "antihypertensive", names: ["norvasc", "amlor"] },
  { inn: "losartan", cls: "antihypertensive", names: ["cozaar"] },
  { inn: "valsartan", cls: "antihypertensive", names: ["diovan"] },
  { inn: "candesartan", cls: "antihypertensive", names: ["atacand"] },
  { inn: "lisinopril", cls: "antihypertensive", names: ["zestril", "prinivil"] },
  { inn: "perindopril", cls: "antihypertensive", names: ["coversyl"] },
  { inn: "bisoprolol", cls: "antihypertensive", names: ["concor"] },
  { inn: "carvedilol", cls: "antihypertensive", names: ["dilatrend"] },
  { inn: "hydrochlorothiazide", cls: "antihypertensive", names: ["esidrex"] },
  { inn: "furosemide", cls: "antihypertensive", names: ["lasix"] },
  { inn: "spironolactone", cls: "antihypertensive", names: ["aldactone"] },
  { inn: "metformin", cls: "antidiabetic", names: ["glucophage", "glucophage xr"] },
  { inn: "gliclazide", cls: "antidiabetic", names: ["diamicron"] },
  { inn: "glimepiride", cls: "antidiabetic", names: ["amaryl"] },
  { inn: "sitagliptin", cls: "antidiabetic", names: ["januvia"] },
  { inn: "linagliptin", cls: "antidiabetic", names: ["trajenta"] },
  { inn: "empagliflozin", cls: "antidiabetic", names: ["jardiance"] },
  { inn: "dapagliflozin", cls: "antidiabetic", names: ["forxiga"] },
  { inn: "insulin glargine", cls: "insulin", names: ["lantus", "toujeo", "basaglar"] },
  { inn: "insulin degludec", cls: "insulin", names: ["tresiba"] },
  { inn: "insulin aspart", cls: "insulin", names: ["novorapid", "fiasp"] },
  { inn: "insulin lispro", cls: "insulin", names: ["humalog"] },
  { inn: "levetiracetam", cls: "anticonvulsant", names: ["keppra"] },
  { inn: "phenytoin", cls: "anticonvulsant", names: ["epanutin", "dilantin"] },
  { inn: "valproate", cls: "anticonvulsant", names: ["depakine", "depakote"] },
  { inn: "paracetamol", cls: "painkiller", names: ["panadol", "adol", "tylenol", "acetaminophen", "بنادول", "باراسيتامول"] },
  { inn: "omeprazole", cls: "other", names: ["losec", "omez"] },
  { inn: "pantoprazole", cls: "other", names: ["controloc", "pantozol"] },
  { inn: "esomeprazole", cls: "other", names: ["nexium"] },
  { inn: "citicoline", cls: "other", names: ["somazina", "cebroton"] },
];

/** Every known ingredient, for the input's suggestions. */
export const KNOWN_INGREDIENTS = LIST.map((x) => x.inn);

/** Guess the ingredient and type from a medicine name ("Plavix 75mg" → clopidogrel). */
export function suggestIngredient(...names: string[]): { inn: string; cls: DrugClass } | null {
  const text = names.join(" ").toLowerCase();
  if (!text.trim()) return null;
  for (const x of LIST) {
    for (const n of [x.inn, ...x.names]) {
      if (new RegExp(`(^|[^\\p{L}])${n.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}($|[^\\p{L}])`, "u").test(text)) return { inn: x.inn, cls: x.cls };
    }
  }
  return null;
}
