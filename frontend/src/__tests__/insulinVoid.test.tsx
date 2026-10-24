import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import React from "react";
import { InsulinPlanCard } from "@/components/InsulinPlanCard";
import { InsulinPlanStatus } from "@/lib/types";
import { translations } from "@/lib/i18n";

let currentLocale = "ar";
const mockFetchApi = vi.fn(async () => ({ status: "success" }));

vi.mock("@/lib/api", () => ({
  fetchApi: (...args: any[]) => (mockFetchApi as any)(...args),
}));

vi.mock("@/context/LanguageContext", () => ({
  useLanguage: () => ({
    locale: currentLocale,
    t: currentLocale === "ar" ? translations.ar : translations.en,
    toggleLocale: () => {},
  }),
}));

const mockCaregiver = {
  id: "cg_fatima",
  name: "فاطمة",
  role: "caregiver",
};

vi.mock("@/context/CaregiverContext", () => ({
  useCaregiver: () => ({
    currentCaregiver: mockCaregiver,
    openPinModal: vi.fn(),
  }),
}));

beforeEach(() => {
  currentLocale = "ar";
  mockFetchApi.mockClear();
});
afterEach(cleanup);

describe("InsulinPlanCard void/undo functionality", () => {
  const dummyStatus: InsulinPlanStatus = {
    plan: {
      id: "ins_novorapid",
      name_ar: "نوفورابيد",
      name_en: "NovoRapid",
      kind: "scale",
      scale: [{ min: 0, max: 20, units: 4 }],
      min_hours_between: 4,
      frequency_slots: ["morning"],
      is_active: true,
    },
    last_given: {
      id: "log_123",
      plan_id: "ins_novorapid",
      caregiver_id: "cg_fatima",
      caregiver_name: "فاطمة",
      given_at: new Date().toISOString(),
      status: "given",
      units: 4,
      scale_units: 4,
      blood_sugar: 12.0,
    },
    recent: [
      {
        id: "log_123",
        plan_id: "ins_novorapid",
        caregiver_id: "cg_fatima",
        caregiver_name: "فاطمة",
        given_at: new Date().toISOString(),
        status: "given",
        units: 4,
        scale_units: 4,
        blood_sugar: 12.0,
      },
    ],
  };

  it("shows undo button on recent active injection and calls void endpoint on confirmation", async () => {
    const onLogged = vi.fn();
    render(<InsulinPlanCard status={dummyStatus} onLogged={onLogged} />);

    // Last injection box has cancel button
    const cancelBtn = screen.getByText("إلغاء هذه الإبرة (تراجع)");
    expect(cancelBtn).toBeTruthy();

    // Click it to open in-page panel
    fireEvent.click(cancelBtn);

    // Cancel panel is now visible with reason chips
    expect(screen.getByText("إلغاء تسجيل الإبرة (يبقى موثقاً في سجل المراجعة):")).toBeTruthy();
    expect(screen.getByText("نقرة خطأ")).toBeTruthy();
    expect(screen.getByText("تأكيد الإلغاء")).toBeTruthy();

    // Select reason
    fireEvent.click(screen.getByText("نقرة خطأ"));

    // Confirm cancellation
    fireEvent.click(screen.getByText("تأكيد الإلغاء"));

    expect(mockFetchApi).toHaveBeenCalledWith(
      "/insulin/log/log_123/void",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ reason: "نقرة خطأ" }),
      })
    );
  });

  it("renders voided injections as cancelled in history", () => {
    const voidedStatus: InsulinPlanStatus = {
      ...dummyStatus,
      last_given: null,
      recent: [
        {
          id: "log_voided",
          plan_id: "ins_novorapid",
          caregiver_id: "cg_fatima",
          caregiver_name: "فاطمة",
          given_at: new Date().toISOString(),
          status: "given",
          units: 4,
          scale_units: 4,
          blood_sugar: 12.0,
          voided_at: new Date().toISOString(),
          void_reason: "نقرة خطأ",
        },
      ],
    };

    render(<InsulinPlanCard status={voidedStatus} onLogged={() => {}} />);

    expect(screen.getByText("ملغاة")).toBeTruthy();
    expect(screen.getByText(/سبب الإلغاء:/)).toBeTruthy();
    expect(screen.getByText("نقرة خطأ")).toBeTruthy();
  });
});
