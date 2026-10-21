/** Issue #20: "about 3 days left" / "يكفي نحو 3 أيام", with Arabic counting rules. */
export function daysLeftText(daysLeft: number | null | undefined, ar: boolean): string {
  const d = Math.floor(Number(daysLeft) || 0);
  if (d < 1) return ar ? "نفد أو سينفد اليوم" : "out or runs out today";
  if (!ar) return `about ${d} day${d === 1 ? "" : "s"} left`;
  if (d === 1) return "يكفي يوماً واحداً تقريباً";
  if (d === 2) return "يكفي نحو يومين";
  return `يكفي نحو ${d} ${d <= 10 ? "أيام" : "يوماً"}`;
}
