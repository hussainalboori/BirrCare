/**
 * Audible cues for live sibling updates.
 *
 * Browsers (iOS Safari especially) only allow audio after a user gesture, so the
 * AudioContext is created/resumed on the first tap anywhere in the app. Until then
 * calls are silent no-ops. Nothing here works while the browser is closed or the
 * phone is locked: that needs Web Push.
 */

let ctx: AudioContext | null = null;
let unlockInstalled = false;

function getContext(): AudioContext | null {
  if (typeof window === "undefined") return null;
  const Ctor = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!Ctor) return null;
  if (!ctx) {
    try {
      ctx = new Ctor();
    } catch {
      return null;
    }
  }
  return ctx;
}

/** Call once on mount: the first tap anywhere unlocks audio for the rest of the session. */
export function installAudioUnlock() {
  if (unlockInstalled || typeof window === "undefined") return;
  unlockInstalled = true;
  const unlock = () => {
    const c = getContext();
    if (c && c.state === "suspended") c.resume().catch(() => {});
  };
  window.addEventListener("pointerdown", unlock, { passive: true });
  window.addEventListener("keydown", unlock);
}

function tone(c: AudioContext, freq: number, start: number, duration: number, volume: number) {
  const osc = c.createOscillator();
  const gain = c.createGain();
  osc.type = "sine";
  osc.frequency.value = freq;
  gain.gain.setValueAtTime(0.0001, start);
  gain.gain.exponentialRampToValueAtTime(volume, start + 0.02);
  gain.gain.exponentialRampToValueAtTime(0.0001, start + duration);
  osc.connect(gain).connect(c.destination);
  osc.start(start);
  osc.stop(start + duration + 0.05);
}

/** "chime": soft two-note cue for a routine update. "crisis": loud repeating alarm. */
export function playAlert(kind: "chime" | "crisis") {
  const c = getContext();
  if (!c || c.state !== "running") return;
  const now = c.currentTime;
  if (kind === "chime") {
    tone(c, 880, now, 0.18, 0.15);
    tone(c, 1320, now + 0.16, 0.25, 0.15);
  } else {
    for (let i = 0; i < 4; i++) {
      tone(c, 960, now + i * 0.5, 0.22, 0.4);
      tone(c, 720, now + i * 0.5 + 0.24, 0.22, 0.4);
    }
  }
}
