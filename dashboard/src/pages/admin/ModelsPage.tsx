import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listModels, createModel, updateModelPricing } from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";

const PROVIDERS = ["openai", "anthropic", "local"];

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

  const { data } = useQuery({
    queryKey: ["models"],
    queryFn: () => listModels().then((r) => r.data),
  });

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
    </div>
  );
}
