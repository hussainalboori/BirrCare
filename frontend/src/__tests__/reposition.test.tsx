import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { CareStatus } from "@/lib/types";

let mockStatus: CareStatus = {
  kind: "reposition",
  last_at: null,
  last_value: null,
  last_caregiver_name: null,
  last_avatar_color: null,
  last_skin_condition: null,
  next_due_at: null,
  minutes_since: null,
  minutes_until_due: null,
  is_overdue_warning: false,
  is_overdue_critical: false,
  interval_minutes: 120,
};

const postedEvents: unknown[] = [];
const fetchApi = vi.fn(async (endpoint: string, opts?: RequestInit): Promise<unknown> => {
  if (endpoint.startsWith("/care/status")) {
    return mockStatus;
  }
  if (endpoint === "/care/events" && opts?.method === "POST") {
    const body = JSON.parse(opts.body as string);
    postedEvents.push(body);
    mockStatus = {
      ...mockStatus,
      last_at: new Date().toISOString(),
      last_value: body.value,
      last_skin_condition: body.skin_condition,
      last_caregiver_name: "Fatima",
      minutes_since: 0,
      is_overdue_warning: false,
      is_overdue_critical: false,
    };
    return { id: "evt_1", ...body };
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
    currentCaregiver: { id: "c1", name: "Fatima", role: "caregiver", avatar_color: "#2563eb" },
  }),
}));

vi.mock("@/lib/theme", () => ({
  haptic: vi.fn(),
}));

import { RepositionCard } from "@/components/RepositionCard";

beforeEach(() => {
  postedEvents.length = 0;
  fetchApi.mockClear();
  mockStatus = {
    kind: "reposition",
    last_at: null,
    last_value: null,
    last_caregiver_name: null,
    last_avatar_color: null,
    last_skin_condition: null,
    next_due_at: null,
    minutes_since: null,
    minutes_until_due: null,
    is_overdue_warning: false,
    is_overdue_critical: false,
    interval_minutes: 120,
  };
});
afterEach(cleanup);

describe("RepositionCard (Issue #27)", () => {
  it("renders with initial empty status and 3 turn buttons", async () => {
    render(<RepositionCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=reposition", undefined));

    expect(screen.getByText("Repositioning Timer (Pressure Sores)")).toBeTruthy();
    expect(screen.getByTestId("reposition-left")).toBeTruthy();
    expect(screen.getByTestId("reposition-back")).toBeTruthy();
    expect(screen.getByTestId("reposition-right")).toBeTruthy();
    expect(screen.getByText(/No repositioning logged yet today/)).toBeTruthy();
  });

  it("logs turning to right side on one tap", async () => {
    render(<RepositionCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=reposition", undefined));

    const rightBtn = screen.getByTestId("reposition-right");
    fireEvent.click(rightBtn);

    await waitFor(() => {
      expect(postedEvents).toHaveLength(1);
    });

    expect(postedEvents[0]).toMatchObject({
      kind: "reposition",
      value: "right",
      skin_condition: "ok",
    });

    // After post, component refetches status and updates
    await waitFor(() => {
      expect(screen.getByTestId("reposition-status").textContent).toContain("Right side");
    });
  });

  it("allows selecting red area skin check and posts it", async () => {
    render(<RepositionCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=reposition", undefined));

    // Select Red area chip
    const redAreaChip = screen.getByTestId("skin-red-area");
    fireEvent.click(redAreaChip);

    // Urgent notice appears
    expect(
      screen.getByText("An urgent handover flag will be automatically raised for the next caregiver.")
    ).toBeTruthy();

    // Click Left side
    fireEvent.click(screen.getByTestId("reposition-left"));

    await waitFor(() => {
      expect(postedEvents).toHaveLength(1);
    });

    expect(postedEvents[0]).toMatchObject({
      kind: "reposition",
      value: "left",
      skin_condition: "red_area",
    });
  });

  it("displays narrative time ago and caregiver name when status exists", async () => {
    mockStatus = {
      ...mockStatus,
      last_at: new Date(Date.now() - 100 * 60 * 1000).toISOString(),
      last_value: "left",
      last_caregiver_name: "Fatima",
      last_avatar_color: "#2563eb",
      minutes_since: 100,
      interval_minutes: 120,
    };

    render(<RepositionCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=reposition", undefined));

    const statusText = screen.getByTestId("reposition-status").textContent;
    expect(statusText).toContain("Last turned");
    expect(statusText).toContain("1h 40m ago");
    expect(statusText).toContain("Left side");
    expect(statusText).toContain("Fatima");
  });

  it("renders warning badge when overdue warning is active", async () => {
    mockStatus = {
      ...mockStatus,
      last_at: new Date(Date.now() - 135 * 60 * 1000).toISOString(),
      last_value: "back",
      minutes_since: 135,
      is_overdue_warning: true,
      is_overdue_critical: false,
    };

    render(<RepositionCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=reposition", undefined));

    expect(screen.getByText("Due soon")).toBeTruthy();
  });

  it("renders critical alert badge when overdue critical is active", async () => {
    mockStatus = {
      ...mockStatus,
      last_at: new Date(Date.now() - 170 * 60 * 1000).toISOString(),
      last_value: "right",
      minutes_since: 170,
      is_overdue_warning: false,
      is_overdue_critical: true,
    };

    render(<RepositionCard />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/care/status?kind=reposition", undefined));

    expect(screen.getByText("Overdue!")).toBeTruthy();
  });
});
