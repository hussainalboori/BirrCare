import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { CareStatus } from "@/lib/types";

let mockFluidStatus: CareStatus = {
  kind: "fluid",
  last_at: null,
  last_value: null,
  last_caregiver_name: null,
  last_avatar_color: null,
  minutes_since: null,
  interval_minutes: 0,
  is_overdue_warning: false,
  is_overdue_critical: false,
  today_total_ml: 0,
  today_thickened_ml: 0,
  target_ml: 1500,
  progress_percent: 0,
  is_low_fluid: false,
};

let mockBowelStatus: CareStatus = {
  kind: "bowel",
  last_at: null,
  last_value: null,
  last_caregiver_name: null,
  last_avatar_color: null,
  minutes_since: null,
  interval_minutes: 0,
  is_overdue_warning: false,
  is_overdue_critical: false,
  days_since_bowel: 0,
  is_bowel_overdue: false,
};

const postedEvents: Record<string, unknown>[] = [];
const fetchApi = vi.fn(async (endpoint: string, opts?: RequestInit): Promise<unknown> => {
  if (endpoint.startsWith("/care/status?kind=fluid")) {
    return mockFluidStatus;
  }
  if (endpoint.startsWith("/care/status?kind=bowel")) {
    return mockBowelStatus;
  }
  if (endpoint === "/care/events" && opts?.method === "POST") {
    const body = JSON.parse(opts.body as string);
    postedEvents.push(body);
    if (body.kind === "fluid") {
      const addedMl = Number(body.value);
      const newTotal = (mockFluidStatus.today_total_ml ?? 0) + addedMl;
      const newThickened = (mockFluidStatus.today_thickened_ml ?? 0) + (body.thickened ? addedMl : 0);
      const target = mockFluidStatus.target_ml ?? 1500;
      mockFluidStatus = {
        ...mockFluidStatus,
        last_at: new Date().toISOString(),
        last_value: body.value,
        last_caregiver_name: "Ali",
        minutes_since: 0,
        today_total_ml: newTotal,
        today_thickened_ml: newThickened,
        progress_percent: Math.round((newTotal / target) * 100),
      };
    } else if (body.kind === "bowel") {
      mockBowelStatus = {
        ...mockBowelStatus,
        last_at: new Date().toISOString(),
        last_value: body.value,
        last_caregiver_name: "Ali",
        minutes_since: 0,
        days_since_bowel: 0,
        is_bowel_overdue: false,
      };
    }
    return { id: "evt_test", ...body };
  }
  return {};
});

vi.mock("@/lib/api", () => ({
  fetchApi: (e: string, o?: RequestInit) => fetchApi(e, o),
}));

vi.mock("@/context/LanguageContext", () => ({
  useLanguage: () => ({ locale: "en", t: {} }),
}));

vi.mock("@/context/CaregiverContext", () => ({
  useCaregiver: () => ({
    currentCaregiver: { id: "c1", name: "Ali", role: "caregiver", avatar_color: "#2563eb" },
  }),
}));

vi.mock("@/lib/theme", () => ({
  haptic: vi.fn(),
}));

import { FluidBowelCard } from "@/components/FluidBowelCard";

beforeEach(() => {
  postedEvents.length = 0;
  fetchApi.mockClear();
  mockFluidStatus = {
    kind: "fluid",
    last_at: null,
    last_value: null,
    last_caregiver_name: null,
    last_avatar_color: null,
    minutes_since: null,
    interval_minutes: 0,
    is_overdue_warning: false,
    is_overdue_critical: false,
    today_total_ml: 0,
    today_thickened_ml: 0,
    target_ml: 1500,
    progress_percent: 0,
    is_low_fluid: false,
  };
  mockBowelStatus = {
    kind: "bowel",
    last_at: null,
    last_value: null,
    last_caregiver_name: null,
    last_avatar_color: null,
    minutes_since: null,
    interval_minutes: 0,
    is_overdue_warning: false,
    is_overdue_critical: false,
    days_since_bowel: 0,
    is_bowel_overdue: false,
  };
});
afterEach(cleanup);

