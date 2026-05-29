import { useState, useEffect, useRef, useCallback } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";
import type { PromptTemplate } from "../../api/client";

// ---- Types ----

interface EvalSuite {
  id: string;
  name: string;
  prompt_name: string;
  created_at: string;
}

interface EvalCase {
  id: string;
  suite_id: string;
  input_vars: Record<string, unknown>;
  expected_output?: string;
  expected_contains?: string[];
  score_method: string;
  created_at: string;
}

interface EvalRun {
  id: string;
  suite_id: string;
  prompt_version: number;
  model: string;
  status: string;
  total_cases: number;
  passed_cases: number;
  failed_cases: number;
  score_pct?: number | null;
  started_at?: string | null;
  completed_at?: string | null;
  created_at: string;
}

interface EvalResult {
  id: string;
  run_id: string;
  case_id: string;
  actual_output?: string;
  passed: boolean;
  score?: number;
  latency_ms: number;
  error?: string;
  created_at: string;
}

interface SuiteDetail {
  suite: EvalSuite;
  cases: EvalCase[];
  runs: EvalRun[];
}

interface RunTrend {
  run_id: string;
  prompt_version: number;
  model: string;
  score_pct: number;
  passed_cases: number;
  total_cases: number;
  created_at: string;
}

interface BenchmarkMeta {
  id: string;
  industry: string;
  name: string;
  description: string;
  case_count: number;
}

interface CompareRunEntry {
  model: string;
  run_id: string;
}

// ---- API helpers ----

const listSuites = () =>
  apiClient.get<{ object: string; data: EvalSuite[] }>("/v1/evals/suites").then((r) => r.data.data ?? []);

const getSuite = (id: string) =>
  apiClient.get<SuiteDetail>(`/v1/evals/suites/${id}`).then((r) => r.data);

const createSuite = (name: string, prompt_name: string) =>
  apiClient.post<EvalSuite>("/v1/evals/suites", { name, prompt_name }).then((r) => r.data);

const addCase = (
  suiteId: string,
  payload: {
    input_vars: Record<string, string>;
    expected_output: string;
    expected_contains: string[];
    score_method: string;
  }
) => apiClient.post<EvalCase>(`/v1/evals/suites/${suiteId}/cases`, payload).then((r) => r.data);

const triggerRun = (suiteId: string, model: string, prompt_version: number) =>
  apiClient
    .post<{ run_id: string; status: string }>(`/v1/evals/suites/${suiteId}/run`, {
      model,
      prompt_version,
    })
    .then((r) => r.data);

const getRun = (runId: string) =>
  apiClient.get<EvalRun>(`/v1/evals/runs/${runId}`).then((r) => r.data);

const getRunResults = (runId: string) =>
  apiClient
    .get<{ object: string; data: EvalResult[] }>(`/v1/evals/runs/${runId}/results`)
    .then((r) => r.data.data ?? []);

const getTrends = (suiteId: string, limit = 20) =>
  apiClient
    .get<{ object: string; data: RunTrend[] }>(`/v1/evals/suites/${suiteId}/trends?limit=${limit}`)
    .then((r) => r.data.data ?? []);

const compareModels = (suiteId: string, models: string[], prompt_version: number | null) =>
  apiClient
    .post<{ runs: CompareRunEntry[] }>(`/v1/evals/suites/${suiteId}/compare`, { models, prompt_version })
    .then((r) => r.data.runs);

const listBenchmarks = () =>
  apiClient
    .get<{ object: string; data: BenchmarkMeta[] }>("/v1/evals/benchmarks")
    .then((r) => r.data.data ?? []);

const importBenchmark = (benchmarkId: string, suite_name: string, model: string) =>
  apiClient
    .post<{ suite_id: string; suite_name: string; model: string; case_count: number }>(
      `/v1/evals/benchmarks/${benchmarkId}/import`,
      { suite_name, model }
    )
    .then((r) => r.data);

// ---- Helpers ----

