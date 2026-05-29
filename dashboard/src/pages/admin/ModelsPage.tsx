import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listModels, createModel, updateModelPricing,
  getFailoverChain, setFailoverChain, clearFailoverChain,
  updateModelComplianceTags, type ModelConfig,
} from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";

const PROVIDERS = ["openai", "anthropic", "local"];

// ---- Failover chain modal ----

interface FailoverModalProps {
  modelName: string;
  allModelNames: string[];
  onClose: () => void;
}

function FailoverModal({ modelName, allModelNames, onClose }: FailoverModalProps) {
  const qc = useQueryClient();

  const { data, isLoading } = useQuery({
    queryKey: ["failover", modelName],
    queryFn: () => getFailoverChain(modelName),
  });

  const [chain, setChain] = useState<string[]>(() => data?.chain ?? []);
  const [addValue, setAddValue] = useState("");

  // Sync chain from server once loaded (only on first load).
  const serverChain = data?.chain ?? [];
  const [synced, setSynced] = useState(false);
  if (!synced && !isLoading && data) {
    setChain(serverChain);
    setSynced(true);
  }

  const saveMutation = useMutation({
    mutationFn: () => setFailoverChain(modelName, chain),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["failover", modelName] });
      onClose();
    },
  });

  const clearMutation = useMutation({
    mutationFn: () => clearFailoverChain(modelName),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["failover", modelName] });
      onClose();
    },
  });

  const moveUp = (i: number) => {
    if (i === 0) return;
    const next = [...chain];
    [next[i - 1], next[i]] = [next[i], next[i - 1]];
    setChain(next);
  };

  const moveDown = (i: number) => {
    if (i === chain.length - 1) return;
    const next = [...chain];
    [next[i], next[i + 1]] = [next[i + 1], next[i]];
    setChain(next);
  };

  const remove = (i: number) => setChain(chain.filter((_, idx) => idx !== i));

  const addModel = () => {
    if (!addValue || chain.includes(addValue) || addValue === modelName) return;
    setChain([...chain, addValue]);
    setAddValue("");
  };

  const available = allModelNames.filter((n) => n !== modelName && !chain.includes(n));

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
      <div className="bg-zinc-900 border border-zinc-700 rounded-xl p-6 space-y-5 w-[460px] max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between">
          <h3 className="font-semibold text-zinc-100">Failover Chain — <span className="font-mono text-sm text-blue-400">{modelName}</span></h3>
          <button onClick={onClose} className="text-zinc-400 hover:text-zinc-100 text-lg leading-none">&times;</button>
        </div>

        {isLoading ? (
          <p className="text-zinc-400 text-sm">Loading...</p>
        ) : (
          <>
            {/* Current chain as ordered badges */}
            <div className="flex flex-wrap items-center gap-1 min-h-[32px]">
              <span className="font-mono text-xs text-zinc-300 bg-zinc-800 border border-zinc-600 px-2 py-1 rounded">{modelName}</span>
              {chain.map((name, i) => (
                <span key={i} className="flex items-center gap-1">
                  <span className="text-zinc-500 text-xs">→</span>
                  <span className="font-mono text-xs text-green-300 bg-zinc-800 border border-zinc-600 px-2 py-1 rounded">{name}</span>
                </span>
              ))}
              {chain.length === 0 && (
                <span className="text-zinc-500 text-xs italic">No fallbacks configured</span>
              )}
            </div>

            {/* Edit list */}
            {chain.length > 0 && (
              <ul className="space-y-1">
                {chain.map((name, i) => (
                  <li key={i} className="flex items-center gap-2 bg-zinc-800 rounded px-3 py-2">
                    <span className="text-zinc-400 text-xs w-4">{i + 1}.</span>
                    <span className="flex-1 font-mono text-sm text-zinc-100">{name}</span>
                    <button
                      onClick={() => moveUp(i)}
                      disabled={i === 0}
                      className="text-zinc-400 hover:text-zinc-100 disabled:opacity-30 text-xs px-1"
                      title="Move up"
                    >
                      ↑
                    </button>
                    <button
                      onClick={() => moveDown(i)}
                      disabled={i === chain.length - 1}
                      className="text-zinc-400 hover:text-zinc-100 disabled:opacity-30 text-xs px-1"
                      title="Move down"
                    >
                      ↓
                    </button>
                    <button
                      onClick={() => remove(i)}
                      className="text-red-400 hover:text-red-300 text-xs px-1"
                      title="Remove"
                    >
                      &times;
                    </button>
                  </li>
                ))}
              </ul>
            )}

            {/* Add fallback model */}
            <div className="flex gap-2">
              <select
                className="flex-1 h-9 rounded-md border border-zinc-600 bg-zinc-800 px-3 text-sm text-zinc-100"
                value={addValue}
                onChange={(e) => setAddValue(e.target.value)}
              >
                <option value="">Add fallback model...</option>
                {available.map((n) => (
                  <option key={n} value={n}>{n}</option>
                ))}
              </select>
              <button
                onClick={addModel}
                disabled={!addValue}
                className="px-3 py-1 bg-blue-600 text-white text-sm rounded hover:bg-blue-700 disabled:opacity-40"
              >
                Add
              </button>
            </div>

            <div className="flex gap-2 justify-end pt-1">
              <button
                onClick={() => clearMutation.mutate()}
                disabled={clearMutation.isPending || chain.length === 0}
                className="px-3 py-2 text-sm border border-red-700 text-red-400 rounded hover:bg-red-900/30 disabled:opacity-40"
              >
                Clear All
              </button>
              <button onClick={onClose} className="px-3 py-2 text-sm border border-zinc-600 text-zinc-300 rounded hover:bg-zinc-800">
                Cancel
              </button>
              <button
                onClick={() => saveMutation.mutate()}
                disabled={saveMutation.isPending}
                className="px-4 py-2 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
              >
                {saveMutation.isPending ? "Saving..." : "Save"}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

// ---- Compliance Tags Modal ----

const DATA_REGIONS = [
  { value: "", label: "(None)" },
  { value: "us", label: "US" },
  { value: "eu", label: "EU" },
  { value: "us-gov", label: "US-Gov" },
  { value: "au", label: "AU" },
];

interface ComplianceTagsModalProps {
  model: ModelConfig;
  onClose: () => void;
}

function ComplianceTagsModal({ model, onClose }: ComplianceTagsModalProps) {
  const qc = useQueryClient();
  const [hipaa, setHipaa] = useState(model.hipaa_eligible ?? false);
  const [govcloud, setGovcloud] = useState(model.govcloud ?? false);
  const [fedramp, setFedramp] = useState(model.fedramp_auth ?? false);
  const [region, setRegion] = useState(model.data_region ?? "");
  const [notes, setNotes] = useState(model.compliance_notes ?? "");

  const saveMutation = useMutation({
    mutationFn: () =>
      updateModelComplianceTags(model.id, {
        hipaa_eligible: hipaa,
        govcloud: govcloud,
        fedramp_auth: fedramp,
        data_region: region,
        compliance_notes: notes,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["models"] });
      onClose();
    },
  });

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
      <div className="bg-zinc-900 border border-zinc-700 rounded-xl p-6 space-y-5 w-[420px] max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between">
          <h3 className="font-semibold text-zinc-100">
            Compliance Tags — <span className="font-mono text-sm text-blue-400">{model.name}</span>
          </h3>
          <button onClick={onClose} className="text-zinc-400 hover:text-zinc-100 text-lg leading-none">&times;</button>
        </div>

        <div className="space-y-3">
          <label className="flex items-center gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={hipaa}
              onChange={(e) => setHipaa(e.target.checked)}
              className="h-4 w-4 rounded border-zinc-600 bg-zinc-800 text-blue-500"
            />
            <span className="text-sm text-zinc-200">HIPAA Eligible</span>
          </label>

          <label className="flex items-center gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={govcloud}
              onChange={(e) => setGovcloud(e.target.checked)}
              className="h-4 w-4 rounded border-zinc-600 bg-zinc-800 text-blue-500"
            />
            <span className="text-sm text-zinc-200">GovCloud / FedRAMP Authorized</span>
          </label>

          <label className="flex items-center gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={fedramp}
              onChange={(e) => setFedramp(e.target.checked)}
              className="h-4 w-4 rounded border-zinc-600 bg-zinc-800 text-blue-500"
            />
            <span className="text-sm text-zinc-200">FedRAMP Auth on Record</span>
          </label>

          <div className="space-y-1">
            <label className="text-xs text-zinc-400 font-medium">Data Region</label>
            <select
              className="w-full h-9 rounded-md border border-zinc-600 bg-zinc-800 px-3 text-sm text-zinc-100"
              value={region}
              onChange={(e) => setRegion(e.target.value)}
            >
              {DATA_REGIONS.map((r) => (
                <option key={r.value} value={r.value}>{r.label}</option>
              ))}
            </select>
          </div>

          <div className="space-y-1">
            <label className="text-xs text-zinc-400 font-medium">Compliance Notes</label>
            <textarea
              className="w-full rounded-md border border-zinc-600 bg-zinc-800 px-3 py-2 text-sm text-zinc-100 resize-none"
              rows={3}
              placeholder="Optional notes for compliance team..."
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
            />
          </div>
        </div>

        <div className="flex gap-2 justify-end pt-1">
          <button
            onClick={onClose}
            className="px-3 py-2 text-sm border border-zinc-600 text-zinc-300 rounded hover:bg-zinc-800"
          >
            Cancel
          </button>
          <button
            onClick={() => saveMutation.mutate()}
            disabled={saveMutation.isPending}
            className="px-4 py-2 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
          >
            {saveMutation.isPending ? "Saving..." : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}

// ---- Main page ----

export function ModelsPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({
    name: "",
    provider: "openai",
    base_url: "",
    api_key: "",
    scu_rate: "1.0",
  });
  const [pricingModelId, setPricingModelId] = useState<string | null>(null);
  const [inputPrice, setInputPrice] = useState("");
  const [outputPrice, setOutputPrice] = useState("");
  const [failoverModel, setFailoverModel] = useState<string | null>(null);
  const [complianceModel, setComplianceModel] = useState<ModelConfig | null>(null);

  const { data } = useQuery({
    queryKey: ["models"],
    queryFn: () => listModels().then((r) => r.data),
  });

  const allModelNames = (data?.models ?? []).map((m) => m.name);

  const createMutation = useMutation({
    mutationFn: createModel,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["models"] });
      setOpen(false);
      setForm({ name: "", provider: "openai", base_url: "", api_key: "", scu_rate: "1.0" });
    },
  });

  const pricingMutation = useMutation({
    mutationFn: ({ id, input, output }: { id: string; input: number; output: number }) =>
      updateModelPricing(id, { price_per_m_input: input, price_per_m_output: output }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["models"] });
      setPricingModelId(null);
      setInputPrice(""); setOutputPrice("");
    },
  });

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    createMutation.mutate({ ...form, scu_rate: parseFloat(form.scu_rate) });
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Models</h1>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button>+ Add Model</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Add Model</DialogTitle>
            </DialogHeader>
            <form onSubmit={handleCreate} className="space-y-4">
              <div className="space-y-1">
                <Label>Model Name</Label>
                <Input
                  placeholder="e.g. gpt-4o"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-1">
                <Label>Provider</Label>
                <select
                  className="w-full h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100"
                  value={form.provider}
                  onChange={(e) => setForm({ ...form, provider: e.target.value })}
                >
                  {PROVIDERS.map((p) => (
                    <option key={p} value={p}>{p}</option>
                  ))}
                </select>
              </div>
              <div className="space-y-1">
                <Label>Base URL</Label>
                <Input
                  placeholder="https://api.openai.com"
                  value={form.base_url}
                  onChange={(e) => setForm({ ...form, base_url: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-1">
                <Label>API Key</Label>
                <Input
                  type="password"
                  placeholder="sk-..."
                  value={form.api_key}
                  onChange={(e) => setForm({ ...form, api_key: e.target.value })}
                />
              </div>
              <div className="space-y-1">
                <Label>SCU Rate (per token)</Label>
                <Input
                  type="number"
                  step="0.0001"
                  min="0"
                  value={form.scu_rate}
                  onChange={(e) => setForm({ ...form, scu_rate: e.target.value })}
                  required
                />
              </div>
              <Button type="submit" className="w-full" disabled={createMutation.isPending}>
                {createMutation.isPending ? "Adding..." : "Add Model"}
              </Button>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <Card>
        <CardContent className="pt-4">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-zinc-800 text-zinc-400">
                <th className="text-left py-2 font-medium">Name</th>
                <th className="text-left py-2 font-medium">Provider</th>
                <th className="text-left py-2 font-medium">Base URL</th>
                <th className="text-right py-2 font-medium">SCU Rate</th>
                <th className="text-right py-2 font-medium">Pricing</th>
                <th className="text-right py-2 font-medium">Compliance</th>
                <th className="text-right py-2 font-medium">Failover</th>
                <th className="text-right py-2 font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {data?.models?.map((m) => (
                <tr key={m.id} className="border-b border-zinc-800/50">
                  <td className="py-2 font-mono text-xs">{m.name}</td>
                  <td>
                    <Badge variant="outline">{m.provider}</Badge>
                  </td>
                  <td className="text-zinc-400 text-xs truncate max-w-[200px]">{m.base_url}</td>
                  <td className="text-right">{m.scu_rate}</td>
                  <td className="text-right">
                    <button
                      onClick={() => { setPricingModelId(m.id); setInputPrice(m.price_per_m_input?.toString() ?? ""); setOutputPrice(m.price_per_m_output?.toString() ?? ""); }}
                      className="text-xs text-blue-600 hover:underline"
                    >
                      {m.price_per_m_input != null ? `$${m.price_per_m_input}/M` : "Set Price"}
                    </button>
                  </td>
                  <td className="text-right">
                    <button
                      onClick={() => setComplianceModel(m)}
                      className="text-xs text-emerald-400 hover:underline"
                    >
                      {m.hipaa_eligible || m.govcloud ? "Tagged" : "Set Tags"}
                    </button>
                  </td>
                  <td className="text-right">
                    <button
                      onClick={() => setFailoverModel(m.name)}
                      className="text-xs text-purple-400 hover:underline"
                    >
                      Edit Chain
                    </button>
                  </td>
                  <td className="text-right">
                    <Badge variant={m.is_active ? "default" : "secondary"}>
                      {m.is_active ? "Active" : "Inactive"}
                    </Badge>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>

      {pricingModelId && (
        <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50">
          <div className="bg-white rounded-xl p-6 space-y-4 w-80">
            <h3 className="font-semibold">Set Model Pricing (per 1M tokens)</h3>
            <div className="space-y-2">
              <label className="block text-sm">
                Input Price ($)
                <input
                  type="number" step="0.01" value={inputPrice}
                  onChange={(e) => setInputPrice(e.target.value)}
                  className="block w-full border rounded px-3 py-2 mt-1"
                  placeholder="e.g. 5.00"
                />
              </label>
              <label className="block text-sm">
                Output Price ($)
                <input
                  type="number" step="0.01" value={outputPrice}
                  onChange={(e) => setOutputPrice(e.target.value)}
                  className="block w-full border rounded px-3 py-2 mt-1"
                  placeholder="e.g. 15.00"
                />
              </label>
            </div>
            <div className="flex gap-2 justify-end">
              <button onClick={() => setPricingModelId(null)} className="px-4 py-2 border rounded">Cancel</button>
              <button
                disabled={pricingMutation.isPending}
                onClick={() => pricingMutation.mutate({ id: pricingModelId, input: parseFloat(inputPrice), output: parseFloat(outputPrice) })}
                className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
              >
                Save
              </button>
            </div>
          </div>
        </div>
      )}

      {failoverModel && (
        <FailoverModal
          modelName={failoverModel}
          allModelNames={allModelNames}
          onClose={() => setFailoverModel(null)}
        />
      )}

      {complianceModel && (
        <ComplianceTagsModal
          model={complianceModel}
          onClose={() => setComplianceModel(null)}
        />
      )}
    </div>
  );
}
