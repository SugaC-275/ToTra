import { useState, useEffect, useCallback, useRef } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listPrompts,
  savePrompt,
  getPromptVersion,
  renderPrompt,
  estimateCost,
  apiClient,
} from "../../api/client";
import type { PromptTemplate, CostEstimate } from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { PromptDiff } from "../../components/PromptDiff";

// Extract {{variable}} patterns from content
function extractVariables(content: string): string[] {
  const matches = content.match(/\{\{([a-zA-Z_][a-zA-Z0-9_]*)\}\}/g) ?? [];
  const unique = Array.from(new Set(matches.map((m) => m.slice(2, -2))));
  return unique;
}

interface VersionEntry {
  version: number;
  content: string;
  created_at: string;
}

export function PromptsPage() {
  const qc = useQueryClient();

  // Left panel state
  const [search, setSearch] = useState("");
  const [selectedName, setSelectedName] = useState<string | null>(null);

  // Editor state
  const [editorName, setEditorName] = useState("");
  const [editorContent, setEditorContent] = useState("");
  const [currentVersion, setCurrentVersion] = useState<number | null>(null);
  const [isNew, setIsNew] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [saveSuccess, setSaveSuccess] = useState(false);

  // Version history modal
  const [showHistory, setShowHistory] = useState(false);
  const [history, setHistory] = useState<VersionEntry[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historySelected, setHistorySelected] = useState<VersionEntry | null>(null);

  // Version diff state
  const [showDiff, setShowDiff] = useState(false);
  const [diffVersionA, setDiffVersionA] = useState<VersionEntry | null>(null);
  const [diffVersionB, setDiffVersionB] = useState<VersionEntry | null>(null);

  // Playground state
  const [varValues, setVarValues] = useState<Record<string, string>>({});
  const [renderedPreview, setRenderedPreview] = useState("");
  const [renderError, setRenderError] = useState("");
  const [testModel, setTestModel] = useState("gpt-4o");
  const [testResponse, setTestResponse] = useState("");
  const [testLoading, setTestLoading] = useState(false);
  const [testError, setTestError] = useState("");
  const [costEstimate, setCostEstimate] = useState<CostEstimate | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const detectedVars = extractVariables(editorContent);

  // Keep varValues keys in sync with detected variables
  useEffect(() => {
    const timer = setTimeout(() => {
      setVarValues((prev) => {
        const next: Record<string, string> = {};
        for (const v of detectedVars) {
          next[v] = prev[v] ?? "";
        }
        return next;
      });
      setRenderedPreview("");
    }, 0);
    return () => clearTimeout(timer);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editorContent]);

  // Debounced cost estimate: fires 500ms after model or content changes.
  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(async () => {
      if (!testModel.trim() || !editorContent.trim()) {
        setCostEstimate(null);
        return;
      }
      try {
        const prompt = editorContent;
        const result = await estimateCost(testModel, [{ role: "user", content: prompt }]);
        setCostEstimate(result);
      } catch {
        setCostEstimate(null);
      }
    }, 500);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [testModel, editorContent]);

  const { data: promptsData, isLoading: promptsLoading } = useQuery({
    queryKey: ["prompts"],
    queryFn: () => listPrompts().then((r) => r.data.data),
  });

  const prompts = promptsData ?? [];
  const filtered = prompts.filter((p) =>
    p.name.toLowerCase().includes(search.toLowerCase())
  );

  const saveMutation = useMutation({
    mutationFn: () => savePrompt(editorName.trim(), editorContent),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ["prompts"] });
      setCurrentVersion(res.data.version);
      setIsNew(false);
      setSelectedName(editorName.trim());
      setSaveSuccess(true);
      setSaveError("");
      setTimeout(() => setSaveSuccess(false), 2500);
    },
    onError: (err: unknown) => {
      const msg = (err as { response?: { data?: { error?: { message?: string } } } })
        ?.response?.data?.error?.message ?? "Save failed";
      setSaveError(msg);
    },
  });

  function handleSelectPrompt(p: PromptTemplate) {
    setSelectedName(p.name);
    setEditorName(p.name);
    setEditorContent(p.content);
    setCurrentVersion(p.version);
    setIsNew(false);
    setSaveError("");
    setSaveSuccess(false);
    setRenderedPreview("");
    setTestResponse("");
    setTestError("");
    setRenderError("");
    setShowHistory(false);
    setHistorySelected(null);
  }

  function handleNewPrompt() {
    setSelectedName(null);
    setEditorName("");
    setEditorContent("");
    setCurrentVersion(null);
    setIsNew(true);
    setSaveError("");
    setSaveSuccess(false);
    setRenderedPreview("");
    setTestResponse("");
    setTestError("");
    setRenderError("");
    setShowHistory(false);
    setHistorySelected(null);
  }

  // Load version history by fetching each version 1..current
  const loadHistory = useCallback(async (): Promise<VersionEntry[]> => {
    if (!selectedName || currentVersion == null) return [];
    setHistoryLoading(true);
    try {
      const requests = Array.from({ length: currentVersion }, (_, i) =>
        getPromptVersion(selectedName, i + 1).then((r) => ({
          version: r.data.version,
          content: r.data.content,
          created_at: r.data.created_at,
        }))
      );
      const results = await Promise.all(requests);
      const sorted = results.reverse(); // newest first
      setHistory(sorted);
      return sorted;
    } catch {
      setHistory([]);
      return [];
    } finally {
      setHistoryLoading(false);
    }
  }, [selectedName, currentVersion]);

  const handleShowHistory = useCallback(async () => {
    if (!selectedName || currentVersion == null) return;
    setShowHistory(true);
    setHistorySelected(null);
    await loadHistory();
  }, [selectedName, currentVersion, loadHistory]);

  const handleShowDiff = useCallback(async () => {
    if (!selectedName || currentVersion == null || currentVersion < 2) return;
    setShowDiff(true);
    setDiffVersionA(null);
    setDiffVersionB(null);
    const loaded = history.length >= currentVersion ? history : await loadHistory();
    if (loaded.length >= 2) {
      // Default: compare latest two (newest first in list)
      setDiffVersionB(loaded[0]);
      setDiffVersionA(loaded[1]);
    }
  }, [selectedName, currentVersion, history, loadHistory]);

  function handleRestoreVersion(entry: VersionEntry) {
    setEditorContent(entry.content);
    setShowHistory(false);
    setHistorySelected(null);
  }

  async function handlePreview() {
    if (!selectedName) {
      setRenderError("Save the prompt first to preview.");
      return;
    }
    setRenderError("");
    try {
      const res = await renderPrompt(selectedName, varValues);
      setRenderedPreview(res.data.rendered);
    } catch {
      setRenderError("Render failed. Ensure the prompt is saved.");
    }
  }

  async function handleTestWithModel() {
    let promptText = renderedPreview;
    if (!promptText) {
      // Try to render inline without saved prompt
      promptText = editorContent;
      for (const [k, v] of Object.entries(varValues)) {
        promptText = promptText.replaceAll(`{{${k}}}`, v);
      }
    }
    if (!promptText.trim()) {
      setTestError("No prompt content to test.");
      return;
    }
    setTestLoading(true);
    setTestError("");
    setTestResponse("");
    try {
      const res = await apiClient.post("/v1/chat/completions", {
        model: testModel,
        messages: [{ role: "user", content: promptText }],
      });
      const choice = res.data.choices?.[0];
      setTestResponse(choice?.message?.content ?? JSON.stringify(res.data, null, 2));
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: { message?: string } } } })
          ?.response?.data?.error?.message ?? "Request failed";
      setTestError(msg);
    } finally {
      setTestLoading(false);
    }
  }

  const hasEditor = isNew || selectedName != null;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Prompt Hub</h1>
        <Badge variant="outline" className="text-zinc-400">
          {prompts.length} prompt{prompts.length !== 1 ? "s" : ""}
        </Badge>
      </div>

      <div className="flex gap-4 h-[calc(100vh-10rem)] min-h-[500px]">
        {/* Left panel — prompt list */}
        <div className="w-56 shrink-0 flex flex-col gap-2">
          <Button size="sm" className="w-full" onClick={handleNewPrompt}>
            + New Prompt
          </Button>
          <Input
            placeholder="Search…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-8 text-sm"
          />
          <div className="flex-1 overflow-y-auto space-y-1">
            {promptsLoading && (
              <p className="text-xs text-zinc-500 px-1 mt-2">Loading…</p>
            )}
            {filtered.map((p) => (
              <button
                key={p.id}
                onClick={() => handleSelectPrompt(p)}
                className={[
                  "w-full text-left px-3 py-2 rounded-md text-sm transition-colors",
                  selectedName === p.name && !isNew
                    ? "bg-indigo-600 text-white"
                    : "text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100",
                ].join(" ")}
              >
                <div className="font-medium truncate">{p.name}</div>
                <div className="text-xs opacity-60">v{p.version}</div>
              </button>
            ))}
            {!promptsLoading && filtered.length === 0 && (
              <p className="text-xs text-zinc-500 px-1 mt-2">No prompts found.</p>
            )}
          </div>
        </div>

        {/* Center + Right panels */}
        {hasEditor ? (
          <div className="flex-1 flex gap-4 min-w-0">
            {/* Center panel — editor */}
            <div className="flex-1 flex flex-col gap-3 min-w-0">
              <Card className="flex-1 flex flex-col">
                <CardContent className="flex-1 flex flex-col pt-4 gap-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="flex-1">
                      <Label className="text-xs text-zinc-400 mb-1 block">
                        Prompt Name
                      </Label>
                      <Input
                        placeholder="e.g. summarize-email"
                        value={editorName}
                        onChange={(e) => setEditorName(e.target.value)}
                        disabled={!isNew}
                        className="h-8 text-sm font-mono"
                      />
                    </div>
                    <div className="flex items-center gap-2 pt-5">
                      {currentVersion != null && (
                        <Badge variant="outline" className="text-zinc-400 shrink-0">
                          v{currentVersion}
                        </Badge>
                      )}
                      {!isNew && selectedName && currentVersion != null && (
                        <button
                          onClick={handleShowHistory}
                          className="text-xs text-indigo-400 hover:underline shrink-0"
                        >
                          History
                        </button>
                      )}
                      {!isNew && selectedName && currentVersion != null && currentVersion >= 2 && (
                        <button
                          onClick={handleShowDiff}
                          className="text-xs text-violet-400 hover:underline shrink-0"
                        >
                          Compare
                        </button>
                      )}
                    </div>
                  </div>

                  <div className="flex-1 flex flex-col gap-1">
                    <Label className="text-xs text-zinc-400">Content</Label>
                    <textarea
                      className="flex-1 resize-none rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm font-mono text-zinc-100 placeholder:text-zinc-600 focus:outline-none focus:ring-1 focus:ring-indigo-500 min-h-[200px]"
                      placeholder={"You are a helpful assistant.\n\nUser query: {{query}}"}
                      value={editorContent}
                      onChange={(e) => setEditorContent(e.target.value)}
                      spellCheck={false}
                    />
                  </div>

                  {detectedVars.length > 0 && (
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-xs text-zinc-500">Variables:</span>
                      {detectedVars.map((v) => (
                        <Badge key={v} variant="outline" className="font-mono text-xs text-indigo-300">
                          {`{{${v}}}`}
                        </Badge>
                      ))}
                    </div>
                  )}

                  <div className="flex items-center gap-3">
                    <Button
                      size="sm"
                      disabled={
                        saveMutation.isPending ||
                        !editorName.trim() ||
                        !editorContent.trim()
                      }
                      onClick={() => {
                        setSaveError("");
                        saveMutation.mutate();
                      }}
                    >
                      {saveMutation.isPending ? "Saving…" : "Save"}
                    </Button>
                    {saveSuccess && (
                      <span className="text-xs text-green-400">Saved!</span>
                    )}
                    {saveError && (
                      <span className="text-xs text-red-400">{saveError}</span>
                    )}
                  </div>
                </CardContent>
              </Card>
            </div>

            {/* Right panel — playground */}
            <div className="w-72 shrink-0 flex flex-col gap-3">
              <Card className="flex flex-col">
                <CardContent className="pt-4 flex flex-col gap-3">
                  <p className="text-sm font-semibold text-zinc-200">Playground</p>

                  {detectedVars.length > 0 ? (
                    <div className="space-y-2">
                      <p className="text-xs text-zinc-500">Variable Inputs</p>
                      {detectedVars.map((v) => (
                        <div key={v} className="space-y-1">
                          <Label className="text-xs font-mono text-indigo-300">
                            {`{{${v}}}`}
                          </Label>
                          <Input
                            value={varValues[v] ?? ""}
                            onChange={(e) =>
                              setVarValues((prev) => ({ ...prev, [v]: e.target.value }))
                            }
                            placeholder={`Value for ${v}`}
                            className="h-8 text-sm"
                          />
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p className="text-xs text-zinc-600">
                      Add <span className="font-mono text-zinc-500">{"{{variable}}"}</span> to your prompt to see inputs here.
                    </p>
                  )}

                  <Button
                    size="sm"
                    variant="outline"
                    onClick={handlePreview}
                    disabled={!selectedName}
                  >
                    Preview Rendered
                  </Button>
                  {renderError && (
                    <p className="text-xs text-red-400">{renderError}</p>
                  )}
                  {renderedPreview && (
                    <div className="rounded-md border border-zinc-700 bg-zinc-900 p-3">
                      <p className="text-xs text-zinc-500 mb-1">Rendered output</p>
                      <pre className="text-xs text-zinc-200 whitespace-pre-wrap break-words font-mono">
                        {renderedPreview}
                      </pre>
                    </div>
                  )}
                </CardContent>
              </Card>

              <Card className="flex flex-col">
                <CardContent className="pt-4 flex flex-col gap-3">
                  <p className="text-sm font-semibold text-zinc-200">Test with Model</p>
                  <div className="space-y-1">
                    <Label className="text-xs text-zinc-400">Model</Label>
                    <Input
                      value={testModel}
                      onChange={(e) => setTestModel(e.target.value)}
                      placeholder="gpt-4o"
                      className="h-8 text-sm font-mono"
                    />
                  </div>
                  {costEstimate && (
                    <div className="flex items-center gap-1.5">
                      <Badge variant="outline" className="text-emerald-400 border-emerald-800 font-mono text-xs">
                        Est. cost: ${costEstimate.estimated_cost_usd.toFixed(4)}
                      </Badge>
                      <span className="text-xs text-zinc-600" title={costEstimate.note}>
                        ~{costEstimate.estimated_prompt_tokens} tokens
                      </span>
                    </div>
                  )}
                  <Button
                    size="sm"
                    onClick={handleTestWithModel}
                    disabled={testLoading || !editorContent.trim()}
                  >
                    {testLoading ? "Running…" : "Run"}
                  </Button>
                  {testError && (
                    <p className="text-xs text-red-400">{testError}</p>
                  )}
                  {testResponse && (
                    <div className="rounded-md border border-zinc-700 bg-zinc-900 p-3 max-h-48 overflow-y-auto">
                      <p className="text-xs text-zinc-500 mb-1">Response</p>
                      <pre className="text-xs text-zinc-200 whitespace-pre-wrap break-words">
                        {testResponse}
                      </pre>
                    </div>
                  )}
                </CardContent>
              </Card>
            </div>
          </div>
        ) : (
          <div className="flex-1 flex items-center justify-center text-zinc-600 text-sm">
            Select a prompt or create a new one.
          </div>
        )}
      </div>

      {/* Version diff modal */}
      {showDiff && selectedName && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-zinc-900 border border-zinc-700 rounded-xl w-[900px] max-w-[95vw] max-h-[85vh] flex flex-col shadow-2xl">
            <div className="flex items-center justify-between px-5 py-4 border-b border-zinc-800">
              <h3 className="font-semibold text-zinc-100">
                Version Diff — <span className="font-mono text-violet-300">{selectedName}</span>
              </h3>
              <button
                onClick={() => { setShowDiff(false); setDiffVersionA(null); setDiffVersionB(null); }}
                className="text-zinc-500 hover:text-zinc-100 text-xl leading-none"
              >
                ×
              </button>
            </div>
            <div className="flex-1 overflow-y-auto p-5">
              {historyLoading ? (
                <p className="text-zinc-500 text-sm text-center py-8">Loading versions…</p>
              ) : history.length < 2 ? (
                <p className="text-zinc-500 text-sm text-center py-8">
                  Need at least 2 versions to compare.
                </p>
              ) : (
                <div className="space-y-4">
                  {/* Version selectors */}
                  <div className="flex items-center gap-4">
                    <div className="space-y-1">
                      <Label className="text-xs text-zinc-400">Version A (before)</Label>
                      <select
                        value={diffVersionA?.version ?? ""}
                        onChange={(e) => {
                          const v = history.find((h) => h.version === parseInt(e.target.value, 10));
                          setDiffVersionA(v ?? null);
                        }}
                        className="h-8 rounded border border-zinc-700 bg-zinc-800 px-2 text-sm text-zinc-100"
                      >
                        {[...history].reverse().map((h) => (
                          <option key={h.version} value={h.version}>v{h.version}</option>
                        ))}
                      </select>
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs text-zinc-400">Version B (after)</Label>
                      <select
                        value={diffVersionB?.version ?? ""}
                        onChange={(e) => {
                          const v = history.find((h) => h.version === parseInt(e.target.value, 10));
                          setDiffVersionB(v ?? null);
                        }}
                        className="h-8 rounded border border-zinc-700 bg-zinc-800 px-2 text-sm text-zinc-100"
                      >
                        {[...history].reverse().map((h) => (
                          <option key={h.version} value={h.version}>v{h.version}</option>
                        ))}
                      </select>
                    </div>
                  </div>

                  {diffVersionA && diffVersionB && diffVersionA.version !== diffVersionB.version ? (
                    <PromptDiff
                      promptName={selectedName}
                      versionA={
                        diffVersionA.version < diffVersionB.version ? diffVersionA : diffVersionB
                      }
                      versionB={
                        diffVersionA.version < diffVersionB.version ? diffVersionB : diffVersionA
                      }
                    />
                  ) : (
                    <p className="text-zinc-600 text-sm text-center py-4">
                      Select two different versions to compare.
                    </p>
                  )}
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Version history modal */}
      {showHistory && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-zinc-900 border border-zinc-700 rounded-xl w-[700px] max-h-[80vh] flex flex-col shadow-2xl">
            <div className="flex items-center justify-between px-5 py-4 border-b border-zinc-800">
              <h3 className="font-semibold text-zinc-100">
                Version History — <span className="font-mono text-indigo-300">{selectedName}</span>
              </h3>
              <button
                onClick={() => { setShowHistory(false); setHistorySelected(null); }}
                className="text-zinc-500 hover:text-zinc-100 text-xl leading-none"
              >
                ×
              </button>
            </div>
            <div className="flex flex-1 overflow-hidden">
              {/* Version list */}
              <div className="w-40 shrink-0 border-r border-zinc-800 overflow-y-auto py-2">
                {historyLoading ? (
                  <p className="text-xs text-zinc-500 px-3 mt-2">Loading…</p>
                ) : history.length === 0 ? (
                  <p className="text-xs text-zinc-500 px-3 mt-2">No history.</p>
                ) : (
                  history.map((entry) => (
                    <button
                      key={entry.version}
                      onClick={() => setHistorySelected(entry)}
                      className={[
                        "w-full text-left px-3 py-2 text-sm transition-colors",
                        historySelected?.version === entry.version
                          ? "bg-indigo-600 text-white"
                          : "text-zinc-300 hover:bg-zinc-800",
                      ].join(" ")}
                    >
                      <div className="font-medium">v{entry.version}</div>
                      <div className="text-xs opacity-60">
                        {new Date(entry.created_at).toLocaleDateString()}
                      </div>
                    </button>
                  ))
                )}
              </div>

              {/* Version content view */}
              <div className="flex-1 flex flex-col p-4 gap-3 overflow-hidden">
                {historySelected ? (
                  <>
                    <div className="flex items-center justify-between">
                      <Badge variant="outline" className="text-zinc-400">
                        v{historySelected.version}
                      </Badge>
                      <Button
                        size="sm"
                        onClick={() => handleRestoreVersion(historySelected)}
                      >
                        Restore this version
                      </Button>
                    </div>
                    <textarea
                      readOnly
                      className="flex-1 resize-none rounded-md border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm font-mono text-zinc-300 min-h-[200px]"
                      value={historySelected.content}
                    />
                  </>
                ) : (
                  <p className="text-zinc-600 text-sm self-center mt-8">
                    Select a version to view its content.
                  </p>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
