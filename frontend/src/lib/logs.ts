import { fetchApi } from "./api";

export interface UserLogEntry {
  id: string;
  caregiver_id?: string;
  caregiver_name?: string;
  level: "debug" | "info" | "warn" | "error";
  action: string;
  resource?: string;
  status_code?: number;
  ip_address?: string;
  user_agent?: string;
  message: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface SystemLogEntry {
  id: string;
  level: "debug" | "info" | "warn" | "error" | "fatal";
  subsystem: string;
  event: string;
  message: string;
  error_details?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface AccessAuditEntry {
  id: string;
  caregiver_id: string;
  caregiver_name: string;
  stream: string;
  operation: string;
  filter_query?: string;
  records_count: number;
  ip_address?: string;
  user_agent?: string;
  created_at: string;
}

export interface LogQueryFilter {
  query?: string;
  level?: string;
  caregiver_id?: string;
  subsystem?: string;
  action?: string;
  event?: string;
  from?: string;
  to?: string;
  limit?: number;
  offset?: number;
}

export interface PaginatedLogsResponse<T> {
  entries: T[];
  total: number;
  limit: number;
  offset: number;
}

function buildQuery(filter?: LogQueryFilter): string {
  if (!filter) return "";
  const params = new URLSearchParams();
  if (filter.query) params.set("query", filter.query);
  if (filter.level) params.set("level", filter.level);
  if (filter.caregiver_id) params.set("caregiver_id", filter.caregiver_id);
  if (filter.subsystem) params.set("subsystem", filter.subsystem);
  if (filter.action) params.set("action", filter.action);
  if (filter.event) params.set("event", filter.event);
  if (filter.from) params.set("from", filter.from);
  if (filter.to) params.set("to", filter.to);
  if (filter.limit) params.set("limit", String(filter.limit));
  if (filter.offset != null) params.set("offset", String(filter.offset));
  const q = params.toString();
  return q ? `?${q}` : "";
}

export async function fetchUserLogs(filter?: LogQueryFilter): Promise<PaginatedLogsResponse<UserLogEntry>> {
  return fetchApi<PaginatedLogsResponse<UserLogEntry>>(`/admin/logs/user${buildQuery(filter)}`);
}

export async function fetchSystemLogs(filter?: LogQueryFilter): Promise<PaginatedLogsResponse<SystemLogEntry>> {
  return fetchApi<PaginatedLogsResponse<SystemLogEntry>>(`/admin/logs/system${buildQuery(filter)}`);
}

export async function fetchAccessAudits(filter?: LogQueryFilter): Promise<PaginatedLogsResponse<AccessAuditEntry>> {
  return fetchApi<PaginatedLogsResponse<AccessAuditEntry>>(`/admin/logs/access-audits${buildQuery(filter)}`);
}

export function getExportLogsUrl(stream: "user" | "system", format: "json" | "csv", filter?: LogQueryFilter): string {
  const params = new URLSearchParams();
  params.set("stream", stream);
  params.set("format", format);
  if (filter?.query) params.set("query", filter.query);
  if (filter?.level) params.set("level", filter.level);
  if (filter?.caregiver_id) params.set("caregiver_id", filter.caregiver_id);
  if (filter?.subsystem) params.set("subsystem", filter.subsystem);
  if (filter?.action) params.set("action", filter.action);
  if (filter?.event) params.set("event", filter.event);
  if (filter?.from) params.set("from", filter.from);
  if (filter?.to) params.set("to", filter.to);
  return `/api/v1/admin/logs/export?${params.toString()}`;
}
