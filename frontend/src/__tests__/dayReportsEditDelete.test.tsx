import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { DayReports } from "@/components/DayReports";
import { HandoverNote } from "@/lib/types";
import { translations } from "@/lib/i18n";

let currentCaregiver: any = null;
const mockFetchApi = vi.fn();

vi.mock("@/lib/api", () => ({
  fetchApi: (...args: any[]) => (mockFetchApi as any)(...args),
}));

vi.mock("@/context/LanguageContext", () => ({
  useLanguage: () => ({
    locale: "ar",
    t: translations.ar,
    toggleLocale: () => {},
  }),
}));

vi.mock("@/context/CaregiverContext", () => ({
  useCaregiver: () => ({
    currentCaregiver,
    openPinModal: vi.fn(),
  }),
}));

const mockNote: HandoverNote = {
  id: "note_123",
  caregiver_id: "cg_fatima",
  caregiver_name: "فاطمة",
  avatar_color: "#059669",
  created_at: "2026-10-24T12:00:00Z",
  shift_type: "report",
  summary: "Initial note summary",
  urgent_flag: false,
  tags: ["ate_half"],
};

beforeEach(() => {
  mockFetchApi.mockReset();
  mockFetchApi.mockImplementation((url: string) => {
    if (url.startsWith("/handovers")) {
      return Promise.resolve([mockNote]);
    }
    return Promise.resolve({});
  });
});
afterEach(cleanup);

describe("DayReports edit and delete", () => {
  it("shows Edit and Delete buttons for the report author", async () => {
    currentCaregiver = { id: "cg_fatima", name: "فاطمة", role: "caregiver" };
    render(<DayReports />);

    await waitFor(() => {
      expect(screen.getByText("تعديل")).toBeTruthy();
      expect(screen.getByText("حذف")).toBeTruthy();
    });
  });

  it("shows Edit and Delete buttons for an admin even if not the author", async () => {
    currentCaregiver = { id: "cg_ali", name: "علي", role: "admin" };
    render(<DayReports />);

    await waitFor(() => {
      expect(screen.getByText("تعديل")).toBeTruthy();
      expect(screen.getByText("حذف")).toBeTruthy();
    });
  });

  it("does not show Edit and Delete buttons for another non-admin caregiver", async () => {
    currentCaregiver = { id: "cg_other", name: "سارة", role: "caregiver" };
    render(<DayReports />);

    await waitFor(() => {
      expect(screen.getByText("Initial note summary")).toBeTruthy();
      expect(screen.queryByText("تعديل")).toBeNull();
      expect(screen.queryByText("حذف")).toBeNull();
    });
  });

  it("allows author to edit report and calls PUT /handovers/:id", async () => {
    currentCaregiver = { id: "cg_fatima", name: "فاطمة", role: "caregiver" };
    render(<DayReports />);

    await waitFor(() => expect(screen.getByText("تعديل")).toBeTruthy());
    fireEvent.click(screen.getByText("تعديل"));

    expect((screen.getByLabelText("تعديل نص البلاغ") as HTMLTextAreaElement).value).toBe("Initial note summary");
    fireEvent.change(screen.getByLabelText("تعديل نص البلاغ"), { target: { value: "Updated note text" } });

    mockFetchApi.mockResolvedValueOnce({ status: "success" });
    fireEvent.click(screen.getByText("حفظ التعديلات"));

    await waitFor(() => {
      expect(mockFetchApi).toHaveBeenCalledWith(
        "/handovers/note_123",
        expect.objectContaining({
          method: "PUT",
          body: expect.stringContaining("Updated note text"),
        })
      );
    });
  });

  it("allows author to delete report with in-page confirmation and calls DELETE /handovers/:id", async () => {
    currentCaregiver = { id: "cg_fatima", name: "فاطمة", role: "caregiver" };
    render(<DayReports />);

    await waitFor(() => expect(screen.getByText("حذف")).toBeTruthy());
    fireEvent.click(screen.getByText("حذف"));

    expect(screen.getByText("هل أنت متأكد من حذف هذا البلاغ نهائياً؟")).toBeTruthy();
    expect(screen.getByText("نعم، احذف البلاغ")).toBeTruthy();

    mockFetchApi.mockResolvedValueOnce({ status: "success" });
    fireEvent.click(screen.getByText("نعم، احذف البلاغ"));

    await waitFor(() => {
      expect(mockFetchApi).toHaveBeenCalledWith("/handovers/note_123", { method: "DELETE" });
    });
  });
});
