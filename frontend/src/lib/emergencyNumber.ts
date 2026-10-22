/** Digits and a leading + only, as setup-env.sh stores it; "" if unusable. Shared by the server layout. */
export function cleanEmergencyNumber(raw: string | undefined): string {
  const v = (raw || "").replace(/[^0-9+]/g, "");
  return /^\+?[0-9]{2,15}$/.test(v) ? v : "";
}
