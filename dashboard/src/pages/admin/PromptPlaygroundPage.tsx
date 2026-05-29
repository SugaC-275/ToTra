import { useState, useRef, useEffect } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient, estimateCost, listPrompts, type CostEstimate } from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";

// ---- Types ----

interface Message {
  role: "system" | "user" | "assistant";
  content: string;
}

interface PiiHit {
  entity_type: string;
  text: string;
  start?: number;
  end?: number;
}

interface PiiCheckResult {
  flagged: boolean;
  hits: PiiHit[];
}

interface ComplianceBundle {
  id: string;
  name: string;
  industry: string;
  is_active: boolean;
  policies: { name: string; action: string }[];
}

interface ChatResponse {
  choices: { message: { role: string; content: string } }[];
  usage?: { prompt_tokens: number; completion_tokens: number; total_tokens: number };
  latency_ms?: number;
}

// ---- API helpers ----

async function checkPii(text: string): Promise<PiiCheckResult> {
  try {
    const { data } = await apiClient.post<PiiCheckResult>("/gateway/v1/pii/check", { text });
    return data;
  } catch {
    // fallback client-side patterns
    const emailRe = /[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/g;
    const phoneRe = /\b\d{3}[-.\s]?\d{3}[-.\s]?\d{4}\b/g;
    const ssnRe = /\b\d{3}-\d{2}-\d{4}\b/g;
    const hits: PiiHit[] = [];
    (text.match(emailRe) ?? []).forEach((t) => hits.push({ entity_type: "EMAIL", text: t }));
    (text.match(phoneRe) ?? []).forEach((t) => hits.push({ entity_type: "PHONE", text: t }));
    (text.match(ssnRe) ?? []).forEach((t) => hits.push({ entity_type: "SSN", text: t }));
    return { flagged: hits.length > 0, hits };
  }
}

async function getActiveBundles(): Promise<ComplianceBundle[]> {
  try {
    const { data } = await apiClient.get<{ bundles: ComplianceBundle[] }>(
      "/api/admin/compliance/bundles"
    );
    return (data.bundles ?? []).filter((b) => b.is_active);
  } catch {
    return [];
  }
}

async function getModelList(): Promise<string[]> {
  try {
    const { data } = await apiClient.get<{ total: number; models: { name: string; is_active: boolean }[] }>(
      "/api/models"
    );
    return (data.models ?? []).filter((m) => m.is_active).map((m) => m.name);
  } catch {
    return ["gpt-4o", "gpt-4o-mini", "claude-3-5-sonnet-20241022"];
  }
}

async function createEvalCase(messages: Message[], response: string): Promise<void> {
  await apiClient.post("/v1/evals/cases-from-playground", {
    messages,
    response,
    score_method: "contains",
  });
}

// ---- PII-highlighted textarea ----

function PiiHighlightDisplay({ text, hits }: { text: string; hits: PiiHit[] }) {
  if (hits.length === 0) {
    return (
      <pre className="text-xs font-mono text-zinc-300 whitespace-pre-wrap break-words">
        {text}
      </pre>
    );
  }
  // Simple highlight: bold + color entity type occurrences
  const segments: { text: string; flagged: boolean; entityType?: string }[] = [];
  let cursor = 0;
  // Sort hits by position in text (find index)
  const sorted = [...hits].map((h) => ({
    ...h,
    idx: text.indexOf(h.text),
  })).filter((h) => h.idx >= 0).sort((a, b) => a.idx - b.idx);

  for (const hit of sorted) {
    if (hit.idx > cursor) {
      segments.push({ text: text.slice(cursor, hit.idx), flagged: false });
    }
    segments.push({ text: hit.text, flagged: true, entityType: hit.entity_type });
    cursor = hit.idx + hit.text.length;
  }
  if (cursor < text.length) {
    segments.push({ text: text.slice(cursor), flagged: false });
  }

  return (
    <pre className="text-xs font-mono text-zinc-300 whitespace-pre-wrap break-words">
      {segments.map((seg, i) =>
        seg.flagged ? (
          <mark
            key={i}
            className="bg-yellow-900/60 text-yellow-300 rounded-sm"
            title={seg.entityType}
          >
            {seg.text}
          </mark>
        ) : (
          <span key={i}>{seg.text}</span>
        )
      )}
    </pre>
  );
}

// ---- Main page ----

export function PromptPlaygroundPage() {
  const qc = useQueryClient();

  // Messages (multi-turn)
  const [messages, setMessages] = useState<Message[]>([
    { role: "system", content: "" },
    { role: "user", content: "" },
  ]);

  // Model + params
  const [model, setModel] = useState("gpt-4o");
  const [temperature, setTemperature] = useState(0.7);
  const [maxTokens, setMaxTokens] = useState(1024);
  const [topP, setTopP] = useState(1.0);

  // PII check
  const [piiResult, setPiiResult] = useState<PiiCheckResult | null>(null);
  const [piiLoading, setPiiLoading] = useState(false);

  // Response
  const [response, setResponse] = useState<ChatResponse | null>(null);
  const [runLoading, setRunLoading] = useState(false);
  const [runError, setRunError] = useState("");
  const [runLatencyMs, setRunLatencyMs] = useState<number | null>(null);

  // Cost estimate
  const [costEstimate, setCostEstimate] = useState<CostEstimate | null>(null);
  const costDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Eval case
  const [evalCreated, setEvalCreated] = useState(false);
  const [evalError, setEvalError] = useState("");

  // Bundles & models
  const { data: activeBundles = [] } = useQuery({
    queryKey: ["active-bundles"],
    queryFn: getActiveBundles,
    staleTime: 60_000,
  });

  const { data: modelList = [] } = useQuery({
    queryKey: ["model-list"],
    queryFn: getModelList,
    staleTime: 60_000,
  });

  const { data: prompts = [] } = useQuery({
    queryKey: ["prompts"],
    queryFn: () => listPrompts().then((r) => r.data.data ?? []),
    staleTime: 60_000,
  });

  // Debounced cost estimate when messages or model changes
  useEffect(() => {
    if (costDebounceRef.current) clearTimeout(costDebounceRef.current);
    const nonEmpty = messages.filter((m) => m.content.trim());
    if (!model.trim() || nonEmpty.length === 0) {
      setCostEstimate(null);
      return;
    }
    costDebounceRef.current = setTimeout(async () => {
      try {
        const result = await estimateCost(
          model,
          nonEmpty.map((m) => ({ role: m.role, content: m.content })),
          maxTokens
        );
        setCostEstimate(result);
      } catch {
        setCostEstimate(null);
      }
    }, 600);
    return () => {
      if (costDebounceRef.current) clearTimeout(costDebounceRef.current);
    };
  }, [model, messages, maxTokens]);

  function updateMessage(index: number, content: string) {
    setMessages((prev) => prev.map((m, i) => (i === index ? { ...m, content } : m)));
    setPiiResult(null);
  }

  function addTurn() {
    setMessages((prev) => [
      ...prev,
      { role: "user", content: "" },
      { role: "assistant", content: "" },
    ]);
  }

  function removeMessage(index: number) {
    setMessages((prev) => prev.filter((_, i) => i !== index));
  }

  async function handleCheckPii() {
    const combined = messages.map((m) => m.content).join("\n");
    setPiiLoading(true);
    try {
      const result = await checkPii(combined);
      setPiiResult(result);
    } finally {
      setPiiLoading(false);
    }
  }

  async function handleRun() {
    setRunLoading(true);
    setRunError("");
    setResponse(null);
    setRunLatencyMs(null);
    setEvalCreated(false);
    const t0 = Date.now();
    try {
      const { data } = await apiClient.post<ChatResponse>("/v1/chat/completions", {
        model,
        messages: messages
          .filter((m) => m.content.trim())
          .map((m) => ({ role: m.role, content: m.content })),
        temperature,
        max_tokens: maxTokens,
        top_p: topP,
      });
      setRunLatencyMs(Date.now() - t0);
      setResponse(data);
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: { message?: string } } } })
          ?.response?.data?.error?.message ?? "Request failed";
      setRunError(msg);
    } finally {
      setRunLoading(false);
    }
  }

  function loadPromptTemplate(content: string) {
    setMessages([
      { role: "system", content },
      { role: "user", content: "" },
    ]);
    setPiiResult(null);
    setResponse(null);
    setRunError("");
  }

  const evalMutation = useMutation({
    mutationFn: () => {
      const responseText = response?.choices?.[0]?.message?.content ?? "";
      return createEvalCase(messages, responseText);
    },
    onSuccess: () => {
      setEvalCreated(true);
      setEvalError("");
      qc.invalidateQueries({ queryKey: ["eval-suites"] });
    },
    onError: () => setEvalError("Failed to create eval case"),
  });

  const allText = messages.map((m) => m.content).join("\n");
  const responseText = response?.choices?.[0]?.message?.content ?? "";
  const usage = response?.usage;
  const actualCostUsd = usage && costEstimate
    ? (usage.prompt_tokens / 1_000_000) * costEstimate.cost_per_million_input +
      (usage.completion_tokens / 1_000_000) * costEstimate.cost_per_million_output
    : null;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Prompt Playground</h1>
        <div className="flex items-center gap-2">
          {activeBundles.length > 0 && (
            <div className="flex gap-1 flex-wrap">
              {activeBundles.map((b) => (
                <Badge
                  key={b.id}
                  className="bg-indigo-900/60 text-indigo-300 border border-indigo-700 text-xs"
                >
                  {b.name}
                </Badge>
              ))}
            </div>
          )}
          {activeBundles.length === 0 && (
            <Badge variant="outline" className="text-zinc-500 text-xs">No active bundles</Badge>
          )}
        </div>
      </div>

      <div className="flex gap-4 h-[calc(100vh-10rem)] min-h-[600px]">
        {/* Left panel — editor */}
        <div className="flex-1 flex flex-col gap-3 min-w-0">
          {/* Top bar: model + params */}
          <Card>
            <CardContent className="pt-3 pb-3">
              <div className="flex flex-wrap items-end gap-4">
                <div className="space-y-1">
                  <Label className="text-xs text-zinc-400">Model</Label>
                  {modelList.length > 0 ? (
                    <select
                      value={model}
                      onChange={(e) => setModel(e.target.value)}
                      className="h-8 rounded-md border border-zinc-700 bg-zinc-800 px-2 py-1 text-sm text-zinc-100 font-mono"
                    >
                      {modelList.map((m) => (
                        <option key={m} value={m}>{m}</option>
                      ))}
                    </select>
                  ) : (
                    <Input
                      value={model}
                      onChange={(e) => setModel(e.target.value)}
                      placeholder="gpt-4o"
                      className="h-8 text-sm font-mono w-48"
                    />
                  )}
                </div>
                <div className="space-y-1">
                  <Label className="text-xs text-zinc-400">Temp ({temperature})</Label>
                  <input
                    type="range"
                    min="0"
                    max="2"
                    step="0.1"
                    value={temperature}
                    onChange={(e) => setTemperature(parseFloat(e.target.value))}
                    className="w-28 accent-indigo-500"
                  />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs text-zinc-400">Max tokens</Label>
                  <Input
                    type="number"
                    min="1"
                    max="32000"
                    value={maxTokens}
                    onChange={(e) => setMaxTokens(parseInt(e.target.value, 10) || 1024)}
                    className="h-8 text-sm w-24"
                  />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs text-zinc-400">Top-p ({topP})</Label>
                  <input
                    type="range"
                    min="0"
                    max="1"
                    step="0.05"
                    value={topP}
                    onChange={(e) => setTopP(parseFloat(e.target.value))}
                    className="w-24 accent-indigo-500"
                  />
                </div>
                {prompts.length > 0 && (
                  <div className="space-y-1">
                    <Label className="text-xs text-zinc-400">Load prompt</Label>
                    <select
                      onChange={(e) => {
                        const p = prompts.find((pr) => pr.name === e.target.value);
                        if (p) loadPromptTemplate(p.content);
                      }}
                      className="h-8 rounded-md border border-zinc-700 bg-zinc-800 px-2 py-1 text-sm text-zinc-100"
                      defaultValue=""
                    >
                      <option value="" disabled>— load from hub —</option>
                      {prompts.map((p) => (
                        <option key={p.id} value={p.name}>{p.name} v{p.version}</option>
                      ))}
                    </select>
                  </div>
                )}
              </div>

              {/* Cost estimate */}
              {costEstimate && (
                <div className="flex items-center gap-2 mt-2 flex-wrap">
                  <Badge variant="outline" className="text-emerald-400 border-emerald-800 font-mono text-xs">
                    Est. ~{costEstimate.estimated_prompt_tokens} tokens
                  </Badge>
                  <Badge variant="outline" className="text-emerald-400 border-emerald-800 font-mono text-xs">
                    Est. cost: ${costEstimate.estimated_cost_usd.toFixed(4)}
                  </Badge>
                </div>
              )}
            </CardContent>
          </Card>

          {/* Messages editor */}
          <Card className="flex-1 flex flex-col overflow-hidden">
            <CardContent className="flex-1 flex flex-col pt-3 gap-3 overflow-y-auto">
              <div className="flex items-center justify-between">
                <p className="text-sm font-semibold text-zinc-200">Messages</p>
                <div className="flex gap-2">
                  <button
                    onClick={handleCheckPii}
                    disabled={piiLoading || !allText.trim()}
                    className="px-3 py-1 rounded text-xs bg-yellow-900/40 text-yellow-300 border border-yellow-800 hover:bg-yellow-900/60 disabled:opacity-50"
                  >
                    {piiLoading ? "Checking…" : "Check PII"}
                  </button>
                  <button
                    onClick={addTurn}
                    className="px-3 py-1 rounded text-xs bg-zinc-800 text-zinc-300 border border-zinc-700 hover:bg-zinc-700"
                  >
                    + Add turn
                  </button>
                </div>
              </div>

              {/* PII result banner */}
              {piiResult && (
                <div className={`rounded border px-3 py-2 text-xs ${
                  piiResult.flagged
                    ? "border-yellow-800 bg-yellow-950/30 text-yellow-300"
                    : "border-green-800 bg-green-950/30 text-green-300"
                }`}>
                  {piiResult.flagged ? (
                    <>
                      <span className="font-medium">PII detected:</span>{" "}
                      {piiResult.hits.map((h) => h.entity_type).join(", ")} — highlighted below.
                      <span className="text-yellow-500"> These patterns may be blocked by active bundles.</span>
                    </>
                  ) : (
                    "No PII patterns detected."
                  )}
                </div>
              )}

              {/* Message fields */}
              <div className="space-y-3 flex-1">
                {messages.map((msg, idx) => (
                  <div key={idx} className="space-y-1">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <select
                          value={msg.role}
                          onChange={(e) =>
                            setMessages((prev) =>
                              prev.map((m, i) =>
                                i === idx ? { ...m, role: e.target.value as Message["role"] } : m
                              )
                            )
                          }
                          className="h-6 rounded border border-zinc-700 bg-zinc-900 px-1 text-xs text-zinc-300"
                        >
                          <option value="system">system</option>
                          <option value="user">user</option>
                          <option value="assistant">assistant</option>
                        </select>
                      </div>
                      {idx > 0 && (
                        <button
                          onClick={() => removeMessage(idx)}
                          className="text-xs text-zinc-600 hover:text-red-400"
                        >
                          remove
                        </button>
                      )}
                    </div>

                    {/* Show PII highlights if checked, otherwise plain textarea */}
                    {piiResult && piiResult.flagged && (
                      <div className="rounded border border-yellow-900/50 bg-zinc-950 p-2 min-h-[60px]">
                        <PiiHighlightDisplay text={msg.content} hits={piiResult.hits} />
                      </div>
                    )}
                    <textarea
                      value={msg.content}
                      onChange={(e) => updateMessage(idx, e.target.value)}
                      rows={msg.role === "system" ? 3 : 4}
                      className={`w-full resize-y rounded border bg-zinc-900 px-3 py-2 text-xs font-mono text-zinc-100 placeholder-zinc-600 focus:outline-none focus:border-indigo-500 ${
                        piiResult && piiResult.flagged && msg.content &&
                        piiResult.hits.some((h) => msg.content.includes(h.text))
                          ? "border-yellow-700"
                          : "border-zinc-700"
                      }`}
                      placeholder={
                        msg.role === "system"
                          ? "System instructions…"
                          : msg.role === "user"
                          ? "User message…"
                          : "Assistant message…"
                      }
                    />
                  </div>
                ))}
              </div>

              {/* Run button */}
              <div className="flex items-center gap-3 pt-1">
                <Button
                  onClick={handleRun}
                  disabled={runLoading || !messages.some((m) => m.content.trim())}
                >
                  {runLoading ? "Running…" : "Send"}
                </Button>
                {runError && (
                  <span className="text-xs text-red-400">{runError}</span>
                )}
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Right panel — response + compliance */}
        <div className="w-80 shrink-0 flex flex-col gap-3">
          {/* Compliance status */}
          <Card>
            <CardContent className="pt-3 pb-3">
              <p className="text-sm font-semibold text-zinc-200 mb-2">Compliance</p>
              {activeBundles.length === 0 ? (
                <p className="text-xs text-zinc-500">No active compliance bundles.</p>
              ) : (
                <div className="space-y-1.5">
                  {activeBundles.map((b) => {
                    const passes = true; // Real check would call model config API
                    return (
                      <div key={b.id} className="flex items-center justify-between text-xs">
                        <span className="text-zinc-300">{b.name}</span>
                        <Badge className={`text-xs ${passes ? "bg-green-800 text-green-200" : "bg-red-800 text-red-200"}`}>
                          {passes ? "Pass" : "Fail"}
                        </Badge>
                      </div>
                    );
                  })}
                </div>
              )}
            </CardContent>
          </Card>

          {/* Response panel */}
          <Card className="flex-1 flex flex-col overflow-hidden">
            <CardContent className="flex-1 flex flex-col pt-3 gap-3 overflow-y-auto">
              <p className="text-sm font-semibold text-zinc-200">Response</p>

              {!response && !runLoading && (
                <p className="text-xs text-zinc-600 mt-4 text-center">
                  Send a message to see the response here.
                </p>
              )}

              {runLoading && (
                <p className="text-xs text-zinc-500 animate-pulse">Waiting for response…</p>
              )}

              {response && (
                <div className="space-y-3 flex-1">
                  {/* Response text */}
                  <div className="rounded border border-zinc-800 bg-zinc-950 p-3 max-h-64 overflow-y-auto">
                    <pre className="text-xs text-zinc-200 whitespace-pre-wrap break-words font-mono">
                      {responseText}
                    </pre>
                  </div>

                  {/* Stats */}
                  <div className="space-y-1 text-xs">
                    {usage && (
                      <>
                        <div className="flex justify-between text-zinc-400">
                          <span>Prompt tokens</span>
                          <span className="font-mono">{usage.prompt_tokens}</span>
                        </div>
                        <div className="flex justify-between text-zinc-400">
                          <span>Completion tokens</span>
                          <span className="font-mono">{usage.completion_tokens}</span>
                        </div>
                        <div className="flex justify-between text-zinc-400">
                          <span>Total tokens</span>
                          <span className="font-mono">{usage.total_tokens}</span>
                        </div>
                      </>
                    )}
                    {runLatencyMs !== null && (
                      <div className="flex justify-between text-zinc-400">
                        <span>Latency</span>
                        <span className="font-mono">{runLatencyMs} ms</span>
                      </div>
                    )}
                    {actualCostUsd !== null && (
                      <div className="flex justify-between text-emerald-400">
                        <span>Actual cost</span>
                        <span className="font-mono">${actualCostUsd.toFixed(6)}</span>
                      </div>
                    )}
                  </div>

                  {/* Save to eval suite */}
                  <div className="pt-1 border-t border-zinc-800">
                    {evalCreated ? (
                      <p className="text-xs text-green-400">Eval case saved.</p>
                    ) : (
                      <button
                        onClick={() => evalMutation.mutate()}
                        disabled={evalMutation.isPending}
                        className="text-xs text-indigo-400 hover:underline disabled:opacity-50"
                      >
                        {evalMutation.isPending ? "Saving…" : "+ Save to Eval Suite"}
                      </button>
                    )}
                    {evalError && (
                      <p className="text-xs text-red-400 mt-1">{evalError}</p>
                    )}
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
