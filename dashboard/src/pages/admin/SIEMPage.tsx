import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listSIEMConfigs,
  createSIEMConfig,
  deleteSIEMConfig,
  sendSIEMTest,
  getSIEMDeliveryLog,
} from "../../api/client";
import type { SIEMConfig, DeliveryLogRow } from "../../api/client";

const ALL_EVENT_TYPES = [
  "pii_violation",
  "policy_block",
  "audit_log",
  "quota_exceeded",
  "routing_event",
];

const STATUS_COLORS: Record<string, string> = {
  delivered: "text-green-600",
  pending: "text-yellow-600",
  failed: "text-red-600",
};

export default function SIEMPage() {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [endpointURL, setEndpointURL] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [eventTypes, setEventTypes] = useState<string[]>([]);
  const [formError, setFormError] = useState("");

  const { data: cfgData, isLoading } = useQuery({
    queryKey: ["siem-configs"],
    queryFn: listSIEMConfigs,
  });

  const { data: logData } = useQuery({
    queryKey: ["siem-delivery-log"],
    queryFn: getSIEMDeliveryLog,
    refetchInterval: 30_000,
  });

  const createMutation = useMutation({
    mutationFn: createSIEMConfig,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["siem-configs"] });
      setName(""); setEndpointURL(""); setApiKey(""); setEventTypes([]); setFormError("");
    },
    onError: (err: unknown) => {
      const apiErr = err as { response?: { data?: { error?: string } } } | null;
      setFormError(apiErr?.response?.data?.error ?? "Failed to create config");
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteSIEMConfig,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["siem-configs"] }),
  });

  const testMutation = useMutation({ mutationFn: sendSIEMTest });

  const toggleEventType = (et: string) =>
    setEventTypes((prev) =>
      prev.includes(et) ? prev.filter((x) => x !== et) : [...prev, et]
    );

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name || !endpointURL || !apiKey || eventTypes.length === 0) {
      setFormError("All fields are required");
      return;
    }
    createMutation.mutate({ name, endpoint_url: endpointURL, api_key: apiKey, event_types: eventTypes });
  };

  const configs: SIEMConfig[] = cfgData?.configs ?? [];
  const deliveryLog: DeliveryLogRow[] = logData?.log ?? [];

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">SIEM Integration</h1>

      {/* Config list */}
      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Active Configurations</h2>
        {isLoading ? (
          <p className="text-gray-400 text-sm">Loading…</p>
        ) : configs.length === 0 ? (
          <p className="text-gray-400 text-sm">No SIEM configurations yet.</p>
        ) : (
          <ul className="divide-y">
            {configs.map((cfg) => (
              <li key={cfg.id} className="py-3 flex items-start justify-between gap-4">
                <div className="space-y-1">
                  <p className="font-medium">{cfg.name}</p>
                  <p className="text-sm text-gray-500 truncate max-w-xs">{cfg.endpoint_url}</p>
                  <div className="flex gap-1 flex-wrap">
                    {cfg.event_types.map((et) => (
                      <span key={et} className="text-xs bg-blue-100 text-blue-700 rounded px-2 py-0.5">{et}</span>
                    ))}
                  </div>
                </div>
                <div className="flex gap-2 shrink-0">
                  <button
                    onClick={() => testMutation.mutate(cfg.id)}
                    disabled={testMutation.isPending}
                    className="text-sm px-3 py-1 border rounded hover:bg-gray-50"
                  >
                    Test
                  </button>
                  <button
                    onClick={() => deleteMutation.mutate(cfg.id)}
                    className="text-sm px-3 py-1 border border-red-300 text-red-600 rounded hover:bg-red-50"
                  >
                    Delete
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* New config form */}
      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Add SIEM Configuration</h2>
        <form onSubmit={handleCreate} className="space-y-3">
          <input
            type="text" value={name} onChange={(e) => setName(e.target.value)}
            placeholder="Name (e.g. Splunk HEC)"
            className="block w-full border rounded px-3 py-2"
          />
          <input
            type="url" value={endpointURL} onChange={(e) => setEndpointURL(e.target.value)}
            placeholder="Endpoint URL"
            className="block w-full border rounded px-3 py-2"
          />
          <input
            type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)}
            placeholder="API Key"
            className="block w-full border rounded px-3 py-2"
          />
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
                  {et}
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

      {/* Delivery log */}
      <div className="bg-white rounded-lg border p-4 space-y-3">
        <h2 className="font-semibold text-lg">Push Delivery Log</h2>
        {deliveryLog.length === 0 ? (
          <p className="text-gray-400 text-sm">No delivery records yet.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-gray-500 border-b">
                <th className="pb-2">Event Type</th>
                <th className="pb-2">Status</th>
                <th className="pb-2">Retries</th>
                <th className="pb-2">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {deliveryLog.map((row) => (
                <tr key={row.id}>
                  <td className="py-2">{row.event_type}</td>
                  <td className={`py-2 font-medium ${STATUS_COLORS[row.status] ?? ""}`}>{row.status}</td>
                  <td className="py-2">{row.attempts}</td>
                  <td className="py-2 text-gray-500">{new Date(row.created_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
