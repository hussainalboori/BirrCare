import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import React from "react";
import { MedicationCatalogModal } from "@/components/MedicationCatalogModal";
import { fetchApi } from "@/lib/api";
import { suggestIngredient } from "@/lib/ingredients";

// Issue #61: a second medicine with the same active ingredient asks before saving,
// naming the one already on the list.
let currentLocale = "en";
const meds = [
  { id: "med_p", name_ar: "بلافكس", name_en: "Plavix", dosage: "75 mg", is_active: 1, frequency_slots: ["morning"], slot_times: { morning: "08:00" }, created_at: "2026-01-01 00:00:00" },
];
vi.mock("@/lib/api", () => ({ fetchApi: vi.fn() }));
vi.mock("@/context/LanguageContext", () => ({ useLanguage: () => ({ locale: currentLocale, toggleLocale: () => {} }) }));
vi.mock("@/context/CaregiverContext", () => ({ useCaregiver: () => ({ currentCaregiver: { id: "cg_admin", name: "Hussain", role: "admin" } }) }));

const api = fetchApi as unknown as ReturnType<typeof vi.fn>;
let puts: Record<string, unknown>[] = [];
beforeEach(() => {
  puts = [];
  api.mockImplementation(async (url: string, opts?: RequestInit) => {
    if (url === "/medications" && !opts) return meds;
    if (opts?.method === "PUT") {
      const body = JSON.parse(String(opts.body));
      puts.push(body);
      if (!body.confirm_duplicate) {
        const err = Object.assign(new Error("dup"), { status: 409, data: { error: "DUPLICATE_INGREDIENT", existing: { name_ar: "كلوبيدوقريل", name_en: "Clopidogrel", dosage: "75 mg" } } });
        throw err;
      }
      return { status: "success" };
    }
    return {};
  });
});
afterEach(cleanup);

async function editPlavix(label: string) {
  render(<MedicationCatalogModal isOpen onClose={() => {}} onUpdated={() => {}} />);
  await waitFor(() => expect(screen.getAllByRole("button", { name: label }).length).toBeGreaterThan(0));
  fireEvent.click(screen.getAllByRole("button", { name: label })[0]);
}

describe("duplicate active ingredient", () => {
  it("en: names the existing medicine and saves only with a reason", async () => {
    currentLocale = "en";
    await editPlavix("Edit Medication");
    fireEvent.click(screen.getByTestId("use-suggested-ingredient")); // Plavix → clopidogrel
    expect((screen.getByTestId("med-ingredient") as HTMLInputElement).value).toBe("clopidogrel");
    fireEvent.submit(screen.getByTestId("med-ingredient").closest("form")!);
    const warn = await screen.findByTestId("duplicate-ingredient");
    expect(warn.textContent).toContain("Same medicine as Clopidogrel 75 mg");
    expect(warn.textContent).toContain("could double the dose");

    fireEvent.click(screen.getByRole("button", { name: "Add it anyway" }));
    expect(await screen.findByText("Write why both are needed.")).toBeTruthy();
    expect(puts).toHaveLength(1);

    fireEvent.change(screen.getByLabelText("Reason"), { target: { value: "Doctor split the dose" } });
    fireEvent.click(screen.getByRole("button", { name: "Add it anyway" }));
    await waitFor(() => expect(puts).toHaveLength(2));
    expect(puts[1]).toMatchObject({ active_ingredient: "clopidogrel", drug_class: "antiplatelet", confirm_duplicate: true, duplicate_reason: "Doctor split the dose" });
  });

  it("ar: the warning is in Arabic and names the medicine in Arabic", async () => {
    currentLocale = "ar";
    await editPlavix("تعديل الدواء والجرعة");
    fireEvent.click(screen.getByTestId("use-suggested-ingredient"));
    fireEvent.submit(screen.getByTestId("med-ingredient").closest("form")!);
    const warn = await screen.findByTestId("duplicate-ingredient");
    expect(warn.textContent).toContain("نفس دواء كلوبيدوقريل");
    expect(warn.textContent).toContain("قد يضاعف الجرعة");
  });
});

describe("suggestIngredient", () => {
  it("knows brands and generics, in names with a dose", () => {
    expect(suggestIngredient("Plavix 75mg")).toEqual({ inn: "clopidogrel", cls: "antiplatelet" });
    expect(suggestIngredient("", "إليكويس")).toEqual({ inn: "apixaban", cls: "anticoagulant" });
    expect(suggestIngredient("Panadol Extra")).toEqual({ inn: "paracetamol", cls: "painkiller" });
    expect(suggestIngredient("Vitamin D")).toBeNull();
  });
});
