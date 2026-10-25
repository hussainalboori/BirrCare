import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import React from "react";
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";
import { AdminLogsModal } from "@/components/AdminLogsModal";
import { LanguageProvider } from "@/context/LanguageContext";
import * as logsApi from "@/lib/logs";

vi.mock("@/lib/logs", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/logs")>();
  return {
    ...actual,
    fetchUserLogs: vi.fn(),
    fetchSystemLogs: vi.fn(),
    fetchAccessAudits: vi.fn(),
  };
});

describe("AdminLogsModal", () => {
  const mockUserLogs: logsApi.PaginatedLogsResponse<logsApi.UserLogEntry> = {
    entries: [
      {
        id: "u1",
        caregiver_name: "Fatima",
        level: "info",
        action: "POST /api/v1/medications/log",
        message: "Aspirin dose given",
        created_at: "2026-10-25T10:00:00Z",
      },
    ],
    total: 1,
    limit: 25,
    offset: 0,
  };

  const mockSystemLogs: logsApi.PaginatedLogsResponse<logsApi.SystemLogEntry> = {
    entries: [
      {
        id: "s1",
        level: "info",
        subsystem: "server",
        event: "startup",
        message: "Birrcare backend listening on :8080",
        created_at: "2026-10-25T10:00:00Z",
      },
    ],
    total: 1,
    limit: 25,
    offset: 0,
  };

  const mockAccessAudits: logsApi.PaginatedLogsResponse<logsApi.AccessAuditEntry> = {
    entries: [
      {
        id: "a1",
        caregiver_id: "cg_superadmin",
        caregiver_name: "Hussain",
        stream: "user",
        operation: "list",
        records_count: 1,
        created_at: "2026-10-25T10:00:00Z",
      },
    ],
    total: 1,
    limit: 25,
    offset: 0,
  };

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(logsApi.fetchUserLogs).mockResolvedValue(mockUserLogs);
    vi.mocked(logsApi.fetchSystemLogs).mockResolvedValue(mockSystemLogs);
    vi.mocked(logsApi.fetchAccessAudits).mockResolvedValue(mockAccessAudits);
  });

  afterEach(() => {
    cleanup();
  });

  it("renders user logs by default and switches to system logs and access audits", async () => {
    render(
      <LanguageProvider initialLocale="en">
        <AdminLogsModal isOpen={true} onClose={() => {}} />
      </LanguageProvider>
    );

    // Initial load: User logs
    await waitFor(() => {
      expect(screen.getByText("Aspirin dose given")).toBeTruthy();
    });

    // Switch to System Events tab
    const systemTabButton = screen.getByText("System Events");
    fireEvent.click(systemTabButton);

    await waitFor(() => {
      expect(screen.getByText("Birrcare backend listening on :8080")).toBeTruthy();
    });

    // Switch to Access Audits tab
    const auditsTabButton = screen.getByText("Access Audits");
    fireEvent.click(auditsTabButton);

    await waitFor(() => {
      expect(screen.getByText("Hussain")).toBeTruthy();
    });
  });

  it("supports text filtering and export URLs", async () => {
    const windowOpenSpy = vi.spyOn(window, "open").mockImplementation(() => null);

    render(
      <LanguageProvider initialLocale="en">
        <AdminLogsModal isOpen={true} onClose={() => {}} />
      </LanguageProvider>
    );

    await waitFor(() => {
      expect(screen.getByText("Aspirin dose given")).toBeTruthy();
    });

    // Trigger CSV export
    const csvButton = screen.getByText("CSV");
    fireEvent.click(csvButton);

    expect(windowOpenSpy).toHaveBeenCalledWith(
      expect.stringContaining("/api/v1/admin/logs/export?stream=user&format=csv"),
      "_blank"
    );

    // Search input
    const searchInput = screen.getByPlaceholderText("Search logs...");
    fireEvent.change(searchInput, { target: { value: "Aspirin" } });

    await waitFor(() => {
      expect(logsApi.fetchUserLogs).toHaveBeenCalledWith(
        expect.objectContaining({
          query: "Aspirin",
        })
      );
    });
  });

  it("renders correctly in Arabic (RTL) and handles close", async () => {
    const onCloseSpy = vi.fn();

    render(
      <LanguageProvider initialLocale="ar">
        <AdminLogsModal isOpen={true} onClose={onCloseSpy} />
      </LanguageProvider>
    );

    await waitFor(() => {
      expect(screen.getByText("سجلات النظام والعمليات")).toBeTruthy();
      expect(screen.getByText("نشاط وأخطاء المستخدمين")).toBeTruthy();
    });

    const closeButton = screen.getByLabelText("إغلاق");
    fireEvent.click(closeButton);
    expect(onCloseSpy).toHaveBeenCalledTimes(1);
  });
});
