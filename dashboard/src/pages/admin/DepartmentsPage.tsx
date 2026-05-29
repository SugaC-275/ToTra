import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listDepartments,
  createDepartment,
  getDepartment,
  setDepartmentBudget,
  deleteDepartment,
  listDepartmentUsers,
  assignUserToDepartment,
  listUsers,
  type Department,
  type DeptUser,
} from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";
import { Alert, AlertDescription } from "../../components/ui/alert";

// ---- Budget ring (SVG donut) ----

function BudgetRing({ spent, budget }: { spent: number; budget: number | null }) {
  if (budget === null || budget <= 0) {
    return <span className="text-zinc-500 text-xs">No limit</span>;
  }
  const pct = Math.min(spent / budget, 1);
  const r = 20;
  const circ = 2 * Math.PI * r;
  const dash = pct * circ;
  const color = pct >= 1 ? "#ef4444" : pct >= 0.8 ? "#f59e0b" : "#6366f1";

  return (
    <div className="flex items-center gap-2">
      <svg width="50" height="50" viewBox="0 0 50 50">
        <circle cx="25" cy="25" r={r} fill="none" stroke="#3f3f46" strokeWidth="6" />
        <circle
          cx="25"
          cy="25"
          r={r}
          fill="none"
          stroke={color}
          strokeWidth="6"
          strokeDasharray={`${dash} ${circ}`}
          strokeLinecap="round"
          transform="rotate(-90 25 25)"
        />
        <text x="25" y="29" textAnchor="middle" fontSize="9" fill="#e4e4e7">
          {Math.round(pct * 100)}%
        </text>
      </svg>
      <span className="text-xs text-zinc-400">
        ${spent.toFixed(2)} / ${budget.toFixed(2)}
      </span>
    </div>
  );
}

// ---- Create department modal ----

function CreateDeptModal({ onCreated }: { onCreated: () => void }) {
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: "", slug: "" });
  const [error, setError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: createDepartment,
    onSuccess: () => {
      onCreated();
      setOpen(false);
      setForm({ name: "", slug: "" });
      setError(null);
    },
    onError: () => setError("Failed to create department."),
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!form.name || !form.slug) return;
    mutation.mutate(form);
  };

  const handleNameChange = (name: string) => {
    const slug = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, "");
    setForm({ name, slug });
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>+ New Department</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create Department</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          {error && (
            <Alert>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <div className="space-y-1">
            <Label>Name</Label>
            <Input
              value={form.name}
              onChange={(e) => handleNameChange(e.target.value)}
              placeholder="Engineering"
              required
            />
          </div>
          <div className="space-y-1">
            <Label>Slug</Label>
            <Input
              value={form.slug}
              onChange={(e) => setForm({ ...form, slug: e.target.value })}
              placeholder="engineering"
              pattern="[a-z0-9-]+"
              required
            />
            <p className="text-xs text-zinc-500">URL-safe identifier, lowercase letters/numbers/hyphens</p>
          </div>
          <Button type="submit" className="w-full" disabled={mutation.isPending}>
            {mutation.isPending ? "Creating..." : "Create"}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---- Budget settings modal ----

function BudgetModal({ dept, onSaved }: { dept: Department; onSaved: () => void }) {
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({
    budget_usd: dept.budget_usd?.toString() ?? "",
    rpm_limit: dept.rpm_limit?.toString() ?? "",
    tpm_limit: dept.tpm_limit?.toString() ?? "",
  });

  const mutation = useMutation({
    mutationFn: (data: { budget_usd: number | null; rpm_limit: number | null; tpm_limit: number | null }) =>
      setDepartmentBudget(dept.id, data),
    onSuccess: () => {
      onSaved();
      setOpen(false);
    },
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    mutation.mutate({
      budget_usd: form.budget_usd ? parseFloat(form.budget_usd) : null,
      rpm_limit: form.rpm_limit ? parseInt(form.rpm_limit) : null,
      tpm_limit: form.tpm_limit ? parseInt(form.tpm_limit) : null,
    });
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">Set Budget</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Budget — {dept.name}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1">
            <Label>Monthly Budget (USD)</Label>
            <Input
              type="number"
              min="0"
              step="0.01"
              value={form.budget_usd}
              onChange={(e) => setForm({ ...form, budget_usd: e.target.value })}
              placeholder="e.g. 500.00 (leave blank for no limit)"
            />
          </div>
          <div className="space-y-1">
            <Label>RPM Limit</Label>
            <Input
              type="number"
              min="0"
              value={form.rpm_limit}
              onChange={(e) => setForm({ ...form, rpm_limit: e.target.value })}
              placeholder="Requests per minute (optional)"
            />
          </div>
          <div className="space-y-1">
            <Label>TPM Limit</Label>
            <Input
              type="number"
              min="0"
              value={form.tpm_limit}
              onChange={(e) => setForm({ ...form, tpm_limit: e.target.value })}
              placeholder="Tokens per minute (optional)"
            />
          </div>
          <Button type="submit" className="w-full" disabled={mutation.isPending}>
            {mutation.isPending ? "Saving..." : "Save"}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---- Department detail view ----

function DepartmentDetail({
  dept,
  onBack,
  onRefresh,
}: {
  dept: Department;
  onBack: () => void;
  onRefresh: () => void;
}) {
  const qc = useQueryClient();

  const { data: deptData } = useQuery({
    queryKey: ["department", dept.id],
    queryFn: () => getDepartment(dept.id).then((r) => r.data),
  });

  const { data: usersData } = useQuery({
    queryKey: ["department-users", dept.id],
    queryFn: () => listDepartmentUsers(dept.id).then((r) => r.data),
  });

  const { data: allUsersData } = useQuery({
    queryKey: ["users"],
    queryFn: () => listUsers().then((r) => r.data),
  });

  const assignMutation = useMutation({
    mutationFn: ({ userId }: { userId: string }) => assignUserToDepartment(dept.id, userId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["department-users", dept.id] }),
  });

  const [selectedUser, setSelectedUser] = useState("");

  const current = deptData ?? dept;
  const deptUserIds = new Set((usersData?.users ?? []).map((u: DeptUser) => u.id));
  const unassigned = (allUsersData?.users ?? []).filter((u) => !deptUserIds.has(u.id));

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <button
          onClick={onBack}
          className="text-zinc-400 hover:text-zinc-100 text-sm"
        >
          ← Back
        </button>
        <h1 className="text-2xl font-bold">{current.name}</h1>
        <Badge variant="outline" className="font-mono text-xs">{current.slug}</Badge>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <Card>
          <CardContent className="pt-4 space-y-3">
            <h3 className="font-semibold text-sm text-zinc-300">Monthly Spend</h3>
            <BudgetRing spent={current.spend_usd} budget={current.budget_usd} />
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 space-y-3">
            <h3 className="font-semibold text-sm text-zinc-300">Limits</h3>
            <div className="text-sm space-y-1 text-zinc-400">
              <div>RPM: {current.rpm_limit ?? "—"}</div>
              <div>TPM: {current.tpm_limit ?? "—"}</div>
            </div>
            <BudgetModal
              dept={current}
              onSaved={() => {
                qc.invalidateQueries({ queryKey: ["department", dept.id] });
                onRefresh();
              }}
            />
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardContent className="pt-4 space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="font-semibold text-sm text-zinc-300">Assigned Users</h3>
            <div className="flex gap-2">
              <select
                className="h-9 rounded-md border border-zinc-700 bg-zinc-800 px-3 text-sm text-zinc-100"
                value={selectedUser}
                onChange={(e) => setSelectedUser(e.target.value)}
              >
                <option value="">Assign user...</option>
                {unassigned.map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.name} ({u.email})
                  </option>
                ))}
              </select>
              <Button
                size="sm"
                disabled={!selectedUser || assignMutation.isPending}
                onClick={() => {
                  if (selectedUser) {
                    assignMutation.mutate({ userId: selectedUser });
                    setSelectedUser("");
                  }
                }}
              >
                Assign
              </Button>
            </div>
          </div>
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-zinc-800 text-zinc-400">
                <th className="text-left py-2 font-medium">Name</th>
                <th className="text-left py-2 font-medium">Email</th>
                <th className="text-left py-2 font-medium">Role</th>
                <th className="text-right py-2 font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {(usersData?.users ?? []).map((u: DeptUser) => (
                <tr key={u.id} className="border-b border-zinc-800/50">
                  <td className="py-2">{u.name}</td>
                  <td className="text-zinc-400">{u.email}</td>
                  <td><Badge variant="outline">{u.role}</Badge></td>
                  <td className="text-right">
                    <Badge variant={u.is_active ? "default" : "secondary"}>
                      {u.is_active ? "Active" : "Inactive"}
                    </Badge>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  );
}

