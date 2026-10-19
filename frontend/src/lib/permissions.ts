import { CaregiverRole } from "./types";

export function isSuperAdmin(role?: CaregiverRole | string): boolean {
  return role === "super_admin";
}

export function canManageShifts(role?: CaregiverRole | string): boolean {
  return role === "super_admin";
}

export function canManageMedications(role?: CaregiverRole | string): boolean {
  return role === "super_admin" || role === "admin";
}

export function canTakeActions(role?: CaregiverRole | string): boolean {
  return role === "super_admin" || role === "admin" || role === "caregiver";
}

export function isViewer(role?: CaregiverRole | string): boolean {
  return role === "viewer";
}

export interface RoleMeta {
  role: CaregiverRole;
  labelAr: string;
  labelEn: string;
  descAr: string;
  descEn: string;
  badgeClass: string;
  dotClass: string;
}

export const ROLE_METAS: Record<CaregiverRole, RoleMeta> = {
  super_admin: {
    role: "super_admin",
    labelAr: "مشرف عام",
    labelEn: "Super Admin",
    descAr: "إدارة المستخدمين والأدوية، وتسجيل كافة العمليات الحيوية",
    descEn: "Full access to manage users, prescriptions, and log clinical actions",
    badgeClass: "bg-purple-100 text-purple-800 border-purple-300 dark:bg-purple-950/60 dark:text-purple-300 dark:border-purple-800",
    dotClass: "bg-purple-500",
  },
  admin: {
    role: "admin",
    labelAr: "مدير أدوية",
    labelEn: "Admin",
    descAr: "إضافة وتعديل الأدوية والجرعات، وتسجيل العمليات الحيوية",
    descEn: "Can add/edit prescriptions and dosages, and log clinical actions",
    badgeClass: "bg-amber-100 text-amber-800 border-amber-300 dark:bg-amber-950/60 dark:text-amber-300 dark:border-amber-800",
    dotClass: "bg-amber-500",
  },
  caregiver: {
    role: "caregiver",
    labelAr: "مقدم رعاية",
    labelEn: "Caregiver",
    descAr: "تسجيل أخذ الجرعات، قراءات الضغط والسكر، وتسليم الورديات",
    descEn: "Log medication doses, vitals, and handover shift notes",
    badgeClass: "bg-emerald-100 text-emerald-800 border-emerald-300 dark:bg-emerald-950/60 dark:text-emerald-300 dark:border-emerald-800",
    dotClass: "bg-emerald-500",
  },
  viewer: {
    role: "viewer",
    labelAr: "مشاهد (قراءة فقط)",
    labelEn: "Viewer (Read Only)",
    descAr: "الاطلاع على الجداول والقراءات والتقارير بدون إمكانية التعديل",
    descEn: "Monitor schedule, vitals, and reports with read-only access",
    badgeClass: "bg-slate-100 text-slate-700 border-slate-300 dark:bg-slate-800 dark:text-slate-300 dark:border-slate-700",
    dotClass: "bg-slate-400",
  },
};
