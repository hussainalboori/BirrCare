"use client";

import { fetchApi } from "./api";

/**
 * Offline outbox (issue #16). A tap that can't reach the server is saved on this
 * phone and shown as "saved on this phone — not sent yet", never as done. It is
 * sent when the connection returns, under the same request id, so a resend after
 * a lost response can't create a duplicate (the backend answers 200).
 *
 * Doses waiting longer than 30 minutes are NOT sent automatically: a sibling may
 * have given it meanwhile, so the caregiver has to confirm.
 */

export type OutboxKind = "dose" | "vital" | "note";

export interface OutboxEntry {
  id: string; // also the client_request_id
  kind: OutboxKind;
  endpoint: string;
  body: Record<string, unknown>;
  created_at: number; // ms
  caregiver_id: string; // only sent while this caregiver is signed in on this phone
  caregiver_name?: string;
  label_ar: string;
  label_en: string;
  dose?: { medication_id: string; slot: string; scheduled_date: string };
  needs_confirm?: boolean;
}

export type OutboxOutcome =
  | { entry: OutboxEntry; outcome: "sent" }
  | { entry: OutboxEntry; outcome: "conflict" | "too_soon" | "failed"; data?: Record<string, unknown>; message?: string };

export const OUTBOX_EVENT = "birrcare:outbox";
export const OUTBOX_RESULT_EVENT = "birrcare:outbox-result";
export const STALE_DOSE_MS = 30 * 60_000;

const DB = "birrcare";
const STORE = "outbox";
let memory: OutboxEntry[] = []; // fallback when IndexedDB is unavailable (private mode)
let idbOk: boolean | null = null;

function openDB(): Promise<IDBDatabase | null> {
  return new Promise((resolve) => {
    if (typeof indexedDB === "undefined") return resolve(null);
    try {
      const req = indexedDB.open(DB, 1);
      req.onupgradeneeded = () => req.result.createObjectStore(STORE, { keyPath: "id" });
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
}

async function tx<T>(mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T> | void): Promise<T | undefined> {
  const db = await openDB();
  if (!db) return undefined;
  return new Promise((resolve, reject) => {
    const t = db.transaction(STORE, mode);
    const req = fn(t.objectStore(STORE));
    t.oncomplete = () => {
      db.close();
      resolve(req ? (req as IDBRequest<T>).result : undefined);
    };
    t.onerror = () => {
      db.close();
      reject(t.error);
    };
  });
}

async function hasIDB(): Promise<boolean> {
  if (idbOk === null) {
    const db = await openDB();
    idbOk = !!db;
    db?.close();
  }
  return idbOk;
}

const changed = () => window.dispatchEvent(new Event(OUTBOX_EVENT));

export async function listOutbox(): Promise<OutboxEntry[]> {
  const all = (await hasIDB()) ? ((await tx<OutboxEntry[]>("readonly", (s) => s.getAll())) ?? []) : [...memory];
  return all.sort((a, b) => a.created_at - b.created_at);
}

async function put(entry: OutboxEntry) {
  if (await hasIDB()) await tx("readwrite", (s) => void s.put(entry));
  else memory = [...memory.filter((e) => e.id !== entry.id), entry];
  changed();
}

export async function removeFromOutbox(id: string) {
  if (await hasIDB()) await tx("readwrite", (s) => void s.delete(id));
  else memory = memory.filter((e) => e.id !== id);
  changed();
}

export function newRequestId(): string {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

/** Save a request that couldn't be sent. Returns the stored entry. */
export async function enqueue(e: Omit<OutboxEntry, "id" | "created_at"> & { id?: string; created_at?: number }): Promise<OutboxEntry> {
  const entry: OutboxEntry = { ...e, id: e.id ?? newRequestId(), created_at: e.created_at ?? Date.now() };
  await put(entry);
  return entry;
}

/** A failed fetch (offline, DNS, dropped Wi-Fi) has no HTTP status. */
export function isNetworkError(err: unknown): boolean {
  return !(err as { status?: number })?.status;
}

let flushing = false;

/**
 * Send what the signed-in caregiver has waiting. Stops at the first network
 * failure (still offline). `confirmed` sends stale doses the caregiver approved.
 */
export async function flushOutbox(caregiverId: string | undefined, confirmed: string[] = []): Promise<void> {
  if (!caregiverId || flushing) return;
  flushing = true;
  try {
    for (const entry of await listOutbox()) {
      if (entry.caregiver_id !== caregiverId) continue;
      const stale = entry.kind === "dose" && Date.now() - entry.created_at > STALE_DOSE_MS;
      if (stale && !confirmed.includes(entry.id)) {
        if (!entry.needs_confirm) await put({ ...entry, needs_confirm: true });
        continue;
      }
      const body = entry.kind === "dose" ? entry.body : { ...entry.body, client_request_id: entry.id };
      try {
        await fetchApi(entry.endpoint, { method: "POST", body: JSON.stringify(body) });
        await removeFromOutbox(entry.id);
        report({ entry, outcome: "sent" });
      } catch (err) {
        if (isNetworkError(err)) break; // still offline: try again later
        const status = (err as { status?: number }).status;
        if (status === 401) break; // signed out: keep it until they sign back in
        const data = (err as { data?: Record<string, unknown> }).data;
        await removeFromOutbox(entry.id);
        report({
          entry,
          outcome: data?.error === "DOSE_ALREADY_LOGGED" ? "conflict" : data?.error === "DOSE_TOO_SOON" ? "too_soon" : "failed",
          data,
          message: (err as Error).message,
        });
      }
    }
  } finally {
    flushing = false;
  }
}

function report(r: OutboxOutcome) {
  window.dispatchEvent(new CustomEvent<OutboxOutcome>(OUTBOX_RESULT_EVENT, { detail: r }));
}
