import { fetchApi } from "@/lib/api";

// Updates offered to the Super Admin, installed only when asked (issue #43).

export interface UpdateCommit {
  sha: string;
  subject: string;
  date: string;
}

export interface UpdateStatus {
  version: string;
  available: {
    current_sha: string;
    target_sha: string;
    ci: string;
    checked_at: string;
    commits: UpdateCommit[] | null;
    changes_database: boolean;
    /** Settings this version needs that the server's .env doesn't have (issue #52). */
    needs_config?: { key: string; question_en: string; question_ar: string }[] | null;
  } | null;
  update_available: boolean;
  result: {
    sha: string;
    status: "success" | "failed" | "rejected" | "needs_config";
    started_at?: string;
    finished_at?: string;
    log_tail?: string;
    missing?: string[] | null;
  } | null;
  policy: { mode: "manual" | "auto"; skipped_sha?: string };
  pending: boolean;
  checker_stale: boolean;
}

/** Fired after the Updates screen changes something, so the header dot refreshes. */
export const UPDATE_STATUS_EVENT = "birrcare:update_status";

export const shortSha = (sha?: string | null) => (sha ? sha.slice(0, 7) : "");

export function fetchUpdateStatus(): Promise<UpdateStatus> {
  return fetchApi<UpdateStatus>("/admin/update/status");
}

export const CHECKER_COMMAND = "systemctl --user enable --now birrcare-deploy.timer birrcare-deploy.path";
