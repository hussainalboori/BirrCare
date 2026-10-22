import { fetchApi } from "@/lib/api";

// Encrypted off-site backups made by scripts/backup.sh on the server (issue #51).

export interface BackupDestination {
  name: string;
  last_success_at?: string;
  snapshot_id?: string;
  size_bytes?: number;
}

export type BackupReason = "ok" | "not_set_up" | "stale" | "failed" | "restore_failed" | "invalid_grant";

export interface BackupStatus {
  configured: boolean;
  stale: boolean;
  reason: BackupReason;
  pending: boolean;
  status: {
    last_success_at?: string;
    last_attempt_at?: string;
    status?: "success" | "failed" | "running";
    destinations?: BackupDestination[] | null;
    last_restore_test_at?: string;
    last_restore_test_ok?: boolean;
    restore_test_error?: string;
    error?: string;
    error_code?: string;
  } | null;
}

export function fetchBackupStatus(): Promise<BackupStatus> {
  return fetchApi<BackupStatus>("/admin/backup/status");
}

/** Needs the Super Admin's attention (red / amber on the card, dot on the menu). */
export const backupNeedsAttention = (s: BackupStatus | null) =>
  !!s && s.reason !== "ok" && !s.pending;
