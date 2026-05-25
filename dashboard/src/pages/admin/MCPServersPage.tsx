import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listMCPServers,
  createMCPServer,
  updateMCPServer,
  deleteMCPServer,
  listMCPToolCalls,
} from "../../api/client";
import type { MCPServer, MCPToolCallLog } from "../../api/client";
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
} from "../../components/ui/dialog";
import { apiErrorMessage } from "../../lib/utils";

const PAGE_SIZE = 50;
const fmtTime = (iso: string) => new Date(iso).toLocaleString();

interface ServerFormState {
  name: string;
  description: string;
  url: string;
  auth_type: "none" | "bearer";
  auth_token: string;
  max_tool_calls: string;
}

const defaultForm = (): ServerFormState => ({
  name: "",
  description: "",
  url: "",
  auth_type: "none",
  auth_token: "",
  max_tool_calls: "10",
});

interface ServerCardProps {
  server: MCPServer;
  onEdit: (s: MCPServer) => void;
  onToggle: (s: MCPServer) => void;
  onDelete: (s: MCPServer) => void;
  toggling: boolean;
  deleting: boolean;
}

function ServerCard({ server, onEdit, onToggle, onDelete, toggling, deleting }: ServerCardProps) {
  return (
    <Card className="mb-3">
      <CardContent className="pt-4 pb-4">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="font-semibold">{server.name}</span>
              <Badge variant={server.enabled ? "default" : "secondary"}>
                {server.enabled ? "Enabled" : "Disabled"}
              </Badge>
            </div>
            {server.description && (
              <p className="text-sm text-zinc-400">{server.description}</p>
            )}
            <a
              href={server.url}
              target="_blank"
              rel="noopener noreferrer"
              className="block text-xs font-mono text-blue-500 hover:underline truncate max-w-sm"
            >
              {server.url}
            </a>
            <div className="flex items-center gap-2 flex-wrap pt-1">
              <Badge variant="outline">{server.auth_type}</Badge>
              <span className="text-xs text-zinc-500">
                max {server.max_tool_calls} calls
              </span>
            </div>
          </div>
          <div className="flex gap-2 shrink-0">
            <Button size="sm" variant="outline" onClick={() => onEdit(server)}>
              Edit
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={toggling}
              onClick={() => onToggle(server)}
            >
              {server.enabled ? "Disable" : "Enable"}
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={deleting}
              className="border-red-400 text-red-500 hover:bg-red-50"
              onClick={() => onDelete(server)}
            >
              Delete
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

interface ServerFormProps {
  form: ServerFormState;
  onChange: (f: ServerFormState) => void;
  nameEditable: boolean;
  isPending: boolean;
  error: string;
  onSubmit: (e: React.FormEvent) => void;
  submitLabel: string;
}

function ServerForm({
  form, onChange, nameEditable, isPending, error, onSubmit, submitLabel,
}: ServerFormProps) {
  const set = (patch: Partial<ServerFormState>) => onChange({ ...form, ...patch });

  return (
    <form onSubmit={onSubmit} className="space-y-4">
      <div className="space-y-1">
        <Label>Name {nameEditable && <span className="text-zinc-400 font-normal text-xs">(a-z, 0-9, -, _ only)</span>}</Label>
        <Input
          placeholder="weather-api"
          value={form.name}
          disabled={!nameEditable}
          onChange={(e) => set({ name: e.target.value })}
          required
        />
        {!nameEditable && (
          <p className="text-xs text-zinc-400">Name cannot be changed after creation.</p>
        )}
      </div>
      <div className="space-y-1">
        <Label>Description <span className="text-zinc-400 font-normal text-xs">(optional)</span></Label>
        <Input
          placeholder="Provides weather data via MCP"
          value={form.description}
          onChange={(e) => set({ description: e.target.value })}
        />
      </div>
      <div className="space-y-1">
        <Label>URL</Label>
        <Input
          placeholder="https://weather.example.com/mcp"
          value={form.url}
          onChange={(e) => set({ url: e.target.value })}
          required
        />
      </div>
      <div className="space-y-1">
        <Label>Auth Type</Label>
        <select
          className="w-full h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100"
          value={form.auth_type}
          onChange={(e) => set({ auth_type: e.target.value as "none" | "bearer" })}
        >
          <option value="none">None</option>
          <option value="bearer">Bearer Token</option>
        </select>
      </div>
      {form.auth_type === "bearer" && (
        <div className="space-y-1">
          <Label>Auth Token</Label>
          <Input
            type="password"
            placeholder={nameEditable ? "sk-..." : "••••••••  (leave blank to keep existing)"}
            value={form.auth_token}
            onChange={(e) => set({ auth_token: e.target.value })}
          />
        </div>
      )}
      <div className="space-y-1">
        <Label>Max Tool Calls (1–100)</Label>
        <Input
          type="number"
          min={1}
          max={100}
          value={form.max_tool_calls}
          onChange={(e) => set({ max_tool_calls: e.target.value })}
          required
        />
      </div>
      {error && <p className="text-sm text-red-500">{error}</p>}
      <Button type="submit" className="w-full" disabled={isPending}>
        {isPending ? "Saving..." : submitLabel}
      </Button>
    </form>
  );
}

interface LogTableProps {
  logs: MCPToolCallLog[];
  page: number;
  total: number;
  onPageChange: (p: number) => void;
}

function LogTable({ logs, page, total, onPageChange }: LogTableProps) {
  const maxPage = Math.max(0, Math.ceil(total / PAGE_SIZE) - 1);

  if (logs.length === 0) {
    return <p className="text-sm text-zinc-400">No tool call records yet.</p>;
  }

  return (
    <div className="space-y-2">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-zinc-800 text-zinc-400">
            <th className="text-left py-2 font-medium">Server</th>
            <th className="text-left py-2 font-medium">Tool</th>
            <th className="text-left py-2 font-medium">Status</th>
            <th className="text-left py-2 font-medium">Duration</th>
            <th className="text-left py-2 font-medium">PII</th>
            <th className="text-left py-2 font-medium">Time</th>
          </tr>
        </thead>
        <tbody>
          {logs.map((row) => {
            const ok = row.status_code >= 200 && row.status_code < 300;
            return (
              <tr key={row.id} className="border-b border-zinc-800/50">
                <td className="py-2 font-mono text-xs">{row.server_name}</td>
                <td className="py-2 font-mono text-xs">{row.tool_name}</td>
                <td className="py-2">
                  <span className={ok ? "text-green-500" : "text-red-500"}>
                    {ok ? "✓" : "✗"} {row.status_code}
                  </span>
                </td>
                <td className="py-2 text-zinc-400">{row.duration_ms} ms</td>
                <td className="py-2">
                  {row.pii_detected && (
                    <span className="text-red-500 font-semibold text-xs">PII</span>
                  )}
                </td>
                <td className="py-2 text-zinc-400 text-xs">{fmtTime(row.created_at)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <div className="flex items-center gap-3 text-sm text-zinc-400 justify-end">
        <span>
          Page {page + 1} of {maxPage + 1} ({total} total)
        </span>
        <button
          disabled={page === 0}
          onClick={() => onPageChange(page - 1)}
          className="px-2 py-1 border rounded disabled:opacity-40 hover:bg-zinc-800"
        >
          Prev
        </button>
        <button
          disabled={page >= maxPage}
          onClick={() => onPageChange(page + 1)}
          className="px-2 py-1 border rounded disabled:opacity-40 hover:bg-zinc-800"
        >
          Next
        </button>
      </div>
    </div>
  );
}

export function MCPServersPage() {
  const qc = useQueryClient();

  // dialogs
  const [addOpen, setAddOpen] = useState(false);
  const [editServer, setEditServer] = useState<MCPServer | null>(null);

  // forms
  const [addForm, setAddForm] = useState<ServerFormState>(defaultForm());
  const [addError, setAddError] = useState("");
  const [editForm, setEditForm] = useState<ServerFormState>(defaultForm());
  const [editError, setEditError] = useState("");

  // log pagination
  const [logPage, setLogPage] = useState(0);

  // ---- queries ----

  const { data: serverData, isLoading: serversLoading } = useQuery({
    queryKey: ["mcp-servers"],
    queryFn: () => listMCPServers().then((r) => r.data),
  });

  const { data: logData, isLoading: logsLoading } = useQuery({
    queryKey: ["mcp-tool-calls", logPage],
    queryFn: () =>
      listMCPToolCalls(PAGE_SIZE, logPage * PAGE_SIZE).then((r) => r.data),
  });

  // ---- mutations ----

  const createMutation = useMutation({
    mutationFn: createMCPServer,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["mcp-servers"] });
      setAddOpen(false);
      setAddForm(defaultForm());
      setAddError("");
    },
    onError: (err: unknown) => {
      setAddError(apiErrorMessage(err, "Failed to create server"));
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Parameters<typeof updateMCPServer>[1] }) =>
      updateMCPServer(id, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["mcp-servers"] });
      setEditServer(null);
      setEditError("");
    },
    onError: (err: unknown) => {
      setEditError(apiErrorMessage(err, "Failed to update server"));
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteMCPServer,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mcp-servers"] }),
    onError: (err: unknown) => {
      alert(apiErrorMessage(err, "Failed to delete server"));
    },
  });

  // ---- handlers ----

  const handleAdd = (e: React.FormEvent) => {
    e.preventDefault();
    const payload: Parameters<typeof createMCPServer>[0] = {
      name: addForm.name,
      url: addForm.url,
      auth_type: addForm.auth_type,
      description: addForm.description || undefined,
      max_tool_calls: parseInt(addForm.max_tool_calls, 10),
    };
    if (addForm.auth_type === "bearer" && addForm.auth_token) {
      payload.auth_token = addForm.auth_token;
    }
    createMutation.mutate(payload);
  };

  const openEdit = (server: MCPServer) => {
    setEditForm({
      name: server.name,
      description: server.description,
      url: server.url,
      auth_type: server.auth_type,
      auth_token: "",
      max_tool_calls: String(server.max_tool_calls),
    });
    setEditError("");
    setEditServer(server);
  };

  const handleEdit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!editServer) return;
    const payload: Parameters<typeof updateMCPServer>[1] = {
      description: editForm.description || undefined,
      url: editForm.url,
      auth_type: editForm.auth_type,
      max_tool_calls: parseInt(editForm.max_tool_calls, 10),
    };
    if (editForm.auth_type === "bearer" && editForm.auth_token) {
      payload.auth_token = editForm.auth_token;
    }
    updateMutation.mutate({ id: editServer.id, data: payload });
  };

  const handleToggle = (server: MCPServer) => {
    updateMutation.mutate({
      id: server.id,
      data: { enabled: !server.enabled },
    });
  };

  const handleDelete = (server: MCPServer) => {
    if (!window.confirm(`Delete MCP server "${server.name}"? This cannot be undone.`)) return;
    deleteMutation.mutate(server.id);
  };

  // ---- render ----

  const servers: MCPServer[] = serverData?.servers ?? [];
  const logs: MCPToolCallLog[] = logData?.tool_calls ?? [];
  const logTotal = logData?.total ?? 0;

  return (
    <div className="space-y-6">
      {/* header */}
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">MCP Servers</h1>
        <Button onClick={() => { setAddForm(defaultForm()); setAddError(""); setAddOpen(true); }}>
          + Add Server
        </Button>
      </div>

      {/* server list */}
      {serversLoading ? (
        <p className="text-sm text-zinc-400">Loading...</p>
      ) : servers.length === 0 ? (
        <Card>
          <CardContent className="pt-6 pb-6 text-center text-zinc-400">
            No MCP servers configured yet.
          </CardContent>
        </Card>
      ) : (
        servers.map((s) => (
          <ServerCard
            key={s.id}
            server={s}
            onEdit={openEdit}
            onToggle={handleToggle}
            onDelete={handleDelete}
            toggling={updateMutation.isPending}
            deleting={deleteMutation.isPending}
          />
        ))
      )}

      {/* tool call log */}
      <div>
        <h2 className="text-lg font-semibold mb-3">Tool Call Audit Log</h2>
        <Card>
          <CardContent className="pt-4">
            {logsLoading ? (
              <p className="text-sm text-zinc-400">Loading...</p>
            ) : (
              <LogTable
                logs={logs}
                page={logPage}
                total={logTotal}
                onPageChange={setLogPage}
              />
            )}
          </CardContent>
        </Card>
      </div>

      {/* Add dialog */}
      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add MCP Server</DialogTitle>
          </DialogHeader>
          <ServerForm
            form={addForm}
            onChange={setAddForm}
            nameEditable={true}
            isPending={createMutation.isPending}
            error={addError}
            onSubmit={handleAdd}
            submitLabel="Add Server"
          />
        </DialogContent>
      </Dialog>

      {/* Edit dialog */}
      <Dialog open={!!editServer} onOpenChange={(open) => { if (!open) setEditServer(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit MCP Server</DialogTitle>
          </DialogHeader>
          <ServerForm
            form={editForm}
            onChange={setEditForm}
            nameEditable={false}
            isPending={updateMutation.isPending}
            error={editError}
            onSubmit={handleEdit}
            submitLabel="Save Changes"
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}
