import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../../api/client";

interface PolicyRule {
  id: number;
  name: string;
  pattern: string;
  action: "block" | "log";
  is_active: boolean;
  created_at: string;
}

interface CreateBody {
  name: string;
  pattern: string;
  action: "block" | "log";
}

interface UpdateBody {
  name: string;
  pattern: string;
  action: "block" | "log";
  is_active: boolean;
}

export default function PolicyRulesPage() {
  const qc = useQueryClient();

  const { data, isLoading } = useQuery<PolicyRule[]>({
    queryKey: ["policy-rules"],
    queryFn: () =>
      apiClient
        .get<PolicyRule[]>("/api/admin/compliance/policy-rules")
        .then((r) => r.data),
  });

  const createMutation = useMutation({
    mutationFn: (body: CreateBody) =>
      apiClient.post<PolicyRule>("/api/admin/compliance/policy-rules", body).then((r) => r.data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["policy-rules"] });
      setNewName("");
      setNewPattern("");
      setNewAction("block");
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, body }: { id: number; body: UpdateBody }) =>
      apiClient.put<PolicyRule>(`/api/admin/compliance/policy-rules/${id}`, body).then((r) => r.data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["policy-rules"] });
      setEditingId(null);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) =>
      apiClient.delete(`/api/admin/compliance/policy-rules/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["policy-rules"] });
    },
  });

  // Create form state
  const [newName, setNewName] = useState("");
  const [newPattern, setNewPattern] = useState("");
  const [newAction, setNewAction] = useState<"block" | "log">("block");
  const [createError, setCreateError] = useState("");

  // Edit state
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editName, setEditName] = useState("");
  const [editPattern, setEditPattern] = useState("");
  const [editAction, setEditAction] = useState<"block" | "log">("block");
  const [editActive, setEditActive] = useState(true);

  function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreateError("");
    if (!newName.trim() || !newPattern.trim()) {
      setCreateError("Name and pattern are required.");
      return;
    }
    createMutation.mutate({ name: newName.trim(), pattern: newPattern.trim(), action: newAction });
  }

  function startEdit(rule: PolicyRule) {
    setEditingId(rule.id);
    setEditName(rule.name);
    setEditPattern(rule.pattern);
    setEditAction(rule.action);
    setEditActive(rule.is_active);
  }

  function handleUpdate(id: number) {
    updateMutation.mutate({
      id,
      body: { name: editName, pattern: editPattern, action: editAction, is_active: editActive },
    });
  }

  if (isLoading) return <div className="p-8 text-gray-400">Loading policy rules…</div>;

  const rules = data ?? [];

  return (
    <div className="p-8 max-w-5xl mx-auto">
      <h1 className="text-2xl font-bold mb-1">Policy Rules</h1>
      <p className="text-gray-500 text-sm mb-6">
        Define per-tenant content policies. Each request through the gateway is checked against active rules.
      </p>

      {/* Create form */}
      <div className="mb-8 p-5 bg-zinc-900 border border-zinc-700 rounded-lg">
        <h2 className="text-base font-semibold mb-4 text-zinc-100">Add New Rule</h2>
        <form onSubmit={handleCreate} className="flex flex-col gap-3">
          <div className="flex gap-3 flex-wrap">
            <input
              type="text"
              placeholder="Rule name (e.g. no-ssn)"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              className="flex-1 min-w-[180px] px-3 py-2 rounded border border-zinc-600 bg-zinc-800 text-zinc-100 text-sm placeholder-zinc-500 focus:outline-none focus:border-indigo-500"
            />
            <input
              type="text"
              placeholder="Regex pattern (e.g. \b\d{3}-\d{2}-\d{4}\b)"
              value={newPattern}
              onChange={(e) => setNewPattern(e.target.value)}
              className="flex-[2] min-w-[240px] px-3 py-2 rounded border border-zinc-600 bg-zinc-800 text-zinc-100 text-sm placeholder-zinc-500 font-mono focus:outline-none focus:border-indigo-500"
            />
            <select
              value={newAction}
              onChange={(e) => setNewAction(e.target.value as "block" | "log")}
              className="px-3 py-2 rounded border border-zinc-600 bg-zinc-800 text-zinc-100 text-sm focus:outline-none focus:border-indigo-500"
            >
              <option value="block">Block</option>
              <option value="log">Log</option>
            </select>
            <button
              type="submit"
              disabled={createMutation.isPending}
              className="px-4 py-2 bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors"
            >
              {createMutation.isPending ? "Adding…" : "Add Rule"}
            </button>
          </div>
          {createError && <p className="text-red-400 text-sm">{createError}</p>}
          {createMutation.isError && (
            <p className="text-red-400 text-sm">
              Error: {(createMutation.error as Error)?.message ?? "Failed to create rule"}
            </p>
          )}
        </form>
      </div>

      {/* Rules table */}
      {rules.length === 0 ? (
        <div className="text-center py-16 text-zinc-500">
          <p className="text-lg mb-2">No policy rules yet</p>
          <p className="text-sm">Add a rule above to start filtering tenant requests.</p>
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-zinc-700">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-zinc-800 text-zinc-400 text-left">
                <th className="px-4 py-3 font-medium">Name</th>
                <th className="px-4 py-3 font-medium">Pattern</th>
                <th className="px-4 py-3 font-medium">Action</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium">Created</th>
                <th className="px-4 py-3 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-700">
              {rules.map((rule) =>
                editingId === rule.id ? (
                  <tr key={rule.id} className="bg-zinc-800/60">
                    <td className="px-4 py-2">
                      <input
                        value={editName}
                        onChange={(e) => setEditName(e.target.value)}
                        className="w-full px-2 py-1 rounded border border-zinc-600 bg-zinc-900 text-zinc-100 text-sm font-mono focus:outline-none focus:border-indigo-500"
                      />
                    </td>
                    <td className="px-4 py-2">
                      <input
                        value={editPattern}
                        onChange={(e) => setEditPattern(e.target.value)}
                        className="w-full px-2 py-1 rounded border border-zinc-600 bg-zinc-900 text-zinc-100 text-sm font-mono focus:outline-none focus:border-indigo-500"
                      />
                    </td>
                    <td className="px-4 py-2">
                      <select
                        value={editAction}
                        onChange={(e) => setEditAction(e.target.value as "block" | "log")}
                        className="px-2 py-1 rounded border border-zinc-600 bg-zinc-900 text-zinc-100 text-sm focus:outline-none focus:border-indigo-500"
                      >
                        <option value="block">Block</option>
                        <option value="log">Log</option>
                      </select>
                    </td>
                    <td className="px-4 py-2">
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={editActive}
                          onChange={(e) => setEditActive(e.target.checked)}
                          className="w-4 h-4 accent-indigo-600"
                        />
                        <span className="text-zinc-300 text-xs">{editActive ? "Active" : "Inactive"}</span>
                      </label>
                    </td>
                    <td className="px-4 py-2 text-zinc-500 text-xs">
                      {new Date(rule.created_at).toLocaleDateString()}
                    </td>
                    <td className="px-4 py-2 text-right">
                      <div className="flex justify-end gap-2">
                        <button
                          onClick={() => handleUpdate(rule.id)}
                          disabled={updateMutation.isPending}
                          className="px-3 py-1 bg-green-700 hover:bg-green-600 disabled:opacity-50 text-white text-xs font-medium rounded transition-colors"
                        >
                          {updateMutation.isPending ? "Saving…" : "Save"}
                        </button>
                        <button
                          onClick={() => setEditingId(null)}
                          className="px-3 py-1 bg-zinc-600 hover:bg-zinc-500 text-white text-xs font-medium rounded transition-colors"
                        >
                          Cancel
                        </button>
                      </div>
                    </td>
                  </tr>
                ) : (
                  <tr key={rule.id} className="bg-zinc-900 hover:bg-zinc-800/50 transition-colors">
                    <td className="px-4 py-3 text-zinc-100 font-medium">{rule.name}</td>
                    <td className="px-4 py-3 font-mono text-zinc-300 text-xs break-all">{rule.pattern}</td>
                    <td className="px-4 py-3">
                      {rule.action === "block" ? (
                        <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold bg-red-900/60 text-red-300 border border-red-700">
                          Block
                        </span>
                      ) : (
                        <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold bg-yellow-900/60 text-yellow-300 border border-yellow-700">
                          Log
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      {rule.is_active ? (
                        <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold bg-green-900/60 text-green-300 border border-green-700">
                          Active
                        </span>
                      ) : (
                        <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold bg-zinc-700 text-zinc-400 border border-zinc-600">
                          Inactive
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-zinc-500 text-xs">
                      {new Date(rule.created_at).toLocaleDateString()}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex justify-end gap-2">
                        <button
                          onClick={() => startEdit(rule)}
                          className="px-3 py-1 bg-zinc-700 hover:bg-zinc-600 text-zinc-100 text-xs font-medium rounded transition-colors"
                        >
                          Edit
                        </button>
                        <button
                          onClick={() => deleteMutation.mutate(rule.id)}
                          disabled={deleteMutation.isPending}
                          className="px-3 py-1 bg-red-800 hover:bg-red-700 disabled:opacity-50 text-white text-xs font-medium rounded transition-colors"
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
