"use client";

import React, { useState, useEffect, useCallback } from "react";
import { useLanguage } from "@/context/LanguageContext";
import {
  UserLogEntry,
  SystemLogEntry,
  AccessAuditEntry,
  fetchUserLogs,
  fetchSystemLogs,
  fetchAccessAudits,
  getExportLogsUrl,
} from "@/lib/logs";
import { Ltr } from "./Ltr";
import {
  X,
  Search,
  RefreshCw,
  Download,
  AlertTriangle,
  Info,
  AlertCircle,
  FileText,
  Activity,
  Server,
  ShieldCheck,
  ChevronLeft,
  ChevronRight,
  ChevronDown,
  ChevronUp,
} from "lucide-react";

interface AdminLogsModalProps {
  isOpen: boolean;
  onClose: () => void;
}

type StreamTab = "user" | "system" | "audits";

export function AdminLogsModal({ isOpen, onClose }: AdminLogsModalProps) {
  const { locale } = useLanguage();
  const ar = locale === "ar";

  const [tab, setTab] = useState<StreamTab>("user");
  const [search, setSearch] = useState("");
  const [level, setLevel] = useState("");
  const [page, setPage] = useState(0);
  const pageSize = 25;

  const [userLogs, setUserLogs] = useState<UserLogEntry[]>([]);
  const [systemLogs, setSystemLogs] = useState<SystemLogEntry[]>([]);
  const [auditLogs, setAuditLogs] = useState<AccessAuditEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const loadData = useCallback(async () => {
    if (!isOpen) return;
    setLoading(true);
    setError(null);
    try {
      const filter = {
        query: search.trim() || undefined,
        level: level || undefined,
        limit: pageSize,
        offset: page * pageSize,
      };

      if (tab === "user") {
        const res = await fetchUserLogs(filter);
        setUserLogs(res.entries || []);
        setTotal(res.total || 0);
      } else if (tab === "system") {
        const res = await fetchSystemLogs(filter);
        setSystemLogs(res.entries || []);
        setTotal(res.total || 0);
      } else {
        const res = await fetchAccessAudits(filter);
        setAuditLogs(res.entries || []);
        setTotal(res.total || 0);
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg || (ar ? "فشل تحميل السجلات" : "Failed to load logs"));
    } finally {
      setLoading(false);
    }
  }, [isOpen, tab, search, level, page, ar]);

  useEffect(() => {
    setPage(0);
    setExpandedId(null);
  }, [tab, search, level]);

  useEffect(() => {
    if (isOpen) {
      loadData();
    }
  }, [isOpen, loadData]);

  if (!isOpen) return null;

  const totalPages = Math.ceil(total / pageSize);
  const startItem = total === 0 ? 0 : page * pageSize + 1;
  const endItem = Math.min((page + 1) * pageSize, total);

  const renderBadge = (lvl: string) => {
    const l = lvl.toLowerCase();
    if (l === "error" || l === "fatal") {
      return (
        <span className="inline-flex items-center gap-1 rounded-md bg-rose-50 px-2 py-0.5 text-xs font-bold text-rose-700 border border-rose-200 dark:bg-rose-950/60 dark:text-rose-300 dark:border-rose-900">
          <AlertCircle className="h-3 w-3 shrink-0" />
          <span className="uppercase">{lvl}</span>
        </span>
      );
    }
    if (l === "warn") {
      return (
        <span className="inline-flex items-center gap-1 rounded-md bg-amber-50 px-2 py-0.5 text-xs font-bold text-amber-700 border border-amber-200 dark:bg-amber-950/60 dark:text-amber-300 dark:border-amber-900">
          <AlertTriangle className="h-3 w-3 shrink-0" />
          <span className="uppercase">{lvl}</span>
        </span>
      );
    }
    return (
      <span className="inline-flex items-center gap-1 rounded-md bg-sky-50 px-2 py-0.5 text-xs font-bold text-sky-700 border border-sky-200 dark:bg-sky-950/60 dark:text-sky-300 dark:border-sky-900">
        <Info className="h-3 w-3 shrink-0" />
        <span className="uppercase">{lvl}</span>
      </span>
    );
  };

  const handleExport = (format: "json" | "csv") => {
    if (tab === "audits") return;
    const url = getExportLogsUrl(tab, format, {
      query: search.trim() || undefined,
      level: level || undefined,
    });
    window.open(url, "_blank");
  };

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-900/60 p-0 backdrop-blur-xs sm:items-center sm:p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-label={ar ? "سجلات النظام والعمليات" : "System & User Logs"}
        data-testid="admin-logs-modal"
        className="relative flex max-h-[92dvh] w-full max-w-4xl flex-col rounded-t-3xl border border-slate-200 bg-white shadow-2xl sm:rounded-3xl dark:border-slate-800 dark:bg-slate-900"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-slate-200 p-4 sm:p-5 dark:border-slate-800">
          <div className="flex items-center gap-2.5">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-indigo-50 text-indigo-700 dark:bg-indigo-950/70 dark:text-indigo-300">
              <FileText className="h-5 w-5" />
            </div>
            <div>
              <h2 className="text-lg font-black text-slate-900 dark:text-white">
                {ar ? "سجلات النظام والعمليات" : "System & User Logs"}
              </h2>
              <p className="text-xs font-medium text-slate-500 dark:text-slate-400">
                {ar ? "متاح للمشرف العام فقط مع تدقيق الوصول" : "Super Admin only • Access is audited"}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={ar ? "إغلاق" : "Close"}
            className="flex min-h-12 min-w-12 items-center justify-center rounded-xl border border-slate-200 text-slate-600 hover:bg-slate-50 active:scale-95 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Stream Tabs */}
        <div className="flex border-b border-slate-200 bg-slate-50 px-4 pt-2 sm:px-5 dark:border-slate-800 dark:bg-slate-950/40">
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => setTab("user")}
              className={`flex min-h-12 items-center gap-2 border-b-2 px-3 py-2 text-sm font-bold transition-colors ${
                tab === "user"
                  ? "border-indigo-600 text-indigo-600 dark:border-indigo-400 dark:text-indigo-400"
                  : "border-transparent text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white"
              }`}
            >
              <Activity className="h-4 w-4" />
              <span>{ar ? "نشاط وأخطاء المستخدمين" : "User Activity & Errors"}</span>
            </button>
            <button
              type="button"
              onClick={() => setTab("system")}
              className={`flex min-h-12 items-center gap-2 border-b-2 px-3 py-2 text-sm font-bold transition-colors ${
                tab === "system"
                  ? "border-indigo-600 text-indigo-600 dark:border-indigo-400 dark:text-indigo-400"
                  : "border-transparent text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white"
              }`}
            >
              <Server className="h-4 w-4" />
              <span>{ar ? "أحداث النظام والخادم" : "System Events"}</span>
            </button>
            <button
              type="button"
              onClick={() => setTab("audits")}
              className={`flex min-h-12 items-center gap-2 border-b-2 px-3 py-2 text-sm font-bold transition-colors ${
                tab === "audits"
                  ? "border-indigo-600 text-indigo-600 dark:border-indigo-400 dark:text-indigo-400"
                  : "border-transparent text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white"
              }`}
            >
              <ShieldCheck className="h-4 w-4" />
              <span>{ar ? "سجل تدقيق الوصول" : "Access Audits"}</span>
            </button>
          </div>
        </div>

        {/* Toolbar: Search, Level filter, Refresh, Export */}
        <div className="flex flex-wrap items-center justify-between gap-2.5 border-b border-slate-200 p-3 sm:px-5 dark:border-slate-800">
          <div className="flex flex-1 flex-wrap items-center gap-2">
            <div className="relative min-w-[200px] flex-1 max-w-sm">
              <Search className="pointer-events-none absolute start-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
              <input
                type="text"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={ar ? "بحث في السجلات..." : "Search logs..."}
                className="min-h-12 w-full rounded-xl border border-slate-300 bg-white ps-9 pe-3 text-base text-slate-800 placeholder-slate-400 focus:border-indigo-500 focus:outline-hidden dark:border-slate-700 dark:bg-slate-800 dark:text-white dark:placeholder-slate-500"
              />
              {search && (
                <button
                  type="button"
                  onClick={() => setSearch("")}
                  className="absolute end-2.5 top-1/2 -translate-y-1/2 text-xs font-bold text-slate-400 hover:text-slate-600 dark:hover:text-slate-200"
                >
                  ✕
                </button>
              )}
            </div>

            {tab !== "audits" && (
              <select
                value={level}
                onChange={(e) => setLevel(e.target.value)}
                className="min-h-12 rounded-xl border border-slate-300 bg-white px-3 text-base font-semibold text-slate-700 focus:border-indigo-500 focus:outline-hidden dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
              >
                <option value="">{ar ? "كل المستويات" : "All Levels"}</option>
                <option value="info">INFO</option>
                <option value="warn">WARN</option>
                <option value="error">ERROR</option>
                {tab === "system" && <option value="fatal">FATAL</option>}
              </select>
            )}

            <button
              type="button"
              onClick={loadData}
              disabled={loading}
              className="flex min-h-12 items-center gap-1.5 rounded-xl border border-slate-200 px-3 text-sm font-bold text-slate-700 hover:bg-slate-50 active:scale-95 disabled:opacity-50 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
            >
              <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
              <span className="hidden sm:inline">{ar ? "تحديث" : "Refresh"}</span>
            </button>
          </div>

          {tab !== "audits" && (
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={() => handleExport("csv")}
                className="flex min-h-12 items-center gap-1.5 rounded-xl border border-slate-200 px-3 text-sm font-bold text-slate-700 hover:bg-slate-50 active:scale-95 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
              >
                <Download className="h-4 w-4" />
                <span>CSV</span>
              </button>
              <button
                type="button"
                onClick={() => handleExport("json")}
                className="flex min-h-12 items-center gap-1.5 rounded-xl border border-slate-200 px-3 text-sm font-bold text-slate-700 hover:bg-slate-50 active:scale-95 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
              >
                <Download className="h-4 w-4" />
                <span>JSON</span>
              </button>
            </div>
          )}
        </div>

        {/* Content Table / Card List */}
        <div className="flex-1 overflow-y-auto p-4 sm:p-5">
          {error && (
            <div className="mb-4 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm font-semibold text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/60 dark:text-rose-300">
              {error}
            </div>
          )}

          {loading ? (
            <div className="py-16 text-center text-sm font-bold text-slate-500 dark:text-slate-400">
              <RefreshCw className="mx-auto mb-2 h-6 w-6 animate-spin text-indigo-500" />
              <span>{ar ? "جارٍ جلب السجلات..." : "Loading log stream..."}</span>
            </div>
          ) : (tab === "user" && userLogs.length === 0) ||
            (tab === "system" && systemLogs.length === 0) ||
            (tab === "audits" && auditLogs.length === 0) ? (
            <div className="py-16 text-center text-sm font-bold text-slate-400 dark:text-slate-500">
              {ar ? "لا توجد سجلات مطابقة للمعايير المحددة" : "No logs match the selected filter criteria"}
            </div>
          ) : (
            <div className="space-y-2">
              {tab === "user" &&
                userLogs.map((entry) => {
                  const isExpanded = expandedId === entry.id;
                  return (
                    <div
                      key={entry.id}
                      className="rounded-2xl border border-slate-200 bg-white transition-colors hover:border-slate-300 dark:border-slate-800 dark:bg-slate-900/70 dark:hover:border-slate-700"
                    >
                      <button
                        type="button"
                        onClick={() => setExpandedId(isExpanded ? null : entry.id)}
                        className="flex min-h-12 w-full items-start justify-between gap-3 p-3 text-start"
                      >
                        <div className="flex flex-1 flex-col gap-1 sm:flex-row sm:items-center sm:gap-3">
                          <div className="flex items-center gap-2">
                            {renderBadge(entry.level)}
                            {entry.status_code ? (
                              <span
                                className={`rounded px-1.5 py-0.5 text-xs font-mono font-bold ${
                                  entry.status_code >= 400
                                    ? "bg-rose-100 text-rose-800 dark:bg-rose-950 dark:text-rose-300"
                                    : "bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300"
                                }`}
                              >
                                <Ltr>{entry.status_code}</Ltr>
                              </span>
                            ) : null}
                          </div>
                          <div className="flex-1">
                            <span className="font-bold text-sm text-slate-900 dark:text-white">
                              {entry.action}
                            </span>
                            <p className="line-clamp-1 text-xs text-slate-600 dark:text-slate-300">
                              {entry.message}
                            </p>
                          </div>
                          {entry.caregiver_name && (
                            <span className="shrink-0 text-xs font-semibold text-slate-500 dark:text-slate-400">
                              <bdi>{entry.caregiver_name}</bdi>
                            </span>
                          )}
                        </div>
                        <div className="flex shrink-0 items-center gap-2 text-xs text-slate-400">
                          <Ltr>{new Date(entry.created_at).toLocaleTimeString()}</Ltr>
                          {isExpanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                        </div>
                      </button>

                      {isExpanded && (
                        <div className="border-t border-slate-100 bg-slate-50 p-3 text-xs dark:border-slate-800 dark:bg-slate-950/60">
                          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                            <div>
                              <span className="font-bold text-slate-500 dark:text-slate-400">{ar ? "المعرّف:" : "ID:"} </span>
                              <span className="font-mono text-slate-800 dark:text-slate-200"><Ltr>{entry.id}</Ltr></span>
                            </div>
                            <div>
                              <span className="font-bold text-slate-500 dark:text-slate-400">{ar ? "الوقت الكامل:" : "Timestamp:"} </span>
                              <span className="text-slate-800 dark:text-slate-200"><Ltr>{entry.created_at}</Ltr></span>
                            </div>
                            {entry.ip_address && (
                              <div>
                                <span className="font-bold text-slate-500 dark:text-slate-400">{ar ? "عنوان IP:" : "IP Address:"} </span>
                                <span className="font-mono text-slate-800 dark:text-slate-200"><Ltr>{entry.ip_address}</Ltr></span>
                              </div>
                            )}
                            {entry.resource && (
                              <div>
                                <span className="font-bold text-slate-500 dark:text-slate-400">{ar ? "المسار:" : "Resource:"} </span>
                                <span className="font-mono text-slate-800 dark:text-slate-200"><Ltr>{entry.resource}</Ltr></span>
                              </div>
                            )}
                          </div>
                          {entry.user_agent && (
                            <div className="mt-2 text-slate-500 dark:text-slate-400">
                              <span className="font-bold">{ar ? "المتصفح:" : "User Agent:"} </span>
                              <span className="font-mono break-all"><Ltr>{entry.user_agent}</Ltr></span>
                            </div>
                          )}
                          {entry.metadata && Object.keys(entry.metadata).length > 0 && (
                            <div className="mt-2">
                              <span className="font-bold text-slate-500 dark:text-slate-400">{ar ? "البيانات الإضافية:" : "Metadata:"}</span>
                              <pre className="mt-1 max-h-32 overflow-x-auto rounded-lg bg-slate-200 p-2 font-mono text-xs dark:bg-slate-900 text-slate-800 dark:text-slate-200">
                                {JSON.stringify(entry.metadata, null, 2)}
                              </pre>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}

              {tab === "system" &&
                systemLogs.map((entry) => {
                  const isExpanded = expandedId === entry.id;
                  return (
                    <div
                      key={entry.id}
                      className="rounded-2xl border border-slate-200 bg-white transition-colors hover:border-slate-300 dark:border-slate-800 dark:bg-slate-900/70 dark:hover:border-slate-700"
                    >
                      <button
                        type="button"
                        onClick={() => setExpandedId(isExpanded ? null : entry.id)}
                        className="flex min-h-12 w-full items-start justify-between gap-3 p-3 text-start"
                      >
                        <div className="flex flex-1 flex-col gap-1 sm:flex-row sm:items-center sm:gap-3">
                          <div className="flex items-center gap-2">
                            {renderBadge(entry.level)}
                            <span className="rounded bg-indigo-50 px-1.5 py-0.5 text-xs font-mono font-bold text-indigo-700 dark:bg-indigo-950 dark:text-indigo-300">
                              {entry.subsystem}
                            </span>
                          </div>
                          <div className="flex-1">
                            <span className="font-bold text-sm text-slate-900 dark:text-white">
                              {entry.event}
                            </span>
                            <p className="line-clamp-1 text-xs text-slate-600 dark:text-slate-300">
                              {entry.message}
                            </p>
                          </div>
                        </div>
                        <div className="flex shrink-0 items-center gap-2 text-xs text-slate-400">
                          <Ltr>{new Date(entry.created_at).toLocaleTimeString()}</Ltr>
                          {isExpanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                        </div>
                      </button>

                      {isExpanded && (
                        <div className="border-t border-slate-100 bg-slate-50 p-3 text-xs dark:border-slate-800 dark:bg-slate-950/60">
                          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                            <div>
                              <span className="font-bold text-slate-500 dark:text-slate-400">{ar ? "المعرّف:" : "ID:"} </span>
                              <span className="font-mono text-slate-800 dark:text-slate-200"><Ltr>{entry.id}</Ltr></span>
                            </div>
                            <div>
                              <span className="font-bold text-slate-500 dark:text-slate-400">{ar ? "الوقت الكامل:" : "Timestamp:"} </span>
                              <span className="text-slate-800 dark:text-slate-200"><Ltr>{entry.created_at}</Ltr></span>
                            </div>
                          </div>
                          {entry.error_details && (
                            <div className="mt-2 rounded-lg border border-rose-200 bg-rose-50 p-2 dark:border-rose-900 dark:bg-rose-950/60">
                              <span className="font-bold text-rose-700 dark:text-rose-300">{ar ? "تفاصيل الخطأ:" : "Error Details:"} </span>
                              <pre className="mt-1 font-mono text-rose-800 dark:text-rose-200">{entry.error_details}</pre>
                            </div>
                          )}
                          {entry.metadata && Object.keys(entry.metadata).length > 0 && (
                            <div className="mt-2">
                              <span className="font-bold text-slate-500 dark:text-slate-400">{ar ? "البيانات الإضافية:" : "Metadata:"}</span>
                              <pre className="mt-1 max-h-32 overflow-x-auto rounded-lg bg-slate-200 p-2 font-mono text-xs dark:bg-slate-900 text-slate-800 dark:text-slate-200">
                                {JSON.stringify(entry.metadata, null, 2)}
                              </pre>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}

              {tab === "audits" &&
                auditLogs.map((entry) => (
                  <div
                    key={entry.id}
                    className="flex min-h-12 items-center justify-between gap-3 rounded-2xl border border-slate-200 bg-white p-3 text-xs dark:border-slate-800 dark:bg-slate-900/70"
                  >
                    <div className="flex flex-1 flex-col gap-1 sm:flex-row sm:items-center sm:gap-3">
                      <span className="font-bold text-sm text-slate-800 dark:text-white">
                        <bdi>{entry.caregiver_name}</bdi>
                      </span>
                      <span className="rounded bg-purple-50 px-2 py-0.5 font-bold uppercase text-purple-700 border border-purple-200 dark:bg-purple-950/60 dark:text-purple-300 dark:border-purple-800">
                        {entry.operation}
                      </span>
                      <span className="font-medium text-slate-600 dark:text-slate-300">
                        {ar ? "المسار:" : "Stream:"} <span className="font-bold">{entry.stream}</span>
                        {entry.records_count != null && (
                          <span className="ms-1.5 opacity-80">
                            ({ar ? "سجلات: " : "count: "}<Ltr>{entry.records_count}</Ltr>)
                          </span>
                        )}
                      </span>
                    </div>
                    <div className="text-end text-slate-400 shrink-0">
                      <div><Ltr>{new Date(entry.created_at).toLocaleDateString()}</Ltr></div>
                      <div><Ltr>{new Date(entry.created_at).toLocaleTimeString()}</Ltr></div>
                    </div>
                  </div>
                ))}
            </div>
          )}
        </div>

        {/* Footer & Pagination */}
        <div className="flex items-center justify-between border-t border-slate-200 p-4 sm:px-5 dark:border-slate-800">
          <div className="text-xs font-semibold text-slate-500 dark:text-slate-400">
            {ar ? (
              <>
                عرض <Ltr>{startItem}</Ltr> - <Ltr>{endItem}</Ltr> من أصل <Ltr>{total}</Ltr>
              </>
            ) : (
              <>
                Showing <Ltr>{startItem}</Ltr>-<Ltr>{endItem}</Ltr> of <Ltr>{total}</Ltr>
              </>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={page === 0 || loading}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              className="flex min-h-12 min-w-12 items-center justify-center rounded-xl border border-slate-200 text-slate-700 hover:bg-slate-50 disabled:opacity-40 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
              aria-label={ar ? "الصفحة السابقة" : "Previous page"}
            >
              {ar ? <ChevronRight className="h-5 w-5" /> : <ChevronLeft className="h-5 w-5" />}
            </button>
            <span className="px-2 text-xs font-bold text-slate-700 dark:text-slate-200">
              <Ltr>{page + 1}</Ltr> / <Ltr>{Math.max(1, totalPages)}</Ltr>
            </span>
            <button
              type="button"
              disabled={page >= totalPages - 1 || loading}
              onClick={() => setPage((p) => p + 1)}
              className="flex min-h-12 min-w-12 items-center justify-center rounded-xl border border-slate-200 text-slate-700 hover:bg-slate-50 disabled:opacity-40 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
              aria-label={ar ? "الصفحة التالية" : "Next page"}
            >
              {ar ? <ChevronLeft className="h-5 w-5" /> : <ChevronRight className="h-5 w-5" />}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
