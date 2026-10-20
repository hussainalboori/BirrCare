import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { translations } from "@/lib/i18n";

// Issue #10: the vitals form must not save a sugar or alertness nobody measured.

const fetchApi = vi.fn(async (endpoint: string, _opts?: RequestInit): Promise<unknown> => (endpoint.startsWith("/vitals") ? [] : {}));
vi.mock("@/lib/api", () => ({ fetchApi: (endpoint: string, opts?: RequestInit) => fetchApi(endpoint, opts) }));
vi.mock("@/context/LanguageContext", () => ({ useLanguage: () => ({ locale: "en", t: translations.en, toggleLocale: () => {} }) }));
vi.mock("@/context/CaregiverContext", () => ({
  useCaregiver: () => ({ currentCaregiver: { id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669" }, openPinModal: () => {} }),
}));
vi.mock("@/hooks/useLiveSync", () => ({ useLiveSync: () => ({ status: "connected" }) }));
vi.mock("@/components/CaregiverHeader", () => ({ CaregiverHeader: () => null }));
const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));

import VitalsPage from "@/app/vitals/page";

const t = translations.en;

function postedBodies(): Record<string, unknown>[] {
  return fetchApi.mock.calls
    .filter(([endpoint, opts]) => endpoint === "/vitals" && opts?.method === "POST")
    .map(([, opts]) => JSON.parse(String(opts!.body)));
}

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

async function openKind(title: string) {
  fireEvent.click(await screen.findByRole("button", { name: new RegExp(escape(t.recordVitals)) }));
  fireEvent.click(await screen.findByRole("button", { name: new RegExp(escape(title)) }));
}

beforeEach(() => {
  fetchApi.mockClear();
  vi.spyOn(window, "alert").mockImplementation(() => {});
  window.scrollTo = vi.fn() as unknown as typeof window.scrollTo;
});
afterEach(cleanup);

describe("vitals form (issue #10)", () => {
  it("Full check with only BP sends no blood_sugar and no alertness_level", async () => {
    render(<VitalsPage />);
    await openKind(t.vitalFullTitle);
    fireEvent.change(screen.getByLabelText(t.systolic), { target: { value: "130" } });
    fireEvent.change(screen.getByLabelText(t.diastolic), { target: { value: "85" } });
    fireEvent.change(screen.getByLabelText(t.pulse), { target: { value: "72" } });
    expect((screen.getByLabelText(t.bloodSugar) as HTMLInputElement).value).toBe("");
    fireEvent.click(screen.getByRole("button", { name: new RegExp(escape(t.saveVitals)) }));

    await waitFor(() => expect(postedBodies()).toHaveLength(1));
    const body = postedBodies()[0];
    expect(body).toMatchObject({ vital_type: "full", systolic: 130, diastolic: 85, pulse: 72 });
    expect(body).not.toHaveProperty("blood_sugar");
    expect(body).not.toHaveProperty("alertness_level");
  });

  it("issue #11: BP 185/115 with an empty pulse saves, with no pulse in the payload", async () => {
    render(<VitalsPage />);
    await openKind(t.vitalBPTitle);
    fireEvent.change(screen.getByLabelText(t.systolic), { target: { value: "185" } });
    fireEvent.change(screen.getByLabelText(t.diastolic), { target: { value: "115" } });
    // The crisis view with the call buttons is already on screen before saving.
    expect(await screen.findByTestId("live-crisis")).toBeTruthy();
    expect(screen.getAllByRole("link").some((a) => a.getAttribute("href")?.startsWith("tel:"))).toBe(true);
    expect(postedBodies()).toHaveLength(0);

    fireEvent.click(screen.getByRole("button", { name: new RegExp(escape(t.saveVitals)) }));
    await waitFor(() => expect(postedBodies()).toHaveLength(1));
    const body = postedBodies()[0];
    expect(body).toMatchObject({ vital_type: "bp", systolic: 185, diastolic: 115 });
    expect(body).not.toHaveProperty("pulse");
    expect(window.alert).not.toHaveBeenCalled();
  });

  it("issue #13: choosing 'Hard to wake' shows what to do and opens F.A.S.T. after saving", async () => {
    render(<VitalsPage />);
    await openKind(t.vitalAlertnessTitle);
    fireEvent.click(screen.getByRole("button", { name: /Hard to wake/ }));
    expect(screen.getByRole("alert").textContent).toContain("Call the doctor now, emergency if this is new");
    expect(screen.getByRole("link", { name: /F\.A\.S\.T\./ }).getAttribute("href")).toBe("/fast");
    fireEvent.click(screen.getByRole("button", { name: new RegExp(escape(t.saveVitals)) }));
    await waitFor(() => expect(postedBodies()).toHaveLength(1));
    expect(postedBodies()[0]).toMatchObject({ vital_type: "alertness", alertness_level: "drowsy" });
    await waitFor(() => expect(push).toHaveBeenCalledWith("/fast"));
  });

  it("after saving sugar 14.2, reopening Full starts with an empty sugar field", async () => {
    render(<VitalsPage />);
    await openKind(t.vitalSugarTitle);
    fireEvent.change(screen.getByLabelText(t.bloodSugar), { target: { value: "14.2" } });
    fireEvent.click(screen.getByRole("button", { name: new RegExp(escape(t.saveVitals)) }));
    await waitFor(() => expect(postedBodies()).toHaveLength(1));
    expect(postedBodies()[0]).toMatchObject({ blood_sugar: 14.2 });

    await openKind(t.vitalFullTitle);
    expect((screen.getByLabelText(t.bloodSugar) as HTMLInputElement).value).toBe("");
    for (const b of screen.getAllByRole("button", { pressed: true })) {
      // No alertness button may start selected.
      expect(b.textContent).not.toMatch(/Alert|Confused|Restless|Drowsy/);
    }
  });
});
