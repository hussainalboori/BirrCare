"use client";

import React, { useState, useEffect } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { Caregiver, CaregiverRole } from "@/lib/types";
import { ROLE_METAS, isSuperAdmin } from "@/lib/permissions";
import { fetchApi } from "@/lib/api";
import {
  X,
  UserPlus,
  ShieldAlert,
  KeyRound,
  Edit2,
  Check,
  UserX,
  UserCheck,
  Loader2,
  AlertCircle,
  Eye,
  Heart,
  Pill,
} from "lucide-react";

interface UserManagementModalProps {
  isOpen: boolean;
  onClose: () => void;
}

const PRESET_COLORS = [
  "#d97706", // Amber
  "#2563eb", // Blue
  "#059669", // Emerald
  "#7c3aed", // Purple
  "#e11d48", // Rose
  "#0d9488", // Teal
  "#4f46e5", // Indigo
  "#ea580c", // Orange
];

export function UserManagementModal({ isOpen, onClose }: UserManagementModalProps) {
  const { currentCaregiver, refreshCaregivers } = useCaregiver();
  const { locale } = useLanguage();

  const [users, setUsers] = useState<Caregiver[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Form states
  const [showAddForm, setShowAddForm] = useState(false);
  const [editingUser, setEditingUser] = useState<Caregiver | null>(null);
  const [resetPinUserId, setResetPinUserId] = useState<string | null>(null);
  const [newPin, setNewPin] = useState("");
  const [pinError, setPinError] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  // New/Edit user form values
  const [formName, setFormName] = useState("");
  const [formEmail, setFormEmail] = useState("");
  const [formPhone, setFormPhone] = useState("");
  const [formRole, setFormRole] = useState<CaregiverRole>("caregiver");
  const [formPin, setFormPin] = useState("1234");
  const [formColor, setFormColor] = useState("#059669");

  const loadUsers = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchApi<Caregiver[]>("/users");
      setUsers(data || []);
    } catch (err: any) {
      setError(err.message || "Failed to load users");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (isOpen && isSuperAdmin(currentCaregiver?.role)) {
      loadUsers();
    }
  }, [isOpen, currentCaregiver]);

  if (!isOpen) return null;

  const handleOpenAdd = () => {
    setFormName("");
    setFormEmail("");
    setFormPhone("");
    setFormRole("caregiver");
    setFormPin("1234");
    setFormColor(PRESET_COLORS[Math.floor(Math.random() * PRESET_COLORS.length)]);
    setEditingUser(null);
    setShowAddForm(true);
  };

  const handleOpenEdit = (user: Caregiver) => {
    setEditingUser(user);
    setFormName(user.name);
    setFormEmail(user.email || "");
    setFormPhone(user.phone || "");
    setFormRole(user.role);
    setFormColor(user.avatar_color || "#2563eb");
    setShowAddForm(true);
  };

  const handleSaveUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formName.trim()) return;

    setIsSubmitting(true);
    try {
      if (editingUser) {
        // Update user
        await fetchApi(`/users/${editingUser.id}`, {
          method: "PUT",
          body: JSON.stringify({
            name: formName.trim(),
            email: formEmail.trim() || undefined,
            phone: formPhone.trim() || undefined,
            role: formRole,
            avatar_color: formColor,
          }),
        });
      } else {
        // Create user
        if (formPin.length !== 4) {
          alert(locale === "ar" ? "يجب أن يتكون رمز PIN من 4 أرقام" : "PIN must be exactly 4 digits");
          setIsSubmitting(false);
          return;
        }

        await fetchApi("/users", {
          method: "POST",
          body: JSON.stringify({
            name: formName.trim(),
            email: formEmail.trim() || undefined,
            phone: formPhone.trim() || undefined,
            role: formRole,
            pin: formPin,
            avatar_color: formColor,
          }),
        });
      }

      setShowAddForm(false);
      setEditingUser(null);
      await loadUsers();
      await refreshCaregivers();
    } catch (err: any) {
      alert(err.message || "Operation failed");
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleResetPinSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (newPin.length !== 4 || !/^\d{4}$/.test(newPin)) {
      setPinError(locale === "ar" ? "يجب إدخال 4 أرقام" : "Enter 4 digits");
      return;
    }

    if (!resetPinUserId) return;

    setIsSubmitting(true);
    try {
      await fetchApi(`/users/${resetPinUserId}/reset-pin`, {
        method: "POST",
        body: JSON.stringify({ new_pin: newPin }),
      });
      setResetPinUserId(null);
      setNewPin("");
      alert(locale === "ar" ? "تم تغيير رمز PIN بنجاح" : "PIN reset successfully");
    } catch (err: any) {
      alert(err.message || "Failed to reset PIN");
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleToggleStatus = async (user: Caregiver) => {
    if (user.id === currentCaregiver?.id) {
      alert(locale === "ar" ? "لا يمكنك تعطيل حسابك النشط كـ Super Admin" : "Cannot deactivate yourself");
      return;
    }

    const confirmMsg = user.is_active
      ? locale === "ar"
        ? `هل تريد بالتأكيد تعطيل حساب ${user.name}؟`
        : `Deactivate ${user.name}?`
      : locale === "ar"
      ? `هل تريد إعادة تفعيل حساب ${user.name}؟`
      : `Re-activate ${user.name}?`;

    if (!confirm(confirmMsg)) return;

    try {
      await fetchApi(`/users/${user.id}`, { method: "DELETE" });
      await loadUsers();
      await refreshCaregivers();
    } catch (err: any) {
      alert(err.message || "Failed to toggle status");
    }
  };

  const getRoleIcon = (role: CaregiverRole) => {
    switch (role) {
      case "super_admin":
        return <ShieldAlert className="h-3.5 w-3.5 text-purple-600 dark:text-purple-400" />;
      case "admin":
        return <Pill className="h-3.5 w-3.5 text-amber-600 dark:text-amber-400" />;
      case "caregiver":
        return <Heart className="h-3.5 w-3.5 text-emerald-600 dark:text-emerald-400" />;
      case "viewer":
        return <Eye className="h-3.5 w-3.5 text-slate-500 dark:text-slate-400" />;
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 backdrop-blur-xs p-4 animate-in fade-in duration-200">
      <div role="dialog" aria-modal="true" className="relative w-full max-w-2xl max-h-[90dvh] flex flex-col rounded-3xl bg-white shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800 overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between p-5 border-b border-slate-100 dark:border-slate-800">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-2xl bg-purple-100 text-purple-700 dark:bg-purple-950/60 dark:text-purple-300">
              <ShieldAlert className="h-5 w-5" />
            </div>
            <div>
              <h3 className="text-base font-bold text-slate-900 dark:text-white">
                {locale === "ar" ? "إدارة المستخدمين والصلاحيات" : "User & Role Management"}
              </h3>
              <p className="text-xs text-slate-500 dark:text-slate-400">
                {locale === "ar"
                  ? "إدارة الحسابات، وتحديد الأدوار، وتعيين رموز المرور (Super Admin)"
                  : "Manage family profiles, assign access roles, and reset PINs"}
              </p>
            </div>
          </div>

          <button
            type="button"
            onClick={onClose}
            aria-label={locale === "ar" ? "إغلاق" : "Close"}
            className="flex min-h-12 min-w-12 shrink-0 items-center justify-center rounded-full p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Content Body */}
        <div className="flex-1 overflow-y-auto p-5">
          {error && (
            <div className="mb-4 rounded-2xl bg-rose-50 p-3 text-xs font-semibold text-rose-700 dark:bg-rose-950/40 dark:text-rose-300 border border-rose-200 dark:border-rose-900 flex items-center gap-2">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {/* Action Bar */}
          {!showAddForm && !resetPinUserId && (
            <div className="mb-4 flex items-center justify-between">
              <span className="text-xs font-bold text-slate-500 dark:text-slate-400">
                {locale === "ar" ? `المستخدمون المسجلون (${users.length})` : `Registered Users (${users.length})`}
              </span>
              <button
                onClick={handleOpenAdd}
                className="flex items-center gap-1.5 rounded-xl bg-purple-600 hover:bg-purple-700 px-3.5 py-2 text-xs font-bold text-white shadow-purple-500/20 shadow-sm transition-all active:scale-95"
              >
                <UserPlus className="h-4 w-4" />
                <span>{locale === "ar" ? "إضافة مستخدم جديد" : "Add New User"}</span>
              </button>
            </div>
          )}

          {/* ADD / EDIT USER FORM */}
          {showAddForm && (
            <form
              onSubmit={handleSaveUser}
              className="mb-6 rounded-2xl border border-purple-200 bg-purple-50/40 p-4 dark:border-purple-900/60 dark:bg-purple-950/20 animate-in fade-in duration-150"
            >
              <div className="flex items-center justify-between mb-3">
                <h4 className="text-xs font-bold text-purple-900 dark:text-purple-200 flex items-center gap-1.5">
                  <UserPlus className="h-4 w-4 text-purple-600" />
                  <span>
                    {editingUser
                      ? locale === "ar"
                        ? `تعديل حساب: ${editingUser.name}`
                        : `Edit Profile: ${editingUser.name}`
                      : locale === "ar"
                      ? "إضافة فرد جديد للعائلة"
                      : "Add New Family Member"}
                  </span>
                </h4>
                <button
                  type="button"
                  onClick={() => {
                    setShowAddForm(false);
                    setEditingUser(null);
                  }}
                  className="text-xs font-semibold text-slate-500 hover:text-slate-700"
                >
                  {locale === "ar" ? "إلغاء" : "Cancel"}
                </button>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-3">
                <div>
                  <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "الاسم الكامل *" : "Full Name *"}
                  </label>
                  <input
                    type="text"
                    required
                    placeholder={locale === "ar" ? "مثال: مريم (Maryam)" : "e.g. Maryam"}
                    value={formName}
                    onChange={(e) => setFormName(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-base font-bold text-slate-900 focus:border-purple-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>

                <div>
                  <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "البريد الإلكتروني (اختياري)" : "Email (Optional)"}
                  </label>
                  <input
                    type="email"
                    placeholder="name@family.local"
                    value={formEmail}
                    onChange={(e) => setFormEmail(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-base font-semibold text-slate-900 focus:border-purple-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
              </div>

              {/* ROLE SELECTION */}
              <div className="mb-3">
                <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                  {locale === "ar" ? "الدور والصلاحية *" : "Access Role *"}
                </label>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                  {(["super_admin", "admin", "caregiver", "viewer"] as CaregiverRole[]).map((rKey) => {
                    const meta = ROLE_METAS[rKey];
                    const isSelected = formRole === rKey;
                    return (
                      <button
                        type="button"
                        key={rKey}
                        onClick={() => setFormRole(rKey)}
                        className={`flex flex-col text-start p-2.5 rounded-xl border transition-all ${
                          isSelected
                            ? "border-purple-600 bg-purple-100/60 dark:bg-purple-950/60 ring-2 ring-purple-500/30"
                            : "border-slate-200 bg-white hover:border-slate-300 dark:border-slate-800 dark:bg-slate-800"
                        }`}
                      >
                        <div className="flex items-center justify-between mb-1">
                          <span className="flex items-center gap-1.5 text-xs font-bold text-slate-900 dark:text-white">
                            {getRoleIcon(rKey)}
                            <span>{locale === "ar" ? meta.labelAr : meta.labelEn}</span>
                          </span>
                          {isSelected && <Check className="h-3.5 w-3.5 text-purple-600" />}
                        </div>
                        <span className="text-[10px] text-slate-500 dark:text-slate-400 leading-tight">
                          {locale === "ar" ? meta.descAr : meta.descEn}
                        </span>
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* PIN INPUT (ONLY FOR NEW USER) */}
              {!editingUser && (
                <div className="mb-3">
                  <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "رمز المرور المبدئي (4 أرقام) *" : "Default 4-Digit PIN *"}
                  </label>
                  <input
                    type="password"
                    inputMode="numeric"
                    autoComplete="new-password"
                    maxLength={4}
                    pattern="\d{4}"
                    required
                    value={formPin}
                    onChange={(e) => setFormPin(e.target.value)}
                    className="w-32 rounded-xl border border-slate-200 bg-white px-3 py-2 text-center text-base font-black tracking-widest text-slate-900 focus:border-purple-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
              )}

              {/* AVATAR COLOR PICKER */}
              <div className="mb-4">
                <label className="block text-[11px] font-semibold text-slate-700 dark:text-slate-300 mb-1">
                  {locale === "ar" ? "لون الصورة الرمزية" : "Avatar Color"}
                </label>
                <div className="flex items-center gap-2">
                  {PRESET_COLORS.map((col) => (
                    <button
                      type="button"
                      key={col}
                      onClick={() => setFormColor(col)}
                      className={`h-7 w-7 rounded-full border-2 transition-transform ${
                        formColor === col ? "scale-115 border-slate-900 dark:border-white shadow-sm" : "border-transparent"
                      }`}
                      style={{ backgroundColor: col }}
                    />
                  ))}
                </div>
              </div>

              {/* SUBMIT BUTTON */}
              <div className="flex items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={() => {
                    setShowAddForm(false);
                    setEditingUser(null);
                  }}
                  className="rounded-xl px-4 py-2 text-xs font-semibold text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
                >
                  {locale === "ar" ? "إلغاء" : "Cancel"}
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="flex items-center gap-1.5 rounded-xl bg-purple-600 hover:bg-purple-700 px-4 py-2 text-xs font-bold text-white shadow-sm active:scale-95 disabled:opacity-50"
                >
                  {isSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
                  <span>{locale === "ar" ? "حفظ المستخدم" : "Save User"}</span>
                </button>
              </div>
            </form>
          )}

          {/* RESET PIN MODAL / DRAWER */}
          {resetPinUserId && (
            <form
              onSubmit={handleResetPinSubmit}
              className="mb-6 rounded-2xl border border-amber-200 bg-amber-50/60 p-4 dark:border-amber-900/60 dark:bg-amber-950/20 animate-in fade-in duration-150"
            >
              <h4 className="text-xs font-bold text-amber-900 dark:text-amber-200 mb-2 flex items-center gap-1.5">
                <KeyRound className="h-4 w-4 text-amber-600" />
                <span>
                  {locale === "ar"
                    ? `إعادة تعيين رمز PIN للمستخدم (${users.find((u) => u.id === resetPinUserId)?.name})`
                    : `Reset 4-Digit PIN for ${users.find((u) => u.id === resetPinUserId)?.name}`}
                </span>
              </h4>
              <p className="text-[11px] text-amber-700 dark:text-amber-300 mb-3">
                {locale === "ar"
                  ? "أدخل 4 أرقام جديدة. سيتمكن المستخدم من تسجيل الدخول بها فوراً."
                  : "Enter a new 4-digit numeric code. The user can log in with it immediately."}
              </p>

              <div className="flex items-center gap-3 mb-2">
                <input
                  type="password"
                  inputMode="numeric"
                  pattern="\d{4}"
                  autoComplete="new-password"
                  maxLength={4}
                  autoFocus
                  placeholder="••••"
                  value={newPin}
                  onChange={(e) => {
                    setNewPin(e.target.value);
                    setPinError("");
                  }}
                  className="w-32 rounded-xl border border-amber-300 bg-white px-3 py-2 text-center text-lg font-black tracking-widest text-slate-900 focus:border-amber-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                />
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="rounded-xl bg-amber-600 hover:bg-amber-700 px-4 py-2 text-xs font-bold text-white shadow-sm active:scale-95 disabled:opacity-50"
                >
                  {isSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : locale === "ar" ? "تعيين الرمز" : "Reset PIN"}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setResetPinUserId(null);
                    setNewPin("");
                  }}
                  className="text-xs font-semibold text-slate-500 hover:text-slate-700"
                >
                  {locale === "ar" ? "إلغاء" : "Cancel"}
                </button>
              </div>
              {pinError && <p className="text-[11px] font-semibold text-rose-600">{pinError}</p>}
            </form>
          )}

          {/* USERS LIST */}
          {loading ? (
            <div className="space-y-3">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-16 w-full animate-pulse rounded-2xl bg-slate-100 dark:bg-slate-800" />
              ))}
            </div>
          ) : users.length === 0 ? (
            <div className="text-center py-8 text-xs text-slate-500">
              {locale === "ar" ? "لا يوجد مستخدمون مسجلون" : "No users found"}
            </div>
          ) : (
            <div className="space-y-2.5">
              {users.map((user) => {
                const meta = ROLE_METAS[user.role] || ROLE_METAS.caregiver;
                const isCurrent = user.id === currentCaregiver?.id;

                return (
                  <div
                    key={user.id}
                    className={`flex items-center justify-between p-3.5 rounded-2xl border transition-all ${
                      !user.is_active
                        ? "opacity-60 bg-slate-50 border-slate-200 dark:bg-slate-900/40 dark:border-slate-800"
                        : "bg-white border-slate-200 hover:border-slate-300 dark:bg-slate-800/80 dark:border-slate-700 shadow-2xs"
                    }`}
                  >
                    {/* User Identity */}
                    <div className="flex items-center gap-3">
                      <div
                        className="flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl text-white font-bold text-sm shadow-xs"
                        style={{ backgroundColor: user.avatar_color || "#059669" }}
                      >
                        {user.name.slice(0, 1)}
                      </div>

                      <div>
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-bold text-slate-900 dark:text-white">
                            {user.name}
                          </span>
                          {isCurrent && (
                            <span className="text-[10px] font-semibold text-purple-600 bg-purple-50 dark:bg-purple-950/60 px-1.5 py-0.5 rounded-sm">
                              {locale === "ar" ? "أنت (الحالي)" : "You (Active)"}
                            </span>
                          )}
                        </div>

                        <div className="flex items-center gap-2 mt-0.5">
                          {/* Role Badge */}
                          <span
                            className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-[10px] font-bold border ${meta.badgeClass}`}
                          >
                            {getRoleIcon(user.role)}
                            <span>{locale === "ar" ? meta.labelAr : meta.labelEn}</span>
                          </span>

                          {!user.is_active && (
                            <span className="text-[10px] font-bold text-rose-500 bg-rose-50 dark:bg-rose-950/60 px-1.5 py-0.5 rounded-sm">
                              {locale === "ar" ? "معطل" : "Inactive"}
                            </span>
                          )}
                        </div>
                      </div>
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1.5">
                      {/* Reset PIN button */}
                      <button
                        onClick={() => {
                          setResetPinUserId(user.id);
                          setNewPin("");
                          setPinError("");
                        }}
                        title={locale === "ar" ? "إعادة تعيين رمز PIN" : "Reset 4-Digit PIN"}
                        aria-label={locale === "ar" ? "إعادة تعيين رمز PIN" : "Reset 4-Digit PIN"}
                        className="flex min-h-12 min-w-12 items-center justify-center rounded-xl border border-slate-200 p-2 text-slate-600 hover:bg-slate-50 hover:text-amber-600 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-700 transition-colors"
                      >
                        <KeyRound className="h-4 w-4" />
                      </button>

                      {/* Edit button */}
                      <button
                        onClick={() => handleOpenEdit(user)}
                        title={locale === "ar" ? "تعديل البيانات والدور" : "Edit Details & Role"}
                        aria-label={locale === "ar" ? "تعديل البيانات والدور" : "Edit Details & Role"}
                        className="flex min-h-12 min-w-12 items-center justify-center rounded-xl border border-slate-200 p-2 text-slate-600 hover:bg-slate-50 hover:text-purple-600 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-700 transition-colors"
                      >
                        <Edit2 className="h-4 w-4" />
                      </button>

                      {/* Deactivate / Activate button */}
                      {!isCurrent && (
                        <button
                          onClick={() => handleToggleStatus(user)}
                          title={
                            user.is_active
                              ? locale === "ar"
                                ? "تعطيل الحساب"
                                : "Deactivate Profile"
                              : locale === "ar"
                              ? "إعادة تفعيل الحساب"
                              : "Re-activate Profile"
                          }
                          aria-label={
                            user.is_active
                              ? locale === "ar"
                                ? "تعطيل الحساب"
                                : "Deactivate Profile"
                              : locale === "ar"
                              ? "إعادة تفعيل الحساب"
                              : "Re-activate Profile"
                          }
                          className={`flex min-h-12 min-w-12 items-center justify-center rounded-xl border p-2 transition-colors ${
                            user.is_active
                              ? "border-slate-200 text-slate-400 hover:border-rose-300 hover:text-rose-600 dark:border-slate-700 dark:hover:text-rose-400"
                              : "border-emerald-300 bg-emerald-50 text-emerald-600 hover:bg-emerald-100 dark:border-emerald-800 dark:bg-emerald-950/40"
                          }`}
                        >
                          {user.is_active ? <UserX className="h-4 w-4" /> : <UserCheck className="h-4 w-4" />}
                        </button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
