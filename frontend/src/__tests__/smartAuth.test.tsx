import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import React from "react";
import { CaregiverProvider, useCaregiver } from "@/context/CaregiverContext";
import { LanguageProvider } from "@/context/LanguageContext";
import { SiblingBar } from "@/components/SiblingBar";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { DevicePairingModal } from "@/components/DevicePairingModal";
import { Caregiver, CaregiverOption, TrustStatus, PairedDevice, HomeLocation } from "@/lib/types";

// Mock router
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));

// Mock emergency link
vi.mock("@/lib/emergencyLink", () => ({
  emergencyPath: () => "/emergency",
  useEmergencyToken: () => null,
}));

const currentCaregiverData: Caregiver = {
  id: "c1",
  name: "Hussain Alboori",
  role: "super_admin",
  avatar_color: "#2563eb",
  preferred_lang: "ar",
  is_active: true,
  created_at: "2026-01-01T00:00:00Z",
};

const caregiversList: CaregiverOption[] = [
  { id: "c1", name: "Hussain", avatar_color: "#2563eb" },
  { id: "c2", name: "Ali", avatar_color: "#16a34a" },
  { id: "c3", name: "Fatima", avatar_color: "#db2777" },
];

let trustStatusData: TrustStatus = {
  trusted: true,
  reason: "trusted_ip",
};

let pairedDevicesData: PairedDevice[] = [
  { id: "p1", device_name: "Bedside iPad", created_at: "2026-10-21T00:00:00Z", is_active: true },
];

let homeLocationData: HomeLocation = {
  latitude: 26.123456,
  longitude: 50.123456,
  radius_meters: 75,
};

const switchCaregiverHandler = vi.fn(async (body: any) => {
  if (trustStatusData.trusted) {
    return {
      status: "authenticated",
      caregiver: {
        id: body.caregiver_id,
        name: body.caregiver_id === "c2" ? "Ali" : "Fatima",
        role: "caregiver",
        avatar_color: body.caregiver_id === "c2" ? "#16a34a" : "#db2777",
        preferred_lang: "ar",
        is_active: true,
        created_at: "2026-01-01T00:00:00Z",
      },
      trust: trustStatusData,
    };
  }
  const err: any = new Error("PIN_REQUIRED");
  err.status = 403;
  err.data = { error: "PIN_REQUIRED" };
  throw err;
});

const fetchApiMock = vi.fn(async (endpoint: string, opts?: RequestInit) => {
  if (endpoint === "/caregivers") return caregiversList;
  if (endpoint === "/auth/me") return currentCaregiverData;
  if (endpoint.startsWith("/auth/trust-status")) return trustStatusData;
  if (endpoint === "/auth/switch-caregiver") {
    const body = opts?.body ? JSON.parse(opts.body as string) : {};
    return switchCaregiverHandler(body);
  }
  if (endpoint === "/auth/paired-devices") return pairedDevicesData;
  if (endpoint === "/settings/home-location") return homeLocationData;
  if (endpoint === "/auth/pair-device") {
    const body = opts?.body ? JSON.parse(opts.body as string) : {};
    const newDev: PairedDevice = {
      id: "p2",
      device_name: body.device_name,
      created_at: "2026-10-21T00:00:00Z",
      is_active: true,
    };
    pairedDevicesData.push(newDev);
    return { status: "paired", device: newDev };
  }
  if (endpoint.startsWith("/auth/paired-devices/") && opts?.method === "DELETE") {
    pairedDevicesData = [];
    return { status: "revoked" };
  }
  if (endpoint === "/settings/home-location" && opts?.method === "PUT") {
    const body = opts?.body ? JSON.parse(opts.body as string) : {};
    homeLocationData = { ...homeLocationData, ...body };
    return { status: "updated", location: homeLocationData };
  }
  return {};
});

vi.mock("@/lib/api", async () => {
  const actual = await vi.importActual<any>("@/lib/api");
  return {
    ...actual,
    fetchApi: (e: string, o?: RequestInit) => fetchApiMock(e, o),
  };
});

beforeEach(() => {
  fetchApiMock.mockClear();
  switchCaregiverHandler.mockClear();
  trustStatusData = {
    trusted: true,
    reason: "trusted_ip",
  };
});

afterEach(cleanup);