function scoreBadge(scorePct?: number | null) {
  if (scorePct == null) return <Badge variant="secondary">--</Badge>;
  if (scorePct >= 80) return <Badge className="bg-green-700 text-white">{scorePct.toFixed(1)}%</Badge>;
  if (scorePct >= 60) return <Badge className="bg-yellow-600 text-white">{scorePct.toFixed(1)}%</Badge>;
  return <Badge className="bg-red-700 text-white">{scorePct.toFixed(1)}%</Badge>;
}

function statusBadge(status: string) {
  const map: Record<string, string> = {
    pending: "bg-zinc-600",
    running: "bg-blue-600",
    completed: "bg-green-700",
    failed: "bg-red-700",
  };
  return (
    <Badge className={`${map[status] ?? "bg-zinc-600"} text-white`}>{status}</Badge>
  );
}

const INDUSTRY_COLORS: Record<string, string> = {
  healthcare: "bg-blue-700",
  legal: "bg-purple-700",
  coding: "bg-emerald-700",
  customer_support: "bg-orange-700",
  general: "bg-zinc-600",
};

function industryBadge(industry: string) {
  const cls = INDUSTRY_COLORS[industry] ?? "bg-zinc-600";
  return <Badge className={`${cls} text-white text-xs`}>{industry}</Badge>;
}

// ---- Sparkline chart ----

function TrendSparkline({ trends }: { trends: RunTrend[] }) {
  if (trends.length < 2) {
    return (
      <p className="text-zinc-500 text-xs text-center py-4">
        Need at least 2 completed runs to display trend chart.
      </p>
    );
  }

  const W = 480;
  const H = 80;
  const PAD = { top: 8, right: 12, bottom: 20, left: 32 };
  const innerW = W - PAD.left - PAD.right;
  const innerH = H - PAD.top - PAD.bottom;

  const scores = trends.map((t) => t.score_pct);
  const minScore = Math.max(0, Math.min(...scores) - 5);
  const maxScore = Math.min(100, Math.max(...scores) + 5);
  const range = maxScore - minScore || 1;

  const xScale = (i: number) => PAD.left + (i / (trends.length - 1)) * innerW;
  const yScale = (v: number) => PAD.top + innerH - ((v - minScore) / range) * innerH;

  const points = trends.map((t, i) => `${xScale(i)},${yScale(t.score_pct)}`).join(" ");

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ maxHeight: 100 }}>
      {/* Y-axis labels */}
      <text x={PAD.left - 4} y={yScale(maxScore) + 4} textAnchor="end" fill="#71717a" fontSize="9">
        {maxScore.toFixed(0)}%
      </text>
      <text x={PAD.left - 4} y={yScale(minScore) + 4} textAnchor="end" fill="#71717a" fontSize="9">
        {minScore.toFixed(0)}%
      </text>

      {/* Guideline at 80% */}
      {maxScore >= 80 && minScore <= 80 && (
        <line
          x1={PAD.left}
          x2={W - PAD.right}
          y1={yScale(80)}
          y2={yScale(80)}
          stroke="#3f3f46"
          strokeDasharray="3 3"
        />
      )}

      {/* Trend polyline */}
      <polyline points={points} fill="none" stroke="#6366f1" strokeWidth="1.5" strokeLinejoin="round" />

      {/* Data points */}
      {trends.map((t, i) => {
        const cx = xScale(i);
        const cy = yScale(t.score_pct);
        const color = t.score_pct >= 80 ? "#16a34a" : t.score_pct >= 60 ? "#ca8a04" : "#dc2626";
        return (
          <g key={t.run_id}>
            <circle cx={cx} cy={cy} r={3} fill={color} />
            {/* X-axis date label — show first, last and every 5th */}
            {(i === 0 || i === trends.length - 1 || i % 5 === 0) && (
              <text x={cx} y={H - 4} textAnchor="middle" fill="#52525b" fontSize="8">
                {new Date(t.created_at).toLocaleDateString(undefined, { month: "short", day: "numeric" })}
              </text>
            )}
          </g>
        );
      })}
    </svg>
  );
}

// ---- Run poller ----

