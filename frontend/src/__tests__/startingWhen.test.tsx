import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { StartingWhen } from "@/components/StartingWhen";

// Issue #57: a schedule change applies from today or tomorrow, asked in plain words.
afterEach(cleanup);

describe("StartingWhen", () => {
  it("offers Today and Tomorrow as large buttons and reports the choice", () => {
    const onChange = vi.fn();
    render(<StartingWhen value="today" onChange={onChange} ar={false} />);
    const today = screen.getByRole("button", { name: "Today" });
    const tomorrow = screen.getByRole("button", { name: "Tomorrow" });
    expect(today.getAttribute("aria-pressed")).toBe("true");
    expect(tomorrow.getAttribute("aria-pressed")).toBe("false");
    for (const b of [today, tomorrow]) expect(b.className).toContain("min-h-12");
    fireEvent.click(tomorrow);
    expect(onChange).toHaveBeenCalledWith("tomorrow");
  });

  it("speaks Arabic", () => {
    render(<StartingWhen value="tomorrow" onChange={() => {}} ar />);
    expect(screen.getByText("متى يبدأ التغيير؟")).toBeTruthy();
    expect(screen.getByRole("button", { name: "غداً" }).getAttribute("aria-pressed")).toBe("true");
  });
});