describe("Smart Caregiver Auth (Issue #29)", () => {
  it("switches caregiver in 1 tap without PIN modal when trusted", async () => {
    function TestConsumer() {
      const { isPinModalOpen, currentCaregiver } = useCaregiver();
      return (
        <div>
          <SiblingBar />
          <div data-testid="pin-modal-status">{isPinModalOpen ? "PIN_MODAL_OPEN" : "PIN_MODAL_CLOSED"}</div>
          <div data-testid="current-caregiver-name">{currentCaregiver?.name}</div>
        </div>
      );
    }

    render(
      <LanguageProvider>
        <CaregiverProvider>
          <TestConsumer />
        </CaregiverProvider>
      </LanguageProvider>
    );

    // Wait for initial load
    await waitFor(() => expect(screen.getByTestId("sibling-c2")).toBeDefined());

    // Click on sibling Ali (c2)
    const aliBtn = screen.getByTestId("sibling-c2");
    await act(async () => {
      fireEvent.click(aliBtn);
    });

    // Should call switch-caregiver
    await waitFor(() => {
      expect(switchCaregiverHandler).toHaveBeenCalledWith(
        expect.objectContaining({ caregiver_id: "c2" })
      );
    });

    // PIN modal should NOT be opened
    expect(screen.getByTestId("pin-modal-status").textContent).toBe("PIN_MODAL_CLOSED");
    // Current caregiver should be updated to Ali
    expect(screen.getByTestId("current-caregiver-name").textContent).toBe("Ali");
  });

  it("falls back to opening PIN modal when untrusted (e.g. 4G/cellular outside geofence)", async () => {
    trustStatusData = {
      trusted: false,
      reason: "untrusted",
    };

    function TestConsumer() {
      const { isPinModalOpen, targetCaregiverId } = useCaregiver();
      return (
        <div>
          <SiblingBar />
          <div data-testid="pin-modal-status">{isPinModalOpen ? "PIN_MODAL_OPEN" : "PIN_MODAL_CLOSED"}</div>
          <div data-testid="target-caregiver">{targetCaregiverId || "NONE"}</div>
        </div>
      );
    }

    render(
      <LanguageProvider>
        <CaregiverProvider>
          <TestConsumer />
        </CaregiverProvider>
      </LanguageProvider>
    );

    await waitFor(() => expect(screen.getByTestId("sibling-c2")).toBeDefined());

    const aliBtn = screen.getByTestId("sibling-c2");
    await act(async () => {
      fireEvent.click(aliBtn);
    });

    await waitFor(() => {
      expect(switchCaregiverHandler).toHaveBeenCalledWith(
        expect.objectContaining({ caregiver_id: "c2" })
      );
    });

    // PIN modal should now be opened with target c2
    await waitFor(() => {
      expect(screen.getByTestId("pin-modal-status").textContent).toBe("PIN_MODAL_OPEN");
    });
    expect(screen.getByTestId("target-caregiver").textContent).toBe("c2");
  });

  it("displays network status badge correctly in CaregiverHeader", async () => {
    // Trusted case
    trustStatusData = {
      trusted: true,
      reason: "trusted_ip",
    };

    const { rerender } = render(
      <LanguageProvider>
        <CaregiverProvider>
          <CaregiverHeader />
        </CaregiverProvider>
      </LanguageProvider>
    );

    await waitFor(() => {
      const badge = screen.getByTestId("network-status-badge");
      expect(badge.textContent).toContain("شبكة المنزل (تبديل مباشر)");
    });

    // Untrusted case
    trustStatusData = {
      trusted: false,
      reason: "untrusted",
    };

    rerender(
      <LanguageProvider>
        <CaregiverProvider>
          <CaregiverHeader />
        </CaregiverProvider>
      </LanguageProvider>
    );

    // Recheck trust status trigger
    await act(async () => {
      window.dispatchEvent(new CustomEvent("birrcare:auth_changed"));
    });

    await waitFor(() => {
      const badge = screen.getByTestId("network-status-badge");
      expect(badge.textContent).toContain("خارج المنزل (مطلوب رمز المرور)");
    });
  });

  it("allows pairing bedside devices and configuring geofence in DevicePairingModal", async () => {
    const onClose = vi.fn();

    render(
      <LanguageProvider>
        <CaregiverProvider>
          <DevicePairingModal isOpen={true} onClose={onClose} />
        </CaregiverProvider>
      </LanguageProvider>
    );

    // Should display existing paired device
    await waitFor(() => {
      expect(screen.getByText("Bedside iPad")).toBeDefined();
    });

    // Pair a new device
    const input = screen.getByPlaceholderText(/آيباد غرفة الوالد/);
    fireEvent.change(input, { target: { value: "Living Room Tablet" } });

    // Pairing stays disabled until the admin types their 4-digit PIN.
    expect((screen.getByText("اعتماد الجهاز").closest("button") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId("pairing-pin"), { target: { value: "1234" } });

    const pairBtn = screen.getByText("اعتماد الجهاز");
    await act(async () => {
      fireEvent.click(pairBtn);
    });

    await waitFor(() => {
      expect(fetchApiMock).toHaveBeenCalledWith(
        "/auth/pair-device",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ device_name: "Living Room Tablet", pin: "1234" }),
        })
      );
    });
  });
});
