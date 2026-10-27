import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";

// Issue #12: alerts missed while the phone was locked are replayed on resync,
// with the siren once, until acknowledged.

let unacked: unknown[] = [];
const fetchApi = vi.fn(async (endpoint: string, _opts?: RequestInit): Promise<unknown> => {
  if (endpoint === "/alerts/unacked") return unacked;
  if (endpoint.startsWith("/alerts/") && endpoint.endsWith("/ack")) {
    unacked = [];
    return { status: "acknowledged" };
  }
  if (endpoint === "/contacts") return { emergency_number: "997" };
  if (endpoint === "/health") return { status: "healthy", timezone: "Asia/Bahrain" };
  return {};
});
const playAlert = vi.fn();

let mockRole = "caregiver";
vi.mock("@/lib/api", () => ({ fetchApi: (e: string, o?: RequestInit) => fetchApi(e, o) }));
vi.mock("@/lib/alertSound", () => ({ playAlert: (k: string) => playAlert(k), installAudioUnlock: () => {} }));
vi.mock("@/context/LanguageContext", () => ({ useLanguage: () => ({ locale: "en" }) }));
vi.mock("@/context/CaregiverContext", () => ({
  useCaregiver: () => ({
    currentCaregiver: { id: "c1", name: "Hussain", role: mockRole },
    allCaregivers: [
      { id: "c1", name: "Hussain" },
      { id: "c2", name: "Ali" },
    ],
  }),
}));

import { AlertCenter, TimezoneMismatchBanner } from "@/components/AlertCenter";

const crisis = {
  id: "a1",
  kind: "bp_crisis",
  created_by_name: "Fatima",
  payload: { systolic: 185, diastolic: 115 },
  created_at: new Date().toISOString(),
};

beforeEach(() => {
  mockRole = "caregiver";
  unacked = [];
  fetchApi.mockClear();
  playAlert.mockClear();
});
afterEach(cleanup);

const resync = () => act(() => window.dispatchEvent(new Event(RESYNC_EVENT)));

describe("AlertCenter (issue #12)", () => {
  it("replays an unacked crisis on resync with the siren once, and never again after Seen", async () => {
    render(<AlertCenter />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/alerts/unacked", undefined));
    expect(screen.queryByTestId("crisis-bar")).toBeNull();

    // The phone was locked when it happened; the stream resyncs on wake.
    unacked = [crisis];
    resync();
    const bar = await screen.findByTestId("crisis-bar");
    expect(bar.textContent).toContain("185/115");
    expect(bar.textContent).toContain("Fatima");
    expect(bar.querySelector('a[href="tel:997"]')).not.toBeNull();
    expect(playAlert).toHaveBeenCalledTimes(1);

    // Another resync before acknowledging: still shown, but the siren doesn't repeat.
    resync();
    await waitFor(() => expect(fetchApi.mock.calls.filter(([e]) => e === "/alerts/unacked")).toHaveLength(3));
    expect(screen.getByTestId("crisis-bar")).toBeTruthy();
    expect(playAlert).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "Seen" }));
    await waitFor(() => expect(screen.queryByTestId("crisis-bar")).toBeNull());
    expect(fetchApi).toHaveBeenCalledWith("/alerts/a1/ack", { method: "POST" });

    resync();
    await waitFor(() => expect(fetchApi.mock.calls.filter(([e]) => e === "/alerts/unacked")).toHaveLength(4));
    expect(screen.queryByTestId("crisis-bar")).toBeNull();
    expect(playAlert).toHaveBeenCalledTimes(1);
  });

  it("refetches when a live event carries an alert id", async () => {
    render(<AlertCenter />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/alerts/unacked", undefined));
    unacked = [{ ...crisis, id: "a2", kind: "vomited", payload: { name_en: "Aspirin" } }];
    act(() => {
      window.dispatchEvent(new CustomEvent("birrcare:sse_event", { detail: { type: "MED_EVENT", payload: { alert_id: "a2" } } }));
    });
    const bar = await screen.findByTestId("alert-bar");
    expect(bar.textContent).toContain("Vomited after a dose");
    expect(bar.textContent).toContain("Aspirin");
  });
});

import alertKinds from "../../../clinical/alert-kinds.json";

