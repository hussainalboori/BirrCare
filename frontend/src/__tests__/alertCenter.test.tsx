import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
const routerPush = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: routerPush }), usePathname: () => "/vitals" }));
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

import { AlertCenter, HomeTimeBanner } from "@/components/AlertCenter";
import { setHouseholdZone } from "@/lib/householdTime";

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

describe("HomeTimeBanner (issue #58)", () => {
  const device = (timeZone: string) =>
    vi.spyOn(Intl.DateTimeFormat.prototype, "resolvedOptions").mockReturnValue({ timeZone } as Intl.ResolvedDateTimeFormatOptions);
  afterEach(() => vi.restoreAllMocks());

  it("phone in London, home in Riyadh: every caregiver sees home time and home's clock", () => {
    setHouseholdZone("Asia/Riyadh");
    device("Europe/London");
    render(<HomeTimeBanner />);
    const banner = screen.getByTestId("home-time-banner");
    expect(banner.textContent).toContain("Times shown are home time");
    expect(banner.textContent).toContain("Riyadh");
    expect(banner.textContent).toMatch(/\d{1,2}:\d{2}/);
  });

  it("phone in Bahrain, home in Riyadh (both UTC+3): no banner", () => {
    setHouseholdZone("Asia/Riyadh");
    device("Asia/Bahrain");
    render(<HomeTimeBanner />);
    expect(screen.queryByTestId("home-time-banner")).toBeNull();
  });
});

describe("AlertCenter: low stock gets an owner (issue #65)", () => {
  it("I'll get it claims the refill and clears the bar; Already bought goes to the keypad", async () => {
    unacked = [{ ...crisis, id: "ls1", kind: "low_stock", payload: { medication_id: "med_clop", name_en: "Clopidogrel", days_left: 2, blood_thinner: true, pharmacy_name: "Al-Nahdi", pharmacy_phone: "+966110000000" }, emergency: false }];
    const { container } = render(<AlertCenter />);
    const bar = await screen.findByTestId("alert-bar");
    expect(bar.querySelector('a[href="tel:+966110000000"]')?.textContent).toContain("Al-Nahdi");
    expect(bar.querySelector('a[href^="tel:997"]')).toBeNull(); // no emergency call for a pill
    fireEvent.click(screen.getByTestId("alert-refill-claim"));
    await waitFor(() => expect(fetchApi.mock.calls.some(([e, o]) => e === "/medications/med_clop/refill/claim" && o?.method === "POST")).toBe(true));
    await waitFor(() => expect(container.querySelector("[data-kind]")).toBeNull());
    cleanup();

    unacked = [{ ...crisis, id: "rf1", kind: "refill_followup", payload: { medication_id: "med_clop", name_en: "Clopidogrel", stage: "owner", due_by: "2026-10-29T09:00:00Z", days_left: 1, critical: true }, emergency: false }];
    const second = render(<AlertCenter />);
    const red = await waitFor(() => {
      const el = second.container.querySelector('[data-kind="refill_followup"]');
      expect(el).not.toBeNull();
      return el as HTMLElement;
    });
    expect(red.className).toContain("bg-rose-700");
    expect(red.textContent).toContain("is it bought?");
    fireEvent.click(within(red).getByTestId("alert-refill-bought"));
    expect(routerPush).toHaveBeenCalledWith("/?refill=med_clop");
  });
});