function useRunPoller(runId: string | null, onDone: (run: EvalRun) => void) {
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (!runId) return;
    intervalRef.current = setInterval(async () => {
      try {
        const run = await getRun(runId);
        if (run.status === "completed" || run.status === "failed") {
          if (intervalRef.current) clearInterval(intervalRef.current);
          onDone(run);
        }
      } catch {
        // ignore transient errors
      }
    }, 2000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [runId, onDone]);
}

// Poll multiple run IDs (for compare mode). Calls onAllDone when every run finishes.
function useMultiRunPoller(
  runIds: string[],
  onAllDone: () => void
) {
  const doneRef = useRef<Set<string>>(new Set());
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (runIds.length === 0) return;
    doneRef.current = new Set();

    intervalRef.current = setInterval(async () => {
      await Promise.allSettled(
        runIds
          .filter((id) => !doneRef.current.has(id))
          .map(async (id) => {
            try {
              const run = await getRun(id);
              if (run.status === "completed" || run.status === "failed") {
                doneRef.current.add(id);
              }
            } catch {
              // ignore
            }
          })
      );
      if (doneRef.current.size === runIds.length) {
        if (intervalRef.current) clearInterval(intervalRef.current);
        onAllDone();
      }
    }, 2000);

    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [runIds.join(","), onAllDone]); // eslint-disable-line react-hooks/exhaustive-deps
}

// ---- Benchmark import form ----

