import { useState, useCallback, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "@tanstack/react-query";
import {
  listRequestLogs,
  getRequestLog,
  apiClient,
  type RequestLogItem,
  type RequestLogDetail,
} from "../../api/client";
import { FeedbackWidget } from "../../components/FeedbackWidget";

const PAGE_SIZE = 50;

// ---- Phase breakdown types ----

interface PhaseBreakdown {
  pii_scan_ms?: number;
  compliance_check_ms?: number;
  quota_check_ms?: number;
  routing_decision_ms?: number;
  routing_reason?: string;
  queue_wait_ms?: number;
  upstream_ms?: number;
  response_pii_scan_ms?: number;
  compliance_events?: ComplianceEvent[];
}

interface ComplianceEvent {
  phase: string;
  type: "pii_hit" | "bundle_violation" | "quota_exceeded";
  detail: string;
}

interface RequestLogDetailWithPhases extends RequestLogDetail {
  phase_breakdown?: PhaseBreakdown;
}

// ---- Helpers ----

function StatusBadge({ code }: { code: number }) {
  const ok = code >= 200 && code < 300;
  return (
    <span
      className={`px-1.5 py-0.5 rounded text-xs font-medium ${
        ok
          ? "bg-green-900/50 text-green-300"
          : "bg-red-900/50 text-red-300"
      }`}
    >
      {code}
    </span>
  );
}

function MetaRow({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div>
      <p className="text-zinc-500 mb-0.5">{label}</p>
      <p className={`text-zinc-200 truncate ${mono ? "font-mono" : ""}`}>{value || "—"}</p>
    </div>
  );
}

// ---- Waterfall / timeline ----

const PHASE_COLORS: Record<string, string> = {
  pii_scan_ms: "bg-violet-500",
  compliance_check_ms: "bg-orange-500",
  quota_check_ms: "bg-amber-500",
  routing_decision_ms: "bg-blue-500",
  queue_wait_ms: "bg-zinc-500",
  upstream_ms: "bg-indigo-500",
  response_pii_scan_ms: "bg-purple-500",
};

const PHASE_LABELS: Record<string, string> = {
  pii_scan_ms: "PII Scan",
  compliance_check_ms: "Compliance Check",
  quota_check_ms: "Quota Check",
  routing_decision_ms: "Routing Decision",
  queue_wait_ms: "Queue Wait",
  upstream_ms: "Upstream (LLM)",
  response_pii_scan_ms: "Response PII Scan",
};

const PHASE_ORDER = [
  "pii_scan_ms",
  "compliance_check_ms",
  "quota_check_ms",
  "routing_decision_ms",
  "queue_wait_ms",
  "upstream_ms",
  "response_pii_scan_ms",
] as const;

function RequestTimeline({
  totalMs,
  phases,
  log,
}: {
  totalMs: number;
  phases?: PhaseBreakdown;
  log: RequestLogDetailWithPhases;
}) {
  if (!phases) {
    // Fallback: show only total
    return (
      <div className="space-y-2">
        <p className="text-zinc-400 text-xs font-medium mb-2">Request Timeline</p>
        <div className="space-y-1.5">
          <div className="flex items-center gap-2">
            <span className="text-zinc-400 w-36 text-xs shrink-0">Upstream (LLM)</span>
            <div className="flex-1 bg-zinc-800 rounded h-4 overflow-hidden">
              <div className="bg-indigo-500 h-full w-full rounded" />
            </div>
            <span className="text-zinc-400 text-xs w-16 text-right font-mono">{totalMs} ms</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-zinc-500 w-36 text-xs shrink-0">Total</span>
            <span className="text-zinc-300 text-xs font-mono font-medium">{totalMs} ms</span>
          </div>
        </div>
        <p className="text-xs text-zinc-600 mt-2">
          Phase breakdown not available for this request. New requests will include governance breakdown.
        </p>
      </div>
    );
  }

  const phaseValues = PHASE_ORDER.map((key) => ({
    key,
    label: PHASE_LABELS[key],
    color: PHASE_COLORS[key],
    ms: (phases[key as keyof PhaseBreakdown] as number | undefined) ?? 0,
  })).filter((p) => p.ms > 0);

  const knownTotal = phaseValues.reduce((s, p) => s + p.ms, 0);
  const maxMs = Math.max(knownTotal, totalMs, 1);

  const complianceEvents = phases.compliance_events ?? [];

  return (
    <div className="space-y-2">
      <p className="text-zinc-400 text-xs font-medium mb-2">Governance Breakdown</p>
      <div className="space-y-2">
        {phaseValues.map((phase) => {
          const widthPct = (phase.ms / maxMs) * 100;
          const events = complianceEvents.filter((e) => e.phase === phase.key);
          const isRouting = phase.key === "routing_decision_ms";
          return (
            <div key={phase.key} className="space-y-0.5">
              <div className="flex items-center gap-2">
                <span className="text-zinc-400 w-40 text-xs shrink-0 truncate">{phase.label}</span>
                <div className="flex-1 bg-zinc-800 rounded h-4 overflow-hidden relative">
                  <div
                    className={`${phase.color} h-full rounded transition-all`}
                    style={{ width: `${Math.max(widthPct, 0.5)}%` }}
                  />
                  {/* Red compliance event markers */}
                  {events.map((ev, i) => (
                    <div
                      key={i}
                      className="absolute top-0 h-full w-1 bg-red-500"
                      style={{ left: `${Math.max(widthPct - 2, 0)}%` }}
                      title={`${ev.type}: ${ev.detail}`}
                    />
                  ))}
                </div>
                <span className="text-zinc-400 text-xs w-16 text-right font-mono">{phase.ms} ms</span>
              </div>
              {isRouting && phases.routing_reason && (
                <p className="text-xs text-indigo-300 pl-42 ml-[11.5rem]">
                  {phases.routing_reason}
                </p>
              )}
              {events.length > 0 && (
                <div className="pl-[11.5rem] space-y-0.5">
                  {events.map((ev, i) => (
                    <p key={i} className="text-xs text-red-400">
                      {ev.type === "pii_hit" ? "PII hit" :
                       ev.type === "bundle_violation" ? "Bundle violation" :
                       "Quota exceeded"}: {ev.detail}
                    </p>
                  ))}
                </div>
              )}
            </div>
          );
        })}

        {/* Total row */}
        <div className="flex items-center gap-2 pt-1 border-t border-zinc-800">
          <span className="text-zinc-300 w-40 text-xs shrink-0 font-medium">Total</span>
          <div className="flex-1" />
          <span className="text-zinc-300 text-xs w-16 text-right font-mono font-medium">{totalMs} ms</span>
        </div>

        {/* Governance overhead summary */}
        {knownTotal > 0 && (
          <div className="flex items-center gap-1 flex-wrap pt-1">
            <span className="text-xs text-zinc-500">Governance overhead:</span>
            {phaseValues
              .filter((p) => p.key !== "upstream_ms")
              .map((p) => (
                <span
                  key={p.key}
                  className={`px-1.5 py-0.5 rounded text-xs font-mono text-white ${p.color}`}
                >
                  {p.label.split(" ")[0]}: {p.ms}ms
                </span>
              ))}
          </div>
        )}
      </div>

      {log.model && (
        <p className="text-xs text-zinc-500">
          Selected model: <span className="text-zinc-300 font-mono">{log.model}</span>
          {phases.routing_reason && (
            <span className="text-zinc-500"> ({phases.routing_reason})</span>
          )}
        </p>
      )}
    </div>
  );
}

// ---- Eval case creation ----

async function createEvalCaseFromLog(logId: string, rating: 1 | -1) {
  await apiClient.post(`/v1/evals/cases-from-log`, { log_id: logId, quality_label: rating });
}

// ---- Detail panel ----

function DetailPanel({
  id,
  onClose,
}: {
  id: string;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const [evalCreated, setEvalCreated] = useState(false);
  const [evalError, setEvalError] = useState("");
  const [lastRating, setLastRating] = useState<1 | -1 | null>(null);

  const { data, isLoading } = useQuery<RequestLogDetailWithPhases>({
    queryKey: ["request-log-detail", id],
    queryFn: () => getRequestLog(id) as Promise<RequestLogDetailWithPhases>,
  });

  const evalMutation = useMutation({
    mutationFn: () => {
      if (lastRating === null) throw new Error("No rating");
      return createEvalCaseFromLog(id, lastRating);
    },
    onSuccess: () => {
      setEvalCreated(true);
      setEvalError("");
      qc.invalidateQueries({ queryKey: ["eval-suites"] });
    },
    onError: () => {
      setEvalError("Failed to create eval case");
    },
  });

  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      <div
        className="flex-1 bg-black/40"
        onClick={onClose}
        aria-label="Close panel"
      />
      <div className="w-full max-w-2xl bg-zinc-900 border-l border-zinc-800 flex flex-col overflow-hidden">
        <div className="flex items-center justify-between px-5 py-4 border-b border-zinc-800 shrink-0">
          <h2 className="text-sm font-semibold text-zinc-100">Log Detail</h2>
          <button
            onClick={onClose}
            className="text-zinc-400 hover:text-zinc-100 text-lg leading-none"
          >
            x
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-5 space-y-4 text-xs">
          {isLoading ? (
            <p className="text-zinc-500">Loading…</p>
          ) : !data ? (
            <p className="text-zinc-500">Not found.</p>
          ) : (
            <>
              {/* Meta grid */}
              <div className="grid grid-cols-2 gap-2">
                <MetaRow label="ID" value={data.id} mono />
                <MetaRow label="User" value={data.user_id} mono />
                <MetaRow label="Model" value={data.model} />
                <MetaRow label="Provider" value={data.provider} />
                <MetaRow label="Status" value={String(data.status_code)} />
                <MetaRow label="Latency" value={`${data.latency_ms} ms`} />
                <MetaRow label="Prompt tokens" value={String(data.prompt_tokens)} />
                <MetaRow label="Completion tokens" value={String(data.completion_tokens)} />
                <MetaRow label="Cost" value={`$${data.cost_usd.toFixed(6)}`} />
                <MetaRow label="Time" value={new Date(data.created_at).toLocaleString()} />
              </div>

              {/* Timeline / Waterfall */}
              <div className="border border-zinc-800 rounded-lg p-4">
                <RequestTimeline
                  totalMs={data.latency_ms}
                  phases={data.phase_breakdown}
                  log={data}
                />
              </div>

              {/* Feedback */}
              <div className="border border-zinc-800 rounded-lg p-4">
                <p className="text-zinc-400 mb-2 font-medium">Feedback</p>
                <FeedbackWidget
                  logId={data.id}
                  onSubmitted={(rating) => setLastRating(rating)}
                  onCreateEvalCase={() => {
                    if (!evalCreated) evalMutation.mutate();
                  }}
                />
                {evalMutation.isPending && (
                  <p className="text-xs text-zinc-500 mt-1">Creating eval case…</p>
                )}
                {evalCreated && (
                  <p className="text-xs text-green-400 mt-1">Eval case created.</p>
                )}
                {evalError && (
                  <p className="text-xs text-red-400 mt-1">{evalError}</p>
                )}
              </div>

              {/* Request body */}
              <div>
                <p className="text-zinc-400 mb-1 font-medium">Request</p>
                <pre className="bg-zinc-950 rounded p-3 overflow-x-auto text-zinc-300 whitespace-pre-wrap break-words">
                  {JSON.stringify(data.request_body, null, 2)}
                </pre>
              </div>

              {/* Response body */}
              <div>
                <p className="text-zinc-400 mb-1 font-medium">Response</p>
                <pre className="bg-zinc-950 rounded p-3 overflow-x-auto text-zinc-300 whitespace-pre-wrap break-words">
                  {JSON.stringify(data.response_body, null, 2)}
                </pre>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

// ---- Main page ----

export default function LogsPage() {
  const [offset, setOffset] = useState(0);
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [model, setModel] = useState("");
  const [provider, setProvider] = useState("");
  const [status, setStatus] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const handleSearchChange = useCallback((val: string) => {
    setSearch(val);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      setDebouncedSearch(val);
      setOffset(0);
    }, 400);
  }, []);

  const { data, isLoading } = useQuery({
    queryKey: ["request-logs", debouncedSearch, model, provider, status, offset],
    queryFn: () =>
      listRequestLogs({
        search: debouncedSearch || undefined,
        model: model || undefined,
        provider: provider || undefined,
        status: status || undefined,
        limit: PAGE_SIZE,
        offset,
      }),
  });

  const logs = data?.data ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;

  function resetFilters() {
    setSearch("");
    setDebouncedSearch("");
    setModel("");
    setProvider("");
    setStatus("");
    setOffset(0);
  }

  return (
    <div className="p-6 space-y-6">
      {selectedId && (
        <DetailPanel id={selectedId} onClose={() => setSelectedId(null)} />
      )}

      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Request Logs</h1>
        <span className="text-sm text-zinc-400">{total.toLocaleString()} total</span>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap gap-3">
        <input
          type="text"
          placeholder="Search prompts & responses…"
          value={search}
          onChange={(e) => handleSearchChange(e.target.value)}
          className="flex-1 min-w-[200px] bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none focus:border-indigo-500"
        />
        <input
          type="text"
          placeholder="Model"
          value={model}
          onChange={(e) => { setModel(e.target.value); setOffset(0); }}
          className="w-36 bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none focus:border-indigo-500"
        />
        <input
          type="text"
          placeholder="Provider"
          value={provider}
          onChange={(e) => { setProvider(e.target.value); setOffset(0); }}
          className="w-36 bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none focus:border-indigo-500"
        />
        <select
          value={status}
          onChange={(e) => { setStatus(e.target.value); setOffset(0); }}
          className="bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-100 focus:outline-none focus:border-indigo-500"
        >
          <option value="">All statuses</option>
          <option value="success">Success (2xx)</option>
          <option value="error">Error (non-2xx)</option>
        </select>
        {(search || model || provider || status) && (
          <button
            onClick={resetFilters}
            className="px-3 py-2 rounded-lg text-sm text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800 border border-zinc-700"
          >
            Clear
          </button>
        )}
      </div>

      {/* Table */}
      <div className="rounded-lg border border-zinc-800 overflow-x-auto">
        {isLoading ? (
          <p className="p-4 text-zinc-500 text-sm">Loading…</p>
        ) : logs.length === 0 ? (
          <p className="p-4 text-zinc-500 text-sm">No logs found.</p>
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-zinc-400 border-b border-zinc-800 bg-zinc-900">
                <th className="px-3 py-2 whitespace-nowrap">Time</th>
                <th className="px-3 py-2">User</th>
                <th className="px-3 py-2">Model</th>
                <th className="px-3 py-2">Provider</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2 whitespace-nowrap">Latency</th>
                <th className="px-3 py-2">Tokens</th>
                <th className="px-3 py-2">Cost</th>
                <th className="px-3 py-2">Preview</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((log: RequestLogItem) => (
                <tr
                  key={log.id}
                  onClick={() => setSelectedId(log.id)}
                  className="border-b border-zinc-800/50 hover:bg-zinc-800/30 cursor-pointer"
                >
                  <td className="px-3 py-2 text-zinc-400 whitespace-nowrap">
                    {new Date(log.created_at).toLocaleString()}
                  </td>
                  <td className="px-3 py-2 font-mono text-zinc-400 truncate max-w-[100px]">
                    {log.user_id}
                  </td>
                  <td className="px-3 py-2 text-zinc-300 truncate max-w-[120px]">
                    {log.model || "—"}
                  </td>
                  <td className="px-3 py-2 text-zinc-400">
                    {log.provider || "—"}
                  </td>
                  <td className="px-3 py-2">
                    <StatusBadge code={log.status_code} />
                  </td>
                  <td className="px-3 py-2 text-zinc-400 whitespace-nowrap">
                    {log.latency_ms} ms
                  </td>
                  <td className="px-3 py-2 text-zinc-400 whitespace-nowrap">
                    {log.prompt_tokens + log.completion_tokens}
                  </td>
                  <td className="px-3 py-2 text-zinc-400 whitespace-nowrap">
                    ${log.cost_usd.toFixed(4)}
                  </td>
                  <td className="px-3 py-2 text-zinc-500 truncate max-w-[200px]">
                    {log.request_preview || log.response_preview || "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Pagination */}
      {total > PAGE_SIZE && (
        <div className="flex items-center justify-between text-sm text-zinc-400">
          <span>
            Page {currentPage} of {totalPages}
          </span>
          <div className="flex gap-2">
            <button
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
              className="px-3 py-1.5 rounded bg-zinc-800 hover:bg-zinc-700 disabled:opacity-40 disabled:cursor-not-allowed"
            >
              Previous
            </button>
            <button
              disabled={offset + PAGE_SIZE >= total}
              onClick={() => setOffset(offset + PAGE_SIZE)}
              className="px-3 py-1.5 rounded bg-zinc-800 hover:bg-zinc-700 disabled:opacity-40 disabled:cursor-not-allowed"
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
