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
  return {};
});
const playAlert = vi.fn();

vi.mock("@/lib/api", () => ({ fetchApi: (e: string, o?: RequestInit) => fetchApi(e, o) }));
vi.mock("@/lib/alertSound", () => ({ playAlert: (k: string) => playAlert(k), installAudioUnlock: () => {} }));
vi.mock("@/context/LanguageContext", () => ({ useLanguage: () => ({ locale: "en" }) }));
vi.mock("@/context/CaregiverContext", () => ({ useCaregiver: () => ({ currentCaregiver: { id: "c1", role: "caregiver" } }) }));

import { AlertCenter } from "@/components/AlertCenter";

const crisis = {
  id: "a1",
  kind: "bp_crisis",
  created_by_name: "Fatima",
  payload: { systolic: 185, diastolic: 115 },
  created_at: new Date().toISOString(),
};

beforeEach(() => {
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
