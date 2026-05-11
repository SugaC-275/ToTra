import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { getWebhookConfigs, createWebhookConfig } from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";

export function IntegrationsPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ platform: "github", webhook_secret: "" });

  const { data } = useQuery({
    queryKey: ["webhook-configs"],
    queryFn: () => getWebhookConfigs().then((r) => r.data),
  });

  const createMutation = useMutation({
    mutationFn: createWebhookConfig,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["webhook-configs"] });
      setOpen(false);
      setForm({ platform: "github", webhook_secret: "" });
    },
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    createMutation.mutate(form);
  };

  const webhookUrl = (platform: string) =>
    `${window.location.protocol}//${window.location.hostname}:8081/webhooks/${platform}?tenant_id=<YOUR_TENANT_ID>`;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Integrations</h1>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button>+ Add Integration</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Configure Webhook</DialogTitle>
            </DialogHeader>
            <form onSubmit={handleSubmit} className="space-y-4">
              <div className="space-y-1">
                <Label>Platform</Label>
                <select
                  className="w-full h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100"
                  value={form.platform}
                  onChange={(e) => setForm({ ...form, platform: e.target.value })}
                >
                  {["github", "jira", "feishu", "dingtalk", "gitlab", "confluence"].map((p) => {
                    const labels: Record<string, string> = {
                      github: "GitHub",
                      jira: "Jira",
                      feishu: "飞书",
                      dingtalk: "DingTalk",
                      gitlab: "GitLab",
                      confluence: "Confluence",
                    };
                    return <option key={p} value={p}>{labels[p]}</option>;
                  })}
                </select>
              </div>
              <div className="space-y-1">
                <Label>Webhook Secret</Label>
                <Input
                  type="password"
                  placeholder="Secret configured in GitHub / Jira / 飞书 / GitLab / Confluence"
                  value={form.webhook_secret}
                  onChange={(e) => setForm({ ...form, webhook_secret: e.target.value })}
                  required
                />
              </div>
              <div className="rounded-md bg-zinc-800 p-3 text-xs text-zinc-400 break-all">
                <p className="font-medium text-zinc-300 mb-1">Webhook URL to register:</p>
                <code>{webhookUrl(form.platform)}</code>
              </div>
              <Button type="submit" className="w-full" disabled={createMutation.isPending}>
                {createMutation.isPending ? "Saving..." : "Save Integration"}
              </Button>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <Card>
        <CardContent className="pt-4">
          {!data?.configs?.length ? (
            <p className="text-zinc-500 text-sm py-4 text-center">No integrations configured.</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 font-medium">Platform</th>
                  <th className="text-left py-2 font-medium">Webhook URL</th>
                  <th className="text-right py-2 font-medium">Status</th>
                </tr>
              </thead>
              <tbody>
                {data.configs.map((c) => (
                  <tr key={c.id} className="border-b border-zinc-800/50">
                    <td className="py-2 capitalize">{c.platform}</td>
                    <td className="text-zinc-400 text-xs truncate max-w-[300px]">
                      {webhookUrl(c.platform)}
                    </td>
                    <td className="text-right">
                      <Badge variant={c.is_active ? "default" : "secondary"}>
                        {c.is_active ? "Active" : "Inactive"}
                      </Badge>
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
