import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { normalizeArabicNumerals, parseNumberInput, isValidNumber } from "@/lib/numbers";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import React from "react";
import { InsulinPlanCard } from "@/components/InsulinPlanCard";
import { InsulinPlanStatus } from "@/lib/types";
import { translations } from "@/lib/i18n";

describe("normalizeArabicNumerals and parseNumberInput", () => {
  it("normalizes Western digits and commas", () => {
    expect(normalizeArabicNumerals("12.5")).toBe("12.5");
    expect(normalizeArabicNumerals("12,5")).toBe("12.5");
    expect(parseNumberInput("12.5")).toBe(12.5);
    expect(parseNumberInput("12,5")).toBe(12.5);
  });

  it("normalizes Eastern Arabic-Indic digits (٠-٩)", () => {
    expect(normalizeArabicNumerals("١٢.٥")).toBe("12.5");
    expect(normalizeArabicNumerals("٨")).toBe("8");
    expect(normalizeArabicNumerals("٥")).toBe("5");
    expect(parseNumberInput("١٢.٥")).toBe(12.5);
    expect(parseNumberInput("٨")).toBe(8);
  });

  it("normalizes Extended Arabic-Indic / Persian digits (۰-۹)", () => {
    expect(normalizeArabicNumerals("۸")).toBe("8");
    expect(normalizeArabicNumerals("۹")).toBe("9");
    expect(parseNumberInput("۸")).toBe(8);
    expect(parseNumberInput("۹")).toBe(9);
  });

  it("normalizes Arabic decimal separator (٫) and Arabic comma (،)", () => {
    expect(normalizeArabicNumerals("٦٫٥")).toBe("6.5");
    expect(normalizeArabicNumerals("٦،٥")).toBe("6.5");
    expect(parseNumberInput("٦٫٥")).toBe(6.5);
    expect(parseNumberInput("٦،٥")).toBe(6.5);
  });

  it("handles whitespace and invalid inputs safely", () => {
    expect(parseNumberInput("   ")).toBeNaN();
    expect(parseNumberInput("")).toBeNaN();
    expect(parseNumberInput(null)).toBeNaN();
    expect(parseNumberInput(undefined)).toBeNaN();
    expect(parseNumberInput("abc")).toBeNaN();
    expect(parseNumberInput(" ٨.٥ ")).toBe(8.5);
    expect(isValidNumber("٨.٥")).toBe(true);
    expect(isValidNumber("abc")).toBe(false);
  });
});

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
  id: "cg_ali",
  name: "علي",
  role: "caregiver",
};

vi.mock("@/context/CaregiverContext", () => ({
  useCaregiver: () => ({
    currentCaregiver: mockCaregiver,
    openPinModal: vi.fn(),
  }),
}));

describe("InsulinPlanCard blood sugar input with Arabic and English numerals", () => {
  beforeEach(() => {
    currentLocale = "ar";
    mockFetchApi.mockClear();
  });
  afterEach(cleanup);

  const statusWithScale: InsulinPlanStatus = {
    plan: {
      id: "ins_novorapid",
      name_ar: "نوفورابيد",
      name_en: "NovoRapid",
      kind: "scale",
      scale: [
        { min: 0, max: 4.9, units: 0 },
        { min: 5, max: 8, units: 10 },
        { min: 8, max: 45, units: 16 },
      ],
      min_hours_between: 4,
      frequency_slots: ["morning"],
      is_active: true,
    },
    last_given: null,
    recent: [],
  };

  it("shows the units trigger button when blood sugar is entered in Arabic numerals", () => {
    render(
      <InsulinPlanCard
        status={statusWithScale}
        onLogged={() => {}}
      />
    );

    const input = screen.getByPlaceholderText("0.0");
    // Initially no dose trigger button is visible because there's no sugar reading
    expect(screen.queryByText(/أعطيتها/)).toBeNull();

    // Type Arabic numeral ٨ (8)
    fireEvent.change(input, { target: { value: "٨" } });

    // The trigger button should now appear with 16 units!
    const triggerBtn = screen.getByRole("button", { name: /أعطيتها/ });
    expect(triggerBtn).toBeTruthy();
    expect(triggerBtn.textContent).toContain("16");
    expect(triggerBtn.textContent).toContain("وحدة");
  });

  it("shows the units trigger button when blood sugar is entered with Arabic decimal separator (٫)", () => {
    render(
      <InsulinPlanCard
        status={statusWithScale}
        onLogged={() => {}}
      />
    );

    const input = screen.getByPlaceholderText("0.0");

    // Type ٦٫٥ (6.5 in Arabic digits with Arabic decimal separator)
    fireEvent.change(input, { target: { value: "٦٫٥" } });

    const triggerBtn = screen.getByRole("button", { name: /أعطيتها/ });
    expect(triggerBtn).toBeTruthy();
    expect(triggerBtn.textContent).toContain("10");
  });

  it("shows the units trigger button in English when blood sugar is entered in Arabic or English numerals", () => {
    currentLocale = "en";
    render(
      <InsulinPlanCard
        status={statusWithScale}
        onLogged={() => {}}
      />
    );

    const input = screen.getByPlaceholderText("0.0");

    // Type Arabic numeral 8 (Extended Arabic ۸)
    fireEvent.change(input, { target: { value: "۸" } });

    const triggerBtn = screen.getByRole("button", { name: /Gave/ });
    expect(triggerBtn).toBeTruthy();
    expect(triggerBtn.textContent).toContain("16");
    expect(triggerBtn.textContent).toContain("units");
  });
});
