import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import React from "react";
import { MedicationCatalogModal } from "@/components/MedicationCatalogModal";
import { fetchApi } from "@/lib/api";

let currentLocale = "ar";

const mockMeds = [
  {
    id: "med_1",
    name_ar: "أسبرين",
    name_en: "Aspirin",
    dosage: "100mg",
    instructions_ar: "بعد الإفطار",
    instructions_en: "After breakfast",
    is_active: 1,
    frequency_slots: ["morning"],
    slot_times: { morning: "08:00" },
    days_of_week: ["monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"],
    created_at: "2026-01-01 00:00:00",
  },
];

vi.mock("@/lib/api", () => ({
  fetchApi: vi.fn(async (url: string) => {
    if (url === "/medications") return mockMeds;
    return {};
  }),
}));

vi.mock("@/context/LanguageContext", () => ({
  useLanguage: () => ({
    locale: currentLocale,
    toggleLocale: () => {},
  }),
}));

vi.mock("@/context/CaregiverContext", () => ({
  useCaregiver: () => ({
    currentCaregiver: { id: "cg_admin", name: "Hussain", role: "super_admin" },
  }),
}));

beforeEach(() => {
  currentLocale = "ar";
  vi.clearAllMocks();
  (fetchApi as unknown as ReturnType<typeof vi.fn>).mockImplementation(async (url: string) => {
    if (url === "/medications") return mockMeds;
    return {};
  });
});
afterEach(cleanup);

describe("MedicationCatalogModal in-page delete confirmation", () => {
  it("opens in-page confirmation modal with PIN input when delete icon is clicked", async () => {
    render(<MedicationCatalogModal isOpen={true} onClose={() => {}} onUpdated={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText("أسبرين")).toBeTruthy();
    });

    const deleteBtn = screen.getByRole("button", { name: "تعطيل الدواء" });
    fireEvent.click(deleteBtn);

    // In-page modal must appear with title and PIN input
    expect(screen.getByText("تأكيد تعطيل الدواء")).toBeTruthy();
    const pinInput = screen.getByPlaceholderText("••••") as HTMLInputElement;
    expect(pinInput).toBeTruthy();

    // Clicking confirm without PIN shows validation message
    const confirmBtn = screen.getByRole("button", { name: "تعطيل الدواء" });
    fireEvent.click(confirmBtn);
    expect(screen.getByText("يرجى إدخال الرمز السري للمشرف للمتابعة")).toBeTruthy();

    // Entering PIN and confirming calls DELETE with header and body
    fireEvent.change(pinInput, { target: { value: "1234" } });
    fireEvent.click(confirmBtn);

    await waitFor(() => {
      expect(fetchApi).toHaveBeenCalledWith(
        "/medications/med_1",
        expect.objectContaining({
          method: "DELETE",
          headers: expect.objectContaining({ "X-Admin-PIN": "1234" }),
        })
      );
    });
  });
});