describe("AlertCenter: every alert kind (issue #54, clinical/alert-kinds.json)", () => {
  const payload = { systolic: 185, diastolic: 115, blood_sugar: 2.9, temperature_c: 38.4, spo2: 91, level: "confused", name_en: "Aspirin", days_left: 2 };
  const fallback = "A dose was given inside its minimum interval";

  for (const kind of alertKinds.kinds) {
    it(`${kind}: has its own text; red crisis bar only for emergencies`, async () => {
      const emergency = alertKinds.emergency.includes(kind);
      unacked = [{ ...crisis, id: `k-${kind}`, kind, payload, emergency }];
      const { container } = render(<AlertCenter />);
      const bar = await waitFor(() => {
        const el = container.querySelector("[data-kind]");
        expect(el).not.toBeNull();
        return el as HTMLElement;
      });
      if (kind !== "dose_override") expect(bar.textContent).not.toContain(fallback);
      expect(bar.getAttribute("data-testid")).toBe(emergency ? "crisis-bar" : kind === "bp_crisis" ? "crisis-bar" : "alert-bar");
      if (emergency) expect(bar.className).toContain("bg-rose-700");
      if (kind === "sugar_very_high" || kind === "fever") expect(bar.className).toContain("bg-amber-800");
    });
  }

  it("sugar_low shows the 15-minute recheck timer and the call buttons, no dose", async () => {
    unacked = [{ ...crisis, id: "s1", kind: "sugar_low", payload: { blood_sugar: 2.9 }, emergency: true, created_at: new Date().toISOString() }];
    render(<AlertCenter />);
    const bar = await screen.findByTestId("crisis-bar");
    expect(bar.textContent).toContain("2.9 mmol/L");
    expect(bar.textContent).toContain("fast sugar");
    expect(screen.getByTestId("sugar-recheck").textContent).toMatch(/Recheck sugar in (15:00|14:5\d)/);
    expect(bar.querySelector('a[href="tel:997"]')).not.toBeNull();
    expect(bar.textContent).not.toMatch(/units|insulin/i);
  });

  it("the recheck timer says 'now' once 15 minutes have passed", async () => {
    unacked = [{ ...crisis, id: "s2", kind: "sugar_low", payload: { blood_sugar: 3.1 }, emergency: true, created_at: new Date(Date.now() - 16 * 60_000).toISOString() }];
    render(<AlertCenter />);
    expect((await screen.findByTestId("sugar-recheck")).textContent).toBe("Recheck sugar now");
  });
});

describe("AlertCenter: overdue dose (issue #55)", () => {
  const overdue = {
    id: "d1",
    kind: "dose_overdue",
    payload: { medication_id: "med_clop", name_en: "Clopidogrel", dosage: "75 mg", slot: "morning", scheduled_date: "2026-10-27", slot_time: "08:00", critical: true },
    created_at: new Date().toISOString(),
  };
  const logCalls = () => fetchApi.mock.calls.filter(([e]) => e === "/medications/log").map(([, o]) => JSON.parse(String(o?.body)));

  it("says which dose isn't logged, red for a blood thinner, without the call buttons", async () => {
    unacked = [overdue];
    render(<AlertCenter />);
    const bar = await screen.findByTestId("alert-bar");
    expect(bar.textContent).toContain("Clopidogrel 75 mg (08:00) isn't logged");
    expect(bar.className).toContain("bg-rose-700");
    expect(bar.querySelector('a[href^="tel:"]')).toBeNull();
    expect(screen.getByRole("button", { name: "Seen" })).toBeTruthy();
  });

  it("Give as me logs the dose and the alert goes", async () => {
    unacked = [overdue];
    render(<AlertCenter />);
    fireEvent.click(await screen.findByTestId("give-dose"));
    await waitFor(() => expect(screen.queryByTestId("alert-bar")).toBeNull());
    expect(logCalls()).toEqual([{ medication_id: "med_clop", slot: "morning", scheduled_date: "2026-10-27", status: "taken" }]);
  });

  it("Someone else gave it: pick the sibling, logged on their behalf", async () => {
    unacked = [overdue];
    render(<AlertCenter />);
    fireEvent.click(await screen.findByTestId("someone-else-gave"));
    expect(screen.queryByRole("button", { name: "Hussain" })).toBeNull(); // that's "Give as"
    fireEvent.click(screen.getByRole("button", { name: "Ali" }));
    await waitFor(() => expect(screen.queryByTestId("alert-bar")).toBeNull());
    expect(logCalls()).toEqual([{ medication_id: "med_clop", slot: "morning", scheduled_date: "2026-10-27", status: "taken", given_by: "c2" }]);
  });

  it("a viewer sees the alert but no buttons to give it", async () => {
    mockRole = "viewer";
    unacked = [overdue];
    render(<AlertCenter />);
    await screen.findByTestId("alert-bar");
    expect(screen.queryByTestId("give-dose")).toBeNull();
  });
});

describe("TimezoneMismatchBanner (issue #26)", () => {
  it("server zone ≠ device zone → admin banner rendered", async () => {
    mockRole = "admin";
    vi.spyOn(Intl.DateTimeFormat.prototype, "resolvedOptions").mockReturnValue({
      timeZone: "UTC",
    } as Intl.ResolvedDateTimeFormatOptions);

    render(<TimezoneMismatchBanner />);
    const banner = await screen.findByTestId("timezone-mismatch-banner");
    expect(banner).toBeTruthy();
    expect(banner.textContent).toContain("Asia/Bahrain");
    expect(banner.textContent).toContain("UTC");
  });

  it("server zone = device zone → no banner", async () => {
    mockRole = "admin";
    vi.spyOn(Intl.DateTimeFormat.prototype, "resolvedOptions").mockReturnValue({
      timeZone: "Asia/Bahrain",
    } as Intl.ResolvedDateTimeFormatOptions);

    render(<TimezoneMismatchBanner />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/health", undefined));
    expect(screen.queryByTestId("timezone-mismatch-banner")).toBeNull();
  });

  it("non-admin caregiver → no banner even on mismatch", async () => {
    mockRole = "caregiver";
    vi.spyOn(Intl.DateTimeFormat.prototype, "resolvedOptions").mockReturnValue({
      timeZone: "UTC",
    } as Intl.ResolvedDateTimeFormatOptions);

    render(<TimezoneMismatchBanner />);
    await waitFor(() => expect(fetchApi).toHaveBeenCalledWith("/health", undefined));
    expect(screen.queryByTestId("timezone-mismatch-banner")).toBeNull();
  });
});

