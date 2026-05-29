import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listWebhooks,
  createWebhook,
  deleteWebhook,
  testWebhook,
  type OutboundWebhookConfig,
} from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "../../components/ui/dialog";

const AVAILABLE_EVENTS = [
  { value: "fine_tuning.succeeded", label: "Fine-tuning completed (success)" },
  { value: "fine_tuning.failed", label: "Fine-tuning completed (failure)" },
  { value: "eval_run.completed", label: "Eval run completed" },
  { value: "batch.completed", label: "Batch job completed" },
];

const emptyForm = { name: "", url: "", secret: "", events: [] as string[] };

export function WebhooksPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState(emptyForm);
  const [toast, setToast] = useState<{ msg: string; ok: boolean } | null>(null);

  const showToast = (msg: string, ok: boolean) => {
    setToast({ msg, ok });
    setTimeout(() => setToast(null), 3500);
  };

  const { data: webhooks = [] } = useQuery({
    queryKey: ["outbound-webhooks"],
    queryFn: listWebhooks,
  });

  const createMutation = useMutation({
    mutationFn: createWebhook,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["outbound-webhooks"] });
      setOpen(false);
      setForm(emptyForm);
      showToast("Webhook created.", true);
    },
    onError: () => showToast("Failed to create webhook.", false),
  });

  const deleteMutation = useMutation({
    mutationFn: deleteWebhook,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["outbound-webhooks"] });
      showToast("Webhook deleted.", true);
    },
    onError: () => showToast("Failed to delete webhook.", false),
  });

  const testMutation = useMutation({
    mutationFn: testWebhook,
    onSuccess: () => showToast("Ping sent successfully.", true),
    onError: () => showToast("Test ping failed.", false),
  });

  const toggleEvent = (eventValue: string) => {
    setForm((f) => ({
      ...f,
      events: f.events.includes(eventValue)
        ? f.events.filter((e) => e !== eventValue)
        : [...f.events, eventValue],
    }));
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (form.events.length === 0) {
      showToast("Select at least one event type.", false);
      return;
    }
    createMutation.mutate(form);
  };

  return (
    <div className="space-y-6">
      {toast && (
        <div
          className={`fixed top-4 right-4 z-50 rounded-md px-4 py-2 text-sm text-white shadow-lg ${
            toast.ok ? "bg-green-700" : "bg-red-700"
          }`}
        >
          {toast.msg}
        </div>
      )}

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Webhooks</h1>
          <p className="text-zinc-400 text-sm mt-1">
            Receive HTTP notifications when fine-tuning, eval runs, or batch jobs complete.
          </p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button>+ Add Webhook</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Add Webhook</DialogTitle>
            </DialogHeader>
            <form onSubmit={handleSubmit} className="space-y-4">
              <div className="space-y-1">
                <Label>Name</Label>
                <Input
                  placeholder="e.g. prod-notifications"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-1">
                <Label>URL</Label>
                <Input
                  type="url"
                  placeholder="https://your-server.com/hooks/totra"
                  value={form.url}
                  onChange={(e) => setForm({ ...form, url: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-1">
                <Label>Secret</Label>
                <Input
                  type="password"
                  placeholder="Used to verify X-ToTra-Signature header"
                  value={form.secret}
                  onChange={(e) => setForm({ ...form, secret: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-2">
                <Label>Events</Label>
                {AVAILABLE_EVENTS.map((ev) => (
                  <label key={ev.value} className="flex items-center gap-2 text-sm cursor-pointer">
                    <input
                      type="checkbox"
                      className="accent-blue-500"
                      checked={form.events.includes(ev.value)}
                      onChange={() => toggleEvent(ev.value)}
                    />
                    <span className="text-zinc-300">{ev.label}</span>
                  </label>
                ))}
              </div>
              <Button
                type="submit"
                className="w-full"
                disabled={createMutation.isPending}
              >
                {createMutation.isPending ? "Saving..." : "Save Webhook"}
              </Button>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <Card>
        <CardContent className="pt-4">
          {webhooks.length === 0 ? (
            <p className="text-zinc-500 text-sm py-4 text-center">
              No webhooks configured. Add one to receive event notifications.
            </p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 font-medium">Name</th>
                  <th className="text-left py-2 font-medium">URL</th>
                  <th className="text-left py-2 font-medium">Events</th>
                  <th className="text-right py-2 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {webhooks.map((wh: OutboundWebhookConfig) => (
                  <tr key={wh.id} className="border-b border-zinc-800/50">
                    <td className="py-2 font-medium">{wh.name}</td>
                    <td className="py-2 text-zinc-400 text-xs truncate max-w-[220px]">
                      {wh.url}
                    </td>
                    <td className="py-2">
                      <div className="flex flex-wrap gap-1">
                        {wh.events.map((ev) => (
                          <Badge key={ev} variant="secondary" className="text-xs">
                            {ev}
                          </Badge>
                        ))}
                      </div>
                    </td>
                    <td className="py-2 text-right space-x-2">
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={testMutation.isPending}
                        onClick={() => testMutation.mutate(wh.id)}
                      >
                        Test
                      </Button>
                      <Button
                        size="sm"
                        variant="destructive"
                        disabled={deleteMutation.isPending}
                        onClick={() => deleteMutation.mutate(wh.id)}
                      >
                        Delete
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