describe("FluidBowelCard (Issue #28)", () => {
  it("renders 4 cup-size buttons (50, 100, 150, 200 ml) and progress ring", async () => {
    render(<FluidBowelCard />);
    await waitFor(() => {
      expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=fluid", undefined);
      expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=bowel", undefined);
    });

    expect(screen.getByTestId("fluid-progress-ring")).toBeTruthy();
    expect(screen.getByTestId("fluid-button-50")).toBeTruthy();
    expect(screen.getByTestId("fluid-button-100")).toBeTruthy();
    expect(screen.getByTestId("fluid-button-150")).toBeTruthy();
    expect(screen.getByTestId("fluid-button-200")).toBeTruthy();
    expect(screen.getByTestId("thickened-toggle")).toBeTruthy();
  });

  it("logs 150 ml fluid on 1-tap and updates today's total", async () => {
    render(<FluidBowelCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=fluid", undefined));

    const btn150 = screen.getByTestId("fluid-button-150");
    fireEvent.click(btn150);

    await waitFor(() => {
      expect(postedEvents).toHaveLength(1);
    });

    expect(postedEvents[0]).toMatchObject({
      kind: "fluid",
      value: "150",
      thickened: false,
    });

    // Check component updated status narrative and total
    await waitFor(() => {
      expect(screen.getByTestId("fluid-status-narrative").textContent).toContain("150 ml");
    });
  });

  it("toggles thickened fluid chip and sends thickened: true", async () => {
    render(<FluidBowelCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=fluid", undefined));

    // Click thickened toggle chip
    const toggle = screen.getByTestId("thickened-toggle");
    fireEvent.click(toggle);

    // Click +100 ml cup button
    const btn100 = screen.getByTestId("fluid-button-100");
    fireEvent.click(btn100);

    await waitFor(() => {
      expect(postedEvents).toHaveLength(1);
    });

    expect(postedEvents[0]).toMatchObject({
      kind: "fluid",
      value: "100",
      thickened: true,
    });
  });

  it("displays low fluid amber alert when is_low_fluid is active", async () => {
    mockFluidStatus = {
      ...mockFluidStatus,
      today_total_ml: 400,
      target_ml: 1500,
      progress_percent: 27,
      is_low_fluid: true,
    };

    render(<FluidBowelCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=fluid", undefined));

    expect(screen.getByTestId("low-fluid-alert")).toBeTruthy();
    expect(screen.getByText(/Alert: Fluid intake below 50% by 18:00/)).toBeTruthy();
  });

  it("renders 3 bowel stool scale buttons (Hard, Normal, Loose) and logs on 1-tap", async () => {
    render(<FluidBowelCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=bowel", undefined));

    expect(screen.getByTestId("bowel-button-hard")).toBeTruthy();
    expect(screen.getByTestId("bowel-button-normal")).toBeTruthy();
    expect(screen.getByTestId("bowel-button-loose")).toBeTruthy();

    const normalBtn = screen.getByTestId("bowel-button-normal");
    fireEvent.click(normalBtn);

    await waitFor(() => {
      expect(postedEvents).toHaveLength(1);
    });

    expect(postedEvents[0]).toMatchObject({
      kind: "bowel",
      value: "normal",
    });

    await waitFor(() => {
      expect(screen.getByTestId("bowel-status-narrative").textContent).toContain("Normal");
    });
  });

  it("displays amber card when bowel movement has not occurred in 3 days", async () => {
    mockBowelStatus = {
      ...mockBowelStatus,
      days_since_bowel: 4,
      is_bowel_overdue: true,
    };

    render(<FluidBowelCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=bowel", undefined));

    expect(screen.getByTestId("bowel-overdue-alert")).toBeTruthy();
    expect(screen.getByText(/Alert: No bowel movement in 4 days/)).toBeTruthy();
  });
});
