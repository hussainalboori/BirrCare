import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import React from "react";
import { InsulinPlanModal } from "@/components/InsulinPlanModal";
import { translations } from "@/lib/i18n";

let currentLocale = "ar";

vi.mock("@/lib/api", () => ({
  fetchApi: vi.fn(async () => ({})),
}));

vi.mock("@/context/LanguageContext", () => ({
  useLanguage: () => ({
    locale: currentLocale,
    t: currentLocale === "ar" ? translations.ar : translations.en,
    toggleLocale: () => {},
  }),
}));

beforeEach(() => {
  currentLocale = "ar";
});
afterEach(cleanup);

describe("InsulinPlanModal scale rows", () => {
  it("allows editing units in all rows (including added rows) in Arabic", () => {
    currentLocale = "ar";
    render(<InsulinPlanModal plans={[]} onClose={() => {}} onSaved={() => {}} />);

    // Row 1 inputs
    const r1Min = screen.getByLabelText("row 1 min") as HTMLInputElement;
    const r1Max = screen.getByLabelText("row 1 max") as HTMLInputElement;
    const r1Units = screen.getByLabelText("row 1 units") as HTMLInputElement;

    expect(r1Min.disabled).toBe(false);
    expect(r1Max.disabled).toBe(false);
    expect(r1Units.disabled).toBe(false);

    // Set row 1 values
    fireEvent.change(r1Max, { target: { value: "10" } });
    fireEvent.change(r1Units, { target: { value: "2" } });
    expect(r1Units.value).toBe("2");

    // Add row
    const addRowBtn = screen.getByRole("button", { name: /^\s*(صف|Row)\s*$/ });
    fireEvent.click(addRowBtn);

    // Row 2 inputs
    const r2Min = screen.getByLabelText("row 2 min") as HTMLInputElement;
    const r2Max = screen.getByLabelText("row 2 max") as HTMLInputElement;
    const r2Units = screen.getByLabelText("row 2 units") as HTMLInputElement;

    // Row 2 min is automatically linked to Row 1 max (no gaps)
    expect(r2Min.value).toBe("10");
    expect(r2Min.disabled).toBe(true);

    // Row 2 units and max MUST be enabled and editable
    expect(r2Max.disabled).toBe(false);
    expect(r2Units.disabled).toBe(false);

    fireEvent.change(r2Units, { target: { value: "4" } });
    expect(r2Units.value).toBe("4");
  });

  it("grid row does not have dir=ltr so columns stay aligned with the headers in RTL", () => {
    currentLocale = "ar";
    render(<InsulinPlanModal plans={[]} onClose={() => {}} onSaved={() => {}} />);

    const r1Units = screen.getByLabelText("row 1 units");
    const rowContainer = r1Units.parentElement;

    // The container of the inputs must not force LTR, otherwise in RTL the grid flips columns
    expect(rowContainer?.getAttribute("dir")).toBeNull();
    // But the input itself should have dir=ltr for numeric typing
    expect(r1Units.getAttribute("dir")).toBe("ltr");
  });
});
