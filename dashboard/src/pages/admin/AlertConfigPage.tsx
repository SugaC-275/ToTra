import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listAlertConfigs,
  createAlertConfig,
  deleteAlertConfig,
  sendTestAlertConfig,
} from "../../api/client";
import type { AlertDeliveryConfig } from "../../api/client";

const ALL_EVENT_TYPES = [
  "budget_exceeded",
  "budget_warning",
  "compliance_violation",
  "pii_spike",
];

const CHANNEL_LABELS: Record<string, string> = {
  slack: "Slack",
  email: "Email",
  webhook: "Webhook",
};

const CHANNEL_PLACEHOLDER: Record<string, string> = {
  slack: "https://hooks.slack.com/services/...",
  email: "alerts@example.com",
  webhook: "https://example.com/hooks/alert",
};

export default function AlertConfigPage() {
  const queryClient = useQueryClient();

  const [channel, setChannel] = useState<"slack" | "email" | "webhook">("slack");
  const [destination, setDestination] = useState("");
  const [eventTypes, setEventTypes] = useState<string[]>([]);
  const [formError, setFormError] = useState("");
  const [testStatus, setTestStatus] = useState<"idle" | "sending" | "ok" | "error">("idle");

  const { data, isLoading } = useQuery({
    queryKey: ["alert-configs"],
    queryFn: listAlertConfigs,
  });

  const createMutation = useMutation({
    mutationFn: createAlertConfig,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["alert-configs"] });
      setDestination("");
      setEventTypes([]);
      setFormError("");
    },
    onError: (err: unknown) => {
      const apiErr = err as { response?: { data?: { error?: string } } } | null;
      setFormError(apiErr?.response?.data?.error ?? "Failed to create config");
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteAlertConfig,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["alert-configs"] }),
  });

  const toggleEventType = (et: string) =>
    setEventTypes((prev) =>
      prev.includes(et) ? prev.filter((x) => x !== et) : [...prev, et]
    );

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    if (!destination || eventTypes.length === 0) {
      setFormError("Destination and at least one event type are required");
      return;
    }
    createMutation.mutate({ channel, destination, event_types: eventTypes });
  };

  const handleTest = async () => {
    setTestStatus("sending");
    try {
      await sendTestAlertConfig();
      setTestStatus("ok");
    } catch {
      setTestStatus("error");
    } finally {
      setTimeout(() => setTestStatus("idle"), 3000);
    }
  };

  const configs: AlertDeliveryConfig[] = data?.configs ?? [];

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Alert Push Delivery</h1>
        <button
          onClick={handleTest}
          disabled={testStatus === "sending"}
          className="px-4 py-2 border rounded text-sm hover:bg-gray-50 disabled:opacity-50"
        >
          {testStatus === "sending"
            ? "Sending…"
            : testStatus === "ok"
            ? "Sent!"
            : testStatus === "error"
            ? "Failed"
            : "Send Test Alert"}
        </button>
      </div>

      {/* Config list */}
      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Active Configurations</h2>
        {isLoading ? (
          <p className="text-gray-400 text-sm">Loading…</p>
        ) : configs.length === 0 ? (
          <p className="text-gray-400 text-sm">No alert delivery configurations yet.</p>
        ) : (
          <ul className="divide-y">
            {configs.map((cfg) => (
              <li key={cfg.id} className="py-3 flex items-start justify-between gap-4">
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{CHANNEL_LABELS[cfg.channel] ?? cfg.channel}</span>
                    {!cfg.enabled && (
                      <span className="text-xs bg-gray-100 text-gray-500 rounded px-2 py-0.5">disabled</span>
                    )}
                  </div>
                  <p className="text-sm text-gray-500 truncate max-w-xs">{cfg.destination}</p>
                  <div className="flex gap-1 flex-wrap">
                    {cfg.event_types.map((et) => (
                      <span key={et} className="text-xs bg-blue-100 text-blue-700 rounded px-2 py-0.5">
                        {et}
                      </span>
                    ))}
                  </div>
                </div>
                <button
                  onClick={() => deleteMutation.mutate(cfg.id)}
                  disabled={deleteMutation.isPending}
                  className="shrink-0 text-sm px-3 py-1 border border-red-300 text-red-600 rounded hover:bg-red-50 disabled:opacity-50"
                >
                  Delete
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* New config form */}
      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Add Delivery Configuration</h2>
        <form onSubmit={handleCreate} className="space-y-3">
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">Channel</label>
            <select
              value={channel}
              onChange={(e) => {
                setChannel(e.target.value as "slack" | "email" | "webhook");
                setDestination("");
              }}
              className="block w-full border rounded px-3 py-2"
            >
              <option value="slack">Slack</option>
              <option value="email">Email</option>
              <option value="webhook">Webhook</option>
            </select>
          </div>

          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">Destination</label>
            <input
              type={channel === "email" ? "email" : "url"}
              value={destination}
              onChange={(e) => setDestination(e.target.value)}
              placeholder={CHANNEL_PLACEHOLDER[channel]}
              className="block w-full border rounded px-3 py-2"
            />
          </div>

          <div className="space-y-1">
            <p className="text-sm font-medium text-gray-700">Event types to push:</p>
            <div className="flex flex-wrap gap-3">
              {ALL_EVENT_TYPES.map((et) => (
                <label key={et} className="flex items-center gap-1 text-sm cursor-pointer">
                  <input
                    type="checkbox"
                    checked={eventTypes.includes(et)}
                    onChange={() => toggleEventType(et)}
                  />
                  {et.replace(/_/g, " ")}
                </label>
              ))}
            </div>
          </div>

          {formError && <p className="text-sm text-red-600">{formError}</p>}

          <button
            type="submit"
            disabled={createMutation.isPending}
            className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
          >
            {createMutation.isPending ? "Saving…" : "Add Configuration"}
          </button>
        </form>
      </div>
    </div>
  );
}
