import type { ScheduledDose } from "./types";

/** Clock order of a day, with each slot's usual time for doses that have none. */
const SLOT_CLOCK: [string, string][] = [
  ["night", "02:00"],
  ["morning", "08:00"],
  ["afternoon", "14:00"],
  ["evening", "20:00"],
  ["bedtime", "22:30"],
];

const minutes = (hhmm: string) => {
  const [h, m] = hhmm.split(":").map(Number);
  return h * 60 + m;
};

/**
 * The order to list the slots in. Without `from` it is clock order; with a shift
 * start ("HH:MM") the slot of the first dose after that time comes first and the
 * ones before it move to the end. Uses the doses' real times, which can be edited.
 */
export function orderedSlots(doses: ScheduledDose[], from?: string): string[] {
  const clock = SLOT_CLOCK.map(([slot]) => slot);
  if (!from) return clock;
  const start = minutes(from);
  const wait = ([slot, usual]: [string, string]) => {
    const times = doses.filter((d) => d.slot === slot).map((d) => d.time || usual);
    return Math.min(...(times.length ? times : [usual]).map((t) => (minutes(t) - start + 1440) % 1440));
  };
  return [...SLOT_CLOCK].sort((a, b) => wait(a) - wait(b)).map(([slot]) => slot);
}