// ---- Main page ----

export function DepartmentsPage() {
  const qc = useQueryClient();
  const [selected, setSelected] = useState<Department | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ["departments"],
    queryFn: () => listDepartments().then((r) => r.data),
  });

  const deleteMutation = useMutation({
    mutationFn: deleteDepartment,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["departments"] }),
  });

  const refresh = () => qc.invalidateQueries({ queryKey: ["departments"] });

  if (selected) {
    return (
      <DepartmentDetail
        dept={selected}
        onBack={() => setSelected(null)}
        onRefresh={refresh}
      />
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Departments</h1>
        <CreateDeptModal onCreated={refresh} />
      </div>

      <Card>
        <CardContent className="pt-4">
          {isLoading ? (
            <p className="text-zinc-500 text-sm">Loading...</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 font-medium">Name</th>
                  <th className="text-left py-2 font-medium">Slug</th>
                  <th className="text-right py-2 font-medium">Budget (USD/mo)</th>
                  <th className="text-right py-2 font-medium">Spend this month</th>
                  <th className="text-right py-2 font-medium">Status</th>
                  <th className="text-right py-2 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {(data?.departments ?? []).map((dept) => (
                  <tr key={dept.id} className="border-b border-zinc-800/50">
                    <td className="py-2">
                      <button
                        className="text-indigo-400 hover:text-indigo-300 font-medium"
                        onClick={() => setSelected(dept)}
                      >
                        {dept.name}
                      </button>
                    </td>
                    <td className="font-mono text-xs text-zinc-400">{dept.slug}</td>
                    <td className="text-right">
                      {dept.budget_usd !== null ? `$${dept.budget_usd.toFixed(2)}` : "—"}
                    </td>
                    <td className="text-right">${dept.spend_usd.toFixed(2)}</td>
                    <td className="text-right">
                      <Badge variant={dept.is_active ? "default" : "secondary"}>
                        {dept.is_active ? "Active" : "Inactive"}
                      </Badge>
                    </td>
                    <td className="text-right">
                      <button
                        className="text-xs text-red-400 hover:text-red-300"
                        onClick={() => {
                          if (confirm(`Delete department "${dept.name}"?`)) {
                            deleteMutation.mutate(dept.id);
                          }
                        }}
                      >
                        Delete
                      </button>
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