function BenchmarkImportRow({
  dataset,
  onImported,
}: {
  dataset: BenchmarkMeta;
  onImported: (suiteId: string) => void;
}) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [suiteName, setSuiteName] = useState(`${dataset.id}-${new Date().toISOString().slice(0, 10)}`);
  const [model, setModel] = useState("gpt-4o-mini");

  const mutation = useMutation({
    mutationFn: () => importBenchmark(dataset.id, suiteName, model),
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ["eval-suites"] });
      setOpen(false);
      onImported(data.suite_id);
    },
  });

  return (
    <div className="flex items-center justify-between py-2 border-b border-zinc-800 last:border-0">
      <div className="flex items-center gap-2 min-w-0">
        {industryBadge(dataset.industry)}
        <span className="text-sm font-medium text-zinc-200 truncate">{dataset.name}</span>
        <span className="text-xs text-zinc-500">{dataset.case_count} cases</span>
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogTrigger asChild>
          <Button variant="outline" className="text-xs h-7 px-3 shrink-0">Import & Run</Button>
        </DialogTrigger>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Import: {dataset.name}</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-zinc-400 mb-4">{dataset.description}</p>
          <form
            onSubmit={(e) => { e.preventDefault(); mutation.mutate(); }}
            className="space-y-4"
          >
            <div className="space-y-1">
              <Label>Suite Name</Label>
              <Input
                value={suiteName}
                onChange={(e) => setSuiteName(e.target.value)}
                required
              />
            </div>
            <div className="space-y-1">
              <Label>Model to run against</Label>
              <Input
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder="e.g. gpt-4o-mini"
              />
            </div>
            <Button type="submit" className="w-full" disabled={mutation.isPending}>
              {mutation.isPending ? "Importing..." : `Import ${dataset.case_count} Cases`}
            </Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ---- Compare modal ----

function CompareModal({
  suiteId,
  onStarted,
}: {
  suiteId: string;
  onStarted: (runs: CompareRunEntry[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [modelsRaw, setModelsRaw] = useState("gpt-4o, gpt-4o-mini");
  const [promptVersion, setPromptVersion] = useState("0");

  const mutation = useMutation({
    mutationFn: () => {
      const models = modelsRaw
        .split(",")
        .map((m) => m.trim())
        .filter(Boolean);
      const pv = parseInt(promptVersion, 10) || 0;
      return compareModels(suiteId, models, pv === 0 ? null : pv);
    },
    onSuccess: (runs) => {
      setOpen(false);
      onStarted(runs);
    },
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" className="text-xs h-7 px-2">Compare Models</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Compare Models</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={(e) => { e.preventDefault(); mutation.mutate(); }}
          className="space-y-4"
        >
          <div className="space-y-1">
            <Label>Models (comma-separated)</Label>
            <Input
              value={modelsRaw}
              onChange={(e) => setModelsRaw(e.target.value)}
              placeholder="gpt-4o, gpt-4o-mini, claude-3-5-sonnet"
              required
            />
          </div>
          <div className="space-y-1">
            <Label>Prompt Version (0 = latest)</Label>
            <Input
              type="number"
              min="0"
              value={promptVersion}
              onChange={(e) => setPromptVersion(e.target.value)}
            />
          </div>
          <Button type="submit" className="w-full" disabled={mutation.isPending}>
            {mutation.isPending ? "Starting..." : "Run Comparison"}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---- Compare results table ----

function CompareResultsCard({
  entries,
  onAllDone,
}: {
  entries: CompareRunEntry[];
  onAllDone: () => void;
}) {
  const [runs, setRuns] = useState<Record<string, EvalRun>>({});

  const stableOnAllDone = useCallback(() => onAllDone(), [onAllDone]);

  useMultiRunPoller(
    entries.map((e) => e.run_id),
    () => {
      stableOnAllDone();
    }
  );

  // Poll individually to update UI as each run completes.
  useEffect(() => {
    const interval = setInterval(async () => {
      await Promise.allSettled(
        entries.map(async ({ run_id }) => {
          try {
            const run = await getRun(run_id);
            setRuns((prev) => ({ ...prev, [run_id]: run }));
          } catch {
            // ignore
          }
        })
      );
    }, 2000);
    return () => clearInterval(interval);
  }, [entries]);

  const bestScore = Math.max(
    ...entries.map((e) => runs[e.run_id]?.score_pct ?? -1)
  );

  return (
    <Card>
      <CardContent className="pt-4">
        <h2 className="font-semibold text-sm text-zinc-300 mb-3">
          Model Comparison
          <span className="ml-2 text-xs text-blue-400 animate-pulse">
            {entries.some((e) => {
              const r = runs[e.run_id];
              return !r || (r.status !== "completed" && r.status !== "failed");
            }) ? "Running..." : ""}
          </span>
        </h2>
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b border-zinc-800 text-zinc-400">
              <th className="text-left py-1.5 font-medium">Model</th>
              <th className="text-left py-1.5 font-medium">Status</th>
              <th className="text-right py-1.5 font-medium">Score</th>
              <th className="text-right py-1.5 font-medium">Pass Rate</th>
              <th className="text-right py-1.5 font-medium">Winner</th>
            </tr>
          </thead>
          <tbody>
            {entries.map(({ model, run_id }) => {
              const run = runs[run_id];
              const isWinner =
                run?.status === "completed" &&
                run.score_pct != null &&
                run.score_pct === bestScore &&
                bestScore >= 0;
              return (
                <tr key={run_id} className="border-b border-zinc-800/50">
                  <td className="py-1.5 font-mono text-zinc-300">{model}</td>
                  <td className="py-1.5">{run ? statusBadge(run.status) : statusBadge("pending")}</td>
                  <td className="py-1.5 text-right">{scoreBadge(run?.score_pct)}</td>
                  <td className="py-1.5 text-right text-zinc-400">
                    {run && run.total_cases > 0
                      ? `${run.passed_cases}/${run.total_cases}`
                      : "--"}
                  </td>
                  <td className="py-1.5 text-right">
                    {isWinner && (
                      <Badge className="bg-indigo-600 text-white text-xs">Recommended</Badge>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </CardContent>
    </Card>
  );
}

// ---- Main component ----

export function EvalsPage() {
  const qc = useQueryClient();
  const [selectedSuiteId, setSelectedSuiteId] = useState<string | null>(null);
  const [newSuiteOpen, setNewSuiteOpen] = useState(false);
  const [newCaseOpen, setNewCaseOpen] = useState(false);
  const [runOpen, setRunOpen] = useState(false);
  const [viewRunId, setViewRunId] = useState<string | null>(null);
  const [pollingRunId, setPollingRunId] = useState<string | null>(null);
  const [compareEntries, setCompareEntries] = useState<CompareRunEntry[]>([]);
  const [benchmarksOpen, setBenchmarksOpen] = useState(false);

  // Forms
  const [suiteForm, setSuiteForm] = useState({ name: "", prompt_name: "" });
  const [caseForm, setCaseForm] = useState({
    input_vars_raw: "{}",
    expected_output: "",
    expected_contains_raw: "",
    score_method: "contains",
  });
  const [runForm, setRunForm] = useState({ model: "", prompt_version: "0" });

  const { data: suites = [] } = useQuery({
    queryKey: ["eval-suites"],
    queryFn: listSuites,
  });

  const { data: prompts = [] } = useQuery({
    queryKey: ["prompts"],
    queryFn: () =>
      apiClient
        .get<{ object: string; data: PromptTemplate[] }>("/v1/prompts?limit=100")
        .then((r) => r.data.data ?? []),
  });

  const { data: suiteDetail, refetch: refetchSuite } = useQuery({
    queryKey: ["eval-suite", selectedSuiteId],
    queryFn: () => getSuite(selectedSuiteId!),
    enabled: !!selectedSuiteId,
  });

  const { data: runResults } = useQuery({
    queryKey: ["eval-run-results", viewRunId],
    queryFn: () => getRunResults(viewRunId!),
    enabled: !!viewRunId,
  });

  const { data: trends = [] } = useQuery({
    queryKey: ["eval-trends", selectedSuiteId],
    queryFn: () => getTrends(selectedSuiteId!, 50),
    enabled: !!selectedSuiteId,
  });

  const { data: benchmarks = [] } = useQuery({
    queryKey: ["eval-benchmarks"],
    queryFn: listBenchmarks,
  });

  const createSuiteMutation = useMutation({
    mutationFn: () => createSuite(suiteForm.name, suiteForm.prompt_name),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["eval-suites"] });
      setNewSuiteOpen(false);
      setSuiteForm({ name: "", prompt_name: "" });
    },
  });

  const addCaseMutation = useMutation({
    mutationFn: () => {
      let inputVars: Record<string, string> = {};
      try { inputVars = JSON.parse(caseForm.input_vars_raw); } catch { /**/ }
      const contains = caseForm.expected_contains_raw
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      return addCase(selectedSuiteId!, {
        input_vars: inputVars,
        expected_output: caseForm.expected_output,
        expected_contains: contains,
        score_method: caseForm.score_method,
      });
    },
    onSuccess: () => {
      refetchSuite();
      setNewCaseOpen(false);
      setCaseForm({ input_vars_raw: "{}", expected_output: "", expected_contains_raw: "", score_method: "contains" });
    },
  });

  const triggerRunMutation = useMutation({
    mutationFn: () =>
      triggerRun(selectedSuiteId!, runForm.model, parseInt(runForm.prompt_version, 10)),
    onSuccess: (data) => {
      setRunOpen(false);
      setRunForm({ model: "", prompt_version: "0" });
      setPollingRunId(data.run_id);
      refetchSuite();
    },
  });

  const onRunDone = useCallback(() => {
    setPollingRunId(null);
    refetchSuite();
    qc.invalidateQueries({ queryKey: ["eval-trends", selectedSuiteId] });
  }, [refetchSuite, qc, selectedSuiteId]);

  useRunPoller(pollingRunId, onRunDone);

  return (
    <div className="space-y-6">
      {/* Quick Start: Benchmark datasets */}
      <Card>
        <CardContent className="pt-4">
          <button
            onClick={() => setBenchmarksOpen((v) => !v)}
            className="w-full flex items-center justify-between text-sm font-semibold text-zinc-300 mb-1"
          >
            <span>Quick Start — Industry Benchmarks ({benchmarks.length})</span>
            <span className="text-zinc-500 text-xs">{benchmarksOpen ? "Hide" : "Show"}</span>
          </button>
          {benchmarksOpen && (
            <div className="mt-2">
              {benchmarks.length === 0 ? (
                <p className="text-zinc-500 text-xs text-center py-4">No benchmarks available.</p>
              ) : (
                benchmarks.map((b) => (
                  <BenchmarkImportRow
                    key={b.id}
                    dataset={b}
                    onImported={(suiteId) => {
                      setSelectedSuiteId(suiteId);
                      setBenchmarksOpen(false);
                    }}
                  />
                ))
              )}
            </div>
          )}
        </CardContent>
      </Card>

      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Eval Suites</h1>
        <Dialog open={newSuiteOpen} onOpenChange={setNewSuiteOpen}>
          <DialogTrigger asChild>
            <Button>+ New Suite</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>New Eval Suite</DialogTitle>
            </DialogHeader>
            <form
              onSubmit={(e) => { e.preventDefault(); createSuiteMutation.mutate(); }}
              className="space-y-4"
            >
              <div className="space-y-1">
                <Label>Suite Name</Label>
                <Input
                  placeholder="e.g. summarization-v1"
                  value={suiteForm.name}
                  onChange={(e) => setSuiteForm({ ...suiteForm, name: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-1">
                <Label>Prompt</Label>
                <select
                  className="w-full h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100"
                  value={suiteForm.prompt_name}
                  onChange={(e) => setSuiteForm({ ...suiteForm, prompt_name: e.target.value })}
                  required
                >
                  <option value="">-- select prompt --</option>
                  {prompts.map((p) => (
                    <option key={p.id} value={p.name}>{p.name}</option>
                  ))}
                </select>
              </div>
              <Button type="submit" className="w-full" disabled={createSuiteMutation.isPending}>
                {createSuiteMutation.isPending ? "Creating..." : "Create Suite"}
              </Button>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Suite list */}
        <Card className="lg:col-span-1">
          <CardContent className="pt-4 space-y-1">
            {suites.length === 0 && (
              <p className="text-zinc-500 text-sm text-center py-8">No eval suites yet.</p>
            )}
            {suites.map((s) => (
              <button
                key={s.id}
                onClick={() => { setSelectedSuiteId(s.id); setViewRunId(null); setCompareEntries([]); }}
                className={`w-full text-left px-3 py-2 rounded-md text-sm transition-colors ${
                  selectedSuiteId === s.id
                    ? "bg-indigo-600 text-white"
                    : "text-zinc-300 hover:bg-zinc-800"
                }`}
              >
                <div className="font-medium">{s.name}</div>
                <div className="text-xs text-zinc-400">{s.prompt_name}</div>
              </button>
            ))}
          </CardContent>
        </Card>

        {/* Suite detail */}
        <div className="lg:col-span-2 space-y-4">
          {!selectedSuiteId && (
            <Card>
              <CardContent className="pt-4">
                <p className="text-zinc-500 text-sm text-center py-8">Select a suite to view details.</p>
              </CardContent>
            </Card>
          )}

          {suiteDetail && (
            <>
              {/* Test cases */}
              <Card>
                <CardContent className="pt-4">
                  <div className="flex items-center justify-between mb-3">
                    <h2 className="font-semibold text-sm text-zinc-300">
                      Test Cases ({suiteDetail.cases.length})
                    </h2>
                    <div className="flex gap-2">
                      <Dialog open={newCaseOpen} onOpenChange={setNewCaseOpen}>
                        <DialogTrigger asChild>
                          <Button variant="outline" className="text-xs h-7 px-2">+ Add Case</Button>
                        </DialogTrigger>
                        <DialogContent>
                          <DialogHeader>
                            <DialogTitle>Add Test Case</DialogTitle>
                          </DialogHeader>
                          <form
                            onSubmit={(e) => { e.preventDefault(); addCaseMutation.mutate(); }}
                            className="space-y-4"
                          >
                            <div className="space-y-1">
                              <Label>Input Variables (JSON)</Label>
                              <textarea
                                className="w-full h-20 rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 font-mono"
                                value={caseForm.input_vars_raw}
                                onChange={(e) => setCaseForm({ ...caseForm, input_vars_raw: e.target.value })}
                              />
                            </div>
                            <div className="space-y-1">
                              <Label>Score Method</Label>
                              <select
                                className="w-full h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100"
                                value={caseForm.score_method}
                                onChange={(e) => setCaseForm({ ...caseForm, score_method: e.target.value })}
                              >
                                <option value="contains">contains</option>
                                <option value="exact">exact</option>
                                <option value="llm_judge">llm_judge</option>
                              </select>
                            </div>
                            {caseForm.score_method === "exact" && (
                              <div className="space-y-1">
                                <Label>Expected Output</Label>
                                <Input
                                  value={caseForm.expected_output}
                                  onChange={(e) => setCaseForm({ ...caseForm, expected_output: e.target.value })}
                                  placeholder="Exact expected output"
                                />
                              </div>
                            )}
                            {caseForm.score_method === "contains" && (
                              <div className="space-y-1">
                                <Label>Expected Contains (comma-separated)</Label>
                                <Input
                                  value={caseForm.expected_contains_raw}
                                  onChange={(e) => setCaseForm({ ...caseForm, expected_contains_raw: e.target.value })}
                                  placeholder="keyword1, keyword2"
                                />
                              </div>
                            )}
                            <Button type="submit" className="w-full" disabled={addCaseMutation.isPending}>
                              {addCaseMutation.isPending ? "Adding..." : "Add Case"}
                            </Button>
                          </form>
                        </DialogContent>
                      </Dialog>

                      <Dialog open={runOpen} onOpenChange={setRunOpen}>
                        <DialogTrigger asChild>
                          <Button className="text-xs h-7 px-2">Run Now</Button>
                        </DialogTrigger>
                        <DialogContent>
                          <DialogHeader>
                            <DialogTitle>Trigger Eval Run</DialogTitle>
                          </DialogHeader>
                          <form
                            onSubmit={(e) => { e.preventDefault(); triggerRunMutation.mutate(); }}
                            className="space-y-4"
                          >
                            <div className="space-y-1">
                              <Label>Model Name</Label>
                              <Input
                                value={runForm.model}
                                onChange={(e) => setRunForm({ ...runForm, model: e.target.value })}
                                placeholder="e.g. gpt-4o"
                                required
                              />
                            </div>
                            <div className="space-y-1">
                              <Label>Prompt Version (0 = latest)</Label>
                              <Input
                                type="number"
                                min="0"
                                value={runForm.prompt_version}
                                onChange={(e) => setRunForm({ ...runForm, prompt_version: e.target.value })}
                              />
                            </div>
                            <Button type="submit" className="w-full" disabled={triggerRunMutation.isPending}>
                              {triggerRunMutation.isPending ? "Triggering..." : "Start Run"}
                            </Button>
                          </form>
                        </DialogContent>
                      </Dialog>

                      <CompareModal
                        suiteId={selectedSuiteId!}
                        onStarted={(runs) => {
                          setCompareEntries(runs);
                          refetchSuite();
                        }}
                      />
                    </div>
                  </div>

                  {suiteDetail.cases.length === 0 ? (
                    <p className="text-zinc-500 text-xs text-center py-4">No test cases yet.</p>
                  ) : (
                    <table className="w-full text-xs">
                      <thead>
                        <tr className="border-b border-zinc-800 text-zinc-400">
                          <th className="text-left py-1.5 font-medium">Vars</th>
                          <th className="text-left py-1.5 font-medium">Method</th>
                          <th className="text-left py-1.5 font-medium">Expected</th>
                        </tr>
                      </thead>
                      <tbody>
                        {suiteDetail.cases.map((ec) => (
                          <tr key={ec.id} className="border-b border-zinc-800/50">
                            <td className="py-1.5 font-mono text-zinc-300 max-w-[120px] truncate">
                              {JSON.stringify(ec.input_vars)}
                            </td>
                            <td className="py-1.5">
                              <Badge variant="outline">{ec.score_method}</Badge>
                            </td>
                            <td className="py-1.5 text-zinc-400 max-w-[200px] truncate">
                              {ec.score_method === "exact"
                                ? ec.expected_output ?? "--"
                                : ec.expected_contains?.join(", ") ?? "--"}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </CardContent>
              </Card>

              {/* Score trend chart */}
              {trends.length > 0 && (
                <Card>
                  <CardContent className="pt-4">
                    <h2 className="font-semibold text-sm text-zinc-300 mb-2">
                      Score Trend ({trends.length} completed runs)
                    </h2>
                    <TrendSparkline trends={trends} />
                  </CardContent>
                </Card>
              )}

              {/* Compare results */}
              {compareEntries.length > 0 && (
                <CompareResultsCard
                  entries={compareEntries}
                  onAllDone={() => {
                    refetchSuite();
                    qc.invalidateQueries({ queryKey: ["eval-trends", selectedSuiteId] });
                  }}
                />
              )}

              {/* Run history */}
              <Card>
                <CardContent className="pt-4">
                  <h2 className="font-semibold text-sm text-zinc-300 mb-3">
                    Run History
                    {pollingRunId && (
                      <span className="ml-2 text-xs text-blue-400 animate-pulse">Running...</span>
                    )}
                  </h2>
                  {suiteDetail.runs.length === 0 ? (
                    <p className="text-zinc-500 text-xs text-center py-4">No runs yet.</p>
                  ) : (
                    <table className="w-full text-xs">
                      <thead>
                        <tr className="border-b border-zinc-800 text-zinc-400">
                          <th className="text-left py-1.5 font-medium">Model</th>
                          <th className="text-left py-1.5 font-medium">v</th>
                          <th className="text-left py-1.5 font-medium">Status</th>
                          <th className="text-right py-1.5 font-medium">Score</th>
                          <th className="text-right py-1.5 font-medium">Cases</th>
                          <th className="text-right py-1.5 font-medium">Results</th>
                        </tr>
                      </thead>
                      <tbody>
                        {suiteDetail.runs.map((run) => (
                          <tr key={run.id} className="border-b border-zinc-800/50">
                            <td className="py-1.5 font-mono text-zinc-300">{run.model}</td>
                            <td className="py-1.5 text-zinc-400">v{run.prompt_version || "latest"}</td>
                            <td className="py-1.5">{statusBadge(run.status)}</td>
                            <td className="py-1.5 text-right">{scoreBadge(run.score_pct)}</td>
                            <td className="py-1.5 text-right text-zinc-400">
                              {run.passed_cases}/{run.total_cases}
                            </td>
                            <td className="py-1.5 text-right">
                              {run.status === "completed" && (
                                <button
                                  onClick={() => setViewRunId(viewRunId === run.id ? null : run.id)}
                                  className="text-blue-400 hover:underline"
                                >
                                  {viewRunId === run.id ? "Hide" : "View"}
                                </button>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </CardContent>
              </Card>

              {/* Run results detail */}
              {viewRunId && runResults && (
                <Card>
                  <CardContent className="pt-4">
                    <h2 className="font-semibold text-sm text-zinc-300 mb-3">
                      Individual Results
                    </h2>
                    <table className="w-full text-xs">
                      <thead>
                        <tr className="border-b border-zinc-800 text-zinc-400">
                          <th className="text-left py-1.5 font-medium">Case</th>
                          <th className="text-left py-1.5 font-medium">Actual Output</th>
                          <th className="text-center py-1.5 font-medium">Passed</th>
                          <th className="text-right py-1.5 font-medium">Latency</th>
                          <th className="text-right py-1.5 font-medium">Score</th>
                        </tr>
                      </thead>
                      <tbody>
                        {runResults.map((r) => (
                          <tr key={r.id} className="border-b border-zinc-800/50 align-top">
                            <td className="py-1.5 font-mono text-zinc-500 max-w-[100px] truncate">
                              {r.case_id.slice(0, 8)}...
                            </td>
                            <td className="py-1.5 text-zinc-300 max-w-[260px]">
                              {r.error ? (
                                <span className="text-red-400">{r.error}</span>
                              ) : (
                                <span className="line-clamp-2">{r.actual_output ?? "--"}</span>
                              )}
                            </td>
                            <td className="py-1.5 text-center">
                              {r.passed ? (
                                <Badge className="bg-green-700 text-white">pass</Badge>
                              ) : (
                                <Badge className="bg-red-700 text-white">fail</Badge>
                              )}
                            </td>
                            <td className="py-1.5 text-right text-zinc-400">{r.latency_ms}ms</td>
                            <td className="py-1.5 text-right text-zinc-400">
                              {r.score != null ? r.score.toFixed(2) : "--"}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </CardContent>
                </Card>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}
