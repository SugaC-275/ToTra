import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listBotConfigs,
  addBotConfig,
  deleteBotConfig,
  sendKPISummary,
  sendTestBotMessage,
} from "../../api/client";
import type { BotConfig } from "../../api/client";

export default function BotConfigPage() {
  const queryClient = useQueryClient();
  const [platform, setPlatform] = useState("feishu");
  const [webhookUrl, setWebhookUrl] = useState("");
  const [label, setLabel] = useState("");
  const [addError, setAddError] = useState("");
  const [summaryMonth, setSummaryMonth] = useState(
    new Date().toISOString().slice(0, 7)
  );
  const [summaryMsg, setSummaryMsg] = useState("");

  const { data, isLoading } = useQuery({
    queryKey: ["bot-configs"],
    queryFn: listBotConfigs,
  });

  const addMutation = useMutation({
    mutationFn: addBotConfig,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["bot-configs"] });
      setWebhookUrl("");
      setLabel("");
      setAddError("");
    },
    onError: (err: any) => {
      setAddError(err?.response?.data?.error ?? "Failed to add bot config");
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteBotConfig,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["bot-configs"] }),
  });

  const testMutation = useMutation({
    mutationFn: sendTestBotMessage,
  });

  const handleAdd = (e: React.FormEvent) => {
    e.preventDefault();
    if (!webhookUrl) return;
    addMutation.mutate({ platform, webhook_url: webhookUrl, label });
  };

  const handleSendSummary = async () => {
    try {
      await sendKPISummary(summaryMonth);
      setSummaryMsg("Summary sent successfully!");
    } catch {
      setSummaryMsg("Failed to send summary.");
    }
    setTimeout(() => setSummaryMsg(""), 3000);
  };

  const configs: BotConfig[] = data?.configs ?? [];

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">Bot Notifications</h1>

      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Add Bot Webhook</h2>
        <form onSubmit={handleAdd} className="space-y-3">
          <div className="flex gap-3">
            <select
              value={platform}
              onChange={(e) => setPlatform(e.target.value)}
              className="border rounded px-3 py-2"
            >
              <option value="feishu">飞书 (Feishu)</option>
              <option value="slack">Slack</option>
            </select>
            <input
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="Label (optional)"
              className="border rounded px-3 py-2 w-48"
            />
          </div>
          <input
            type="url"
            value={webhookUrl}
            onChange={(e) => setWebhookUrl(e.target.value)}
            placeholder="Webhook URL"
            className="border rounded px-3 py-2 w-full"
            required
          />
          {addError && <p className="text-red-500 text-sm">{addError}</p>}
          <button
            type="submit"
            disabled={addMutation.isPending}
            className="bg-blue-600 text-white px-4 py-2 rounded hover:bg-blue-700 disabled:opacity-50"
          >
            {addMutation.isPending ? "Adding..." : "Add Webhook"}
          </button>
        </form>
      </div>

      <div className="bg-white rounded-lg border p-4">
        <h2 className="font-semibold text-lg mb-3">Configured Bots</h2>
        {isLoading ? (
          <p className="text-gray-500">Loading...</p>
        ) : configs.length === 0 ? (
          <p className="text-gray-400 text-sm">No bots configured yet.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-gray-500 border-b">
                <th className="pb-2">Platform</th>
                <th className="pb-2">Label</th>
                <th className="pb-2">Enabled</th>
                <th className="pb-2">Added</th>
                <th className="pb-2">Actions</th>
              </tr>
            </thead>
            <tbody>
              {configs.map((c) => (
                <tr key={c.id} className="border-b last:border-0">
                  <td className="py-2 capitalize">{c.platform}</td>
                  <td className="py-2">{c.label || "—"}</td>
                  <td className="py-2">{c.enabled ? "Yes" : "No"}</td>
                  <td className="py-2">
                    {new Date(c.created_at).toLocaleDateString()}
                  </td>
                  <td className="py-2 space-x-2">
                    <button
                      onClick={() => testMutation.mutate(c.id)}
                      disabled={testMutation.isPending}
                      className="text-blue-600 hover:underline text-xs"
                    >
                      Test
                    </button>
                    <button
                      onClick={() => deleteMutation.mutate(c.id)}
                      disabled={deleteMutation.isPending}
                      className="text-red-500 hover:underline text-xs"
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Send KPI Summary</h2>
        <div className="flex items-center gap-3">
          <input
            type="month"
            value={summaryMonth}
            onChange={(e) => setSummaryMonth(e.target.value)}
            className="border rounded px-3 py-2"
          />
          <button
            onClick={handleSendSummary}
            className="bg-green-600 text-white px-4 py-2 rounded hover:bg-green-700"
          >
            Send Summary to All Bots
          </button>
        </div>
        {summaryMsg && <p className="text-sm text-gray-600">{summaryMsg}</p>}
      </div>
    </div>
  );
}
