import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getCacheStats,
  clearTenantCache,
  getCacheConfig,
  setCacheConfig,
  type CacheConfig,
} from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";

type Period = "7d" | "30d" | "90d";

export function CachePage() {
  const qc = useQueryClient();
  const [period, setPeriod] = useState<Period>("30d");
  const [showClearModal, setShowClearModal] = useState(false);
  const [configForm, setConfigForm] = useState<Omit<CacheConfig, "tenant_id"> | null>(null);
  const [saveMsg, setSaveMsg] = useState<string | null>(null);

  const { data: stats, isLoading: statsLoading } = useQuery({
    queryKey: ["cache-stats", period],
    queryFn: () => getCacheStats(period),
  });

  const { data: config, isLoading: configLoading } = useQuery({
    queryKey: ["cache-config"],
    queryFn: getCacheConfig,
  });


  const clearMutation = useMutation({
    mutationFn: clearTenantCache,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["cache-stats"] });
      setShowClearModal(false);
    },
  });

  const saveMutation = useMutation({
    mutationFn: (cfg: Omit<CacheConfig, "tenant_id">) => setCacheConfig(cfg),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["cache-config"] });
      setSaveMsg("Saved.");
      setTimeout(() => setSaveMsg(null), 3000);
    },
  });

  const form = configForm ?? {
    exact_ttl_seconds: config?.exact_ttl_seconds ?? 3600,
    semantic_ttl_seconds: config?.semantic_ttl_seconds ?? 7200,
    semantic_enabled: config?.semantic_enabled ?? true,
  };

  function handleSaveConfig() {
    if (!configForm) return;
    saveMutation.mutate(configForm);
  }

  const periodLabels: { value: Period; label: string }[] = [
    { value: "7d", label: "7 days" },
    { value: "30d", label: "30 days" },
    { value: "90d", label: "90 days" },
  ];

  const ttlPresets = [
    { label: "1h", value: 3600 },
    { label: "6h", value: 21600 },
    { label: "24h", value: 86400 },
  ];

  const semTtlPresets = [
    { label: "2h", value: 7200 },
    { label: "12h", value: 43200 },
    { label: "48h", value: 172800 },
  ];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Cache Management</h1>
        <div className="flex gap-2">
          {periodLabels.map((p) => (
            <button
              key={p.value}
              onClick={() => setPeriod(p.value)}
              className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
                period === p.value
                  ? "bg-indigo-600 text-white"
                  : "bg-zinc-800 text-zinc-400 hover:text-zinc-100"
              }`}
            >
              {p.label}
            </button>
          ))}
        </div>
      </div>

      {/* Stats cards */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Total Hits</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">
              {statsLoading ? "—" : (stats?.total_hits ?? 0).toLocaleString()}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Exact Hits</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">
              {statsLoading ? "—" : (stats?.exact_hits ?? 0).toLocaleString()}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Semantic Hits</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold">
              {statsLoading ? "—" : (stats?.semantic_hits ?? 0).toLocaleString()}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-zinc-400 font-normal">Savings</CardTitle>
          </CardHeader>
          <CardContent>
            {statsLoading ? (
              <p className="text-3xl font-bold">—</p>
            ) : (
              <>
                <p className="text-3xl font-bold text-emerald-400">
                  ${(stats?.estimated_savings_usd ?? 0).toFixed(2)}
                </p>
                {(stats?.estimated_savings_usd ?? 0) > 0 && (
                  <p className="text-xs text-emerald-500 mt-1">
                    This {period} saved you{" "}
                    <span className="font-semibold">
                      ${(stats?.estimated_savings_usd ?? 0).toFixed(2)}
                    </span>
                  </p>
                )}
              </>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Avg latency saved */}
      {(stats?.avg_latency_saved_ms ?? 0) > 0 && (
        <p className="text-sm text-zinc-400">
          Average latency saved per cache hit:{" "}
          <span className="text-zinc-200 font-medium">~{stats!.avg_latency_saved_ms} ms</span>
        </p>
      )}

      {/* Top models by savings */}
      {(stats?.top_saved_models?.length ?? 0) > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Top Models by Savings</CardTitle>
          </CardHeader>
          <CardContent>
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 font-medium">Model</th>
                  <th className="text-right py-2 font-medium">Hits</th>
                  <th className="text-right py-2 font-medium">Savings (USD)</th>
                </tr>
              </thead>
              <tbody>
                {stats!.top_saved_models.map((m) => (
                  <tr key={m.model} className="border-b border-zinc-800/50">
                    <td className="py-2 font-mono text-xs">{m.model}</td>
                    <td className="text-right">{m.hits.toLocaleString()}</td>
                    <td className="text-right text-emerald-400">${m.savings_usd.toFixed(4)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}

      {/* Config section */}
      <Card>
        <CardHeader>
          <CardTitle>Cache Configuration</CardTitle>
        </CardHeader>
        <CardContent className="space-y-6">
          {configLoading ? (
            <p className="text-zinc-500 text-sm">Loading...</p>
          ) : (
            <>
              {/* Exact cache TTL */}
              <div className="space-y-2">
                <label className="block text-sm font-medium text-zinc-300">
                  Exact Cache TTL (seconds)
                </label>
                <div className="flex items-center gap-3">
                  <input
                    type="number"
                    min={60}
                    value={form.exact_ttl_seconds}
                    onChange={(e) =>
                      setConfigForm((f) => ({
                        ...(f ?? form),
                        exact_ttl_seconds: parseInt(e.target.value, 10) || 3600,
                      }))
                    }
                    className="w-32 bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-zinc-100 focus:outline-none focus:border-indigo-500"
                  />
                  <span className="text-xs text-zinc-500">Presets:</span>
                  {ttlPresets.map((p) => (
                    <button
                      key={p.label}
                      onClick={() =>
                        setConfigForm((f) => ({ ...(f ?? form), exact_ttl_seconds: p.value }))
                      }
                      className="px-2 py-1 rounded text-xs bg-zinc-800 text-zinc-400 hover:text-zinc-100 hover:bg-zinc-700 transition-colors"
                    >
                      {p.label}
                    </button>
                  ))}
                </div>
              </div>

              {/* Semantic cache TTL + toggle */}
              <div className="space-y-2">
                <div className="flex items-center gap-3">
                  <label className="block text-sm font-medium text-zinc-300">
                    Semantic Cache TTL (seconds)
                  </label>
                  <button
                    onClick={() =>
                      setConfigForm((f) => ({
                        ...(f ?? form),
                        semantic_enabled: !(f ?? form).semantic_enabled,
                      }))
                    }
                    className={`relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors ${
                      form.semantic_enabled ? "bg-indigo-600" : "bg-zinc-700"
                    }`}
                    role="switch"
                    aria-checked={form.semantic_enabled}
                  >
                    <span
                      className={`inline-block h-4 w-4 rounded-full bg-white shadow transition-transform ${
                        form.semantic_enabled ? "translate-x-4" : "translate-x-0"
                      }`}
                    />
                  </button>
                  <span className="text-xs text-zinc-500">
                    {form.semantic_enabled ? "Enabled" : "Disabled"}
                  </span>
                </div>
                <div className="flex items-center gap-3">
                  <input
                    type="number"
                    min={60}
                    disabled={!form.semantic_enabled}
                    value={form.semantic_ttl_seconds}
                    onChange={(e) =>
                      setConfigForm((f) => ({
                        ...(f ?? form),
                        semantic_ttl_seconds: parseInt(e.target.value, 10) || 7200,
                      }))
                    }
                    className="w-32 bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-zinc-100 focus:outline-none focus:border-indigo-500 disabled:opacity-40"
                  />
                  <span className="text-xs text-zinc-500">Presets:</span>
                  {semTtlPresets.map((p) => (
                    <button
                      key={p.label}
                      disabled={!form.semantic_enabled}
                      onClick={() =>
                        setConfigForm((f) => ({
                          ...(f ?? form),
                          semantic_ttl_seconds: p.value,
                        }))
                      }
                      className="px-2 py-1 rounded text-xs bg-zinc-800 text-zinc-400 hover:text-zinc-100 hover:bg-zinc-700 transition-colors disabled:opacity-40 disabled:pointer-events-none"
                    >
                      {p.label}
                    </button>
                  ))}
                </div>
              </div>

              <div className="flex items-center gap-4">
                <button
                  onClick={handleSaveConfig}
                  disabled={saveMutation.isPending}
                  className="px-4 py-2 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 rounded text-sm font-medium transition-colors"
                >
                  {saveMutation.isPending ? "Saving..." : "Save Config"}
                </button>
                {saveMsg && <span className="text-sm text-emerald-400">{saveMsg}</span>}
                {saveMutation.isError && (
                  <span className="text-sm text-red-400">Save failed. Please try again.</span>
                )}
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Danger zone */}
      <Card>
        <CardHeader>
          <CardTitle className="text-red-400">Danger Zone</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-sm text-zinc-400">
            Clearing the cache removes all cached responses and resets hit counters for your
            tenant. Requests will hit upstream providers until the cache is warm again.
          </p>
          <button
            onClick={() => setShowClearModal(true)}
            className="px-4 py-2 bg-red-700 hover:bg-red-600 rounded text-sm font-medium transition-colors"
          >
            Clear Cache
          </button>
        </CardContent>
      </Card>

      {/* Clear confirmation modal */}
      {showClearModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="bg-zinc-900 border border-zinc-700 rounded-lg p-6 w-full max-w-md space-y-4">
            <h2 className="text-lg font-semibold">Clear tenant cache?</h2>
            <p className="text-sm text-zinc-400">
              This will flush all exact and semantic cache entries for your tenant. This action
              cannot be undone.
            </p>
            {clearMutation.isError && (
              <p className="text-sm text-red-400">Failed to clear cache. Please try again.</p>
            )}
            <div className="flex justify-end gap-3">
              <button
                onClick={() => setShowClearModal(false)}
                className="px-4 py-2 rounded text-sm bg-zinc-800 text-zinc-300 hover:bg-zinc-700 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={() => clearMutation.mutate()}
                disabled={clearMutation.isPending}
                className="px-4 py-2 rounded text-sm bg-red-700 hover:bg-red-600 disabled:opacity-50 transition-colors"
              >
                {clearMutation.isPending ? "Clearing..." : "Yes, clear cache"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
