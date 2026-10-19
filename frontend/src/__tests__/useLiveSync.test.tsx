import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { StrictMode } from "react";
import { backoffDelay, BACKOFF_MAX_MS, STALE_AFTER_HIDDEN_MS, useLiveSync } from "@/hooks/useLiveSync";
import { AUTH_CHANGED_EVENT } from "@/lib/api";

let authStatus = 200;
const fetchMock = vi.fn(async () => ({ status: authStatus }) as Response);

class MockEventSource {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;
  static instances: MockEventSource[] = [];

  readyState = MockEventSource.CONNECTING;
  onopen: ((e: Event) => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: Event) => void) | null = null;
  listeners: Record<string, Array<(e: Event) => void>> = {};
  closed = false;

  constructor(public url: string, public init?: EventSourceInit) {
    MockEventSource.instances.push(this);
  }
  addEventListener(type: string, cb: (e: Event) => void) {
    (this.listeners[type] ||= []).push(cb);
  }
  close() {
    this.closed = true;
    this.readyState = MockEventSource.CLOSED;
  }
  // --- test helpers ---
  open() {
    this.readyState = MockEventSource.OPEN;
    this.onopen?.(new Event("open"));
  }
  fail() {
    this.readyState = MockEventSource.CLOSED;
    this.onerror?.(new Event("error"));
  }
  emit(data: unknown) {
    this.onmessage?.(new MessageEvent("message", { data: JSON.stringify(data) }));
  }
}

const live = () => MockEventSource.instances.filter((e) => !e.closed);
const latest = () => MockEventSource.instances[MockEventSource.instances.length - 1];

function setVisibility(state: "visible" | "hidden") {
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
  document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
  MockEventSource.instances = [];
  authStatus = 200;
  fetchMock.mockClear();
  vi.stubGlobal("fetch", fetchMock);
  vi.useFakeTimers();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.spyOn(Math, "random").mockReturnValue(1); // deterministic: upper bound of jitter window
});

afterEach(() => {
  cleanup(); // unmount hooks so their window/document listeners don't leak into the next test
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("backoffDelay", () => {
  it("grows exponentially with equal jitter and is capped", () => {
    expect(backoffDelay(0, () => 0)).toBe(500);
    expect(backoffDelay(0, () => 1)).toBe(1000);
    expect(backoffDelay(3, () => 1)).toBe(8000);
    expect(backoffDelay(3, () => 0)).toBe(4000);
    expect(backoffDelay(20, () => 1)).toBe(BACKOFF_MAX_MS);
    expect(backoffDelay(20, () => 0)).toBe(BACKOFF_MAX_MS / 2);
  });
});

describe("useLiveSync", () => {
  it("keeps exactly one connection under StrictMode double effects and closes it on unmount", () => {
    const { unmount } = renderHook(() => useLiveSync(), { wrapper: StrictMode });
    expect(live()).toHaveLength(1);
    unmount();
    expect(live()).toHaveLength(0);
  });

  it("reconnects with exponential backoff and resets after a successful open", () => {
    renderHook(() => useLiveSync());
    const first = latest();
    act(() => first.open());

    act(() => first.fail());
    expect(first.closed).toBe(true);
    expect(MockEventSource.instances).toHaveLength(1);

    act(() => vi.advanceTimersByTime(999));
    expect(MockEventSource.instances).toHaveLength(1);
    act(() => vi.advanceTimersByTime(1));
    expect(MockEventSource.instances).toHaveLength(2); // 1s

    act(() => latest().fail());
    act(() => vi.advanceTimersByTime(1999));
    expect(MockEventSource.instances).toHaveLength(2);
    act(() => vi.advanceTimersByTime(1));
    expect(MockEventSource.instances).toHaveLength(3); // 2s

    act(() => latest().fail());
    act(() => vi.advanceTimersByTime(4000));
    expect(MockEventSource.instances).toHaveLength(4); // 4s

    // Successful open resets the backoff
    act(() => latest().open());
    act(() => latest().fail());
    act(() => vi.advanceTimersByTime(1000));
    expect(MockEventSource.instances).toHaveLength(5);
    expect(live()).toHaveLength(1);
  });

  it("re-hydrates (onResync) after reconnecting, but not on the very first open", () => {
    const onResync = vi.fn();
    renderHook(() => useLiveSync({ onResync }));
    act(() => latest().open());
    expect(onResync).not.toHaveBeenCalled();

    act(() => latest().fail());
    act(() => vi.advanceTimersByTime(1000));
    act(() => latest().open());
    expect(onResync).toHaveBeenCalledTimes(1);
  });

  it("reconnects immediately when returning from background after iOS froze the stream", () => {
    const onResync = vi.fn();
    renderHook(() => useLiveSync({ onResync }));
    const first = latest();
    act(() => first.open());

    act(() => setVisibility("hidden"));
    act(() => vi.advanceTimersByTime(STALE_AFTER_HIDDEN_MS + 1000));
    act(() => setVisibility("visible"));

    expect(first.closed).toBe(true);
    expect(MockEventSource.instances).toHaveLength(2);
    expect(live()).toHaveLength(1);
    act(() => latest().open());
    expect(onResync).toHaveBeenCalledTimes(1);
  });

  it("reconnects immediately on the 'online' event without waiting for backoff", () => {
    renderHook(() => useLiveSync());
    act(() => latest().open());
    act(() => latest().fail());
    expect(MockEventSource.instances).toHaveLength(1);
    act(() => {
      window.dispatchEvent(new Event("online"));
    });
    expect(MockEventSource.instances).toHaveLength(2);
    expect(live()).toHaveLength(1);
  });

  it("does not reconnect after unmount even if a retry was pending", () => {
    const { unmount } = renderHook(() => useLiveSync());
    act(() => latest().fail());
    unmount();
    act(() => vi.advanceTimersByTime(60_000));
    expect(MockEventSource.instances).toHaveLength(1);
  });

  it("always calls the latest handler (no stale closure)", () => {
    const calls: string[] = [];
    const { rerender } = renderHook(({ tag }) => useLiveSync({ onMedLogged: () => calls.push(tag) }), {
      initialProps: { tag: "first" },
    });
    act(() => latest().open());
    rerender({ tag: "second" });
    act(() => latest().emit({ type: "MED_LOGGED", payload: {}, actor: { id: "1", name: "A", avatar_color: "#000" } }));
    expect(calls).toEqual(["second"]);
    expect(MockEventSource.instances).toHaveLength(1); // rerender did not reconnect
  });

  it("stops reconnecting on auth failure (401) and resumes after PIN login", async () => {
    authStatus = 401;
    const onAuth = vi.fn();
    window.addEventListener("birrcare:auth_required", onAuth);
    renderHook(() => useLiveSync());
    await act(async () => {
      latest().fail(); // stream rejected before ever opening
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/auth/me"), expect.anything());
    expect(onAuth).toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(120_000));
    expect(MockEventSource.instances).toHaveLength(1); // no hammering

    authStatus = 200;
    act(() => {
      window.dispatchEvent(new CustomEvent(AUTH_CHANGED_EVENT));
    });
    expect(MockEventSource.instances).toHaveLength(2);
    expect(live()).toHaveLength(1);
    window.removeEventListener("birrcare:auth_required", onAuth);
  });
});
