import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listUsers, createUser } from "../../api/client";
import { Card, CardContent } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "../../components/ui/dialog";
import { Alert, AlertDescription } from "../../components/ui/alert";

export function UsersPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [newKey, setNewKey] = useState<string | null>(null);
  const [form, setForm] = useState({ name: "", email: "", role: "standard" });

  const { data } = useQuery({
    queryKey: ["users"],
    queryFn: () => listUsers().then((r) => r.data),
  });

  const createMutation = useMutation({
    mutationFn: createUser,
    onSuccess: (res) => {
      setNewKey(res.data.api_key);
      qc.invalidateQueries({ queryKey: ["users"] });
    },
  });

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    createMutation.mutate(form);
  };

  const handleOpen = () => {
    setNewKey(null);
    setForm({ name: "", email: "", role: "standard" });
    setOpen(true);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Employees</h1>
        <Dialog open={open} onOpenChange={(v) => { setOpen(v); if (!v) setNewKey(null); }}>
          <DialogTrigger asChild>
            <Button onClick={handleOpen}>+ Add Employee</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Add Employee</DialogTitle>
            </DialogHeader>
            {newKey ? (
              <div className="space-y-3">
                <Alert>
                  <AlertDescription>
                    <p className="font-medium mb-2">Employee Key (save this — shown only once):</p>
                    <code className="text-xs bg-zinc-800 p-2 rounded block break-all">{newKey}</code>
                  </AlertDescription>
                </Alert>
                <Button className="w-full" onClick={() => { setOpen(false); setNewKey(null); }}>
                  Done
                </Button>
              </div>
            ) : (
              <form onSubmit={handleCreate} className="space-y-4">
                <div className="space-y-1">
                  <Label>Name</Label>
                  <Input
                    value={form.name}
                    onChange={(e) => setForm({ ...form, name: e.target.value })}
                    required
                  />
                </div>
                <div className="space-y-1">
                  <Label>Email</Label>
                  <Input
                    type="email"
                    value={form.email}
                    onChange={(e) => setForm({ ...form, email: e.target.value })}
                    required
                  />
                </div>
                <div className="space-y-1">
                  <Label>Role</Label>
                  <select
                    className="w-full h-10 rounded-md border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-zinc-100"
                    value={form.role}
                    onChange={(e) => setForm({ ...form, role: e.target.value })}
                  >
                    <option value="standard">Standard</option>
                    <option value="senior">Senior</option>
                    <option value="researcher">Researcher</option>
                    <option value="admin">Admin</option>
                  </select>
                </div>
                <Button type="submit" className="w-full" disabled={createMutation.isPending}>
                  {createMutation.isPending ? "Creating..." : "Create Employee"}
                </Button>
              </form>
            )}
          </DialogContent>
        </Dialog>
      </div>

      <Card>
        <CardContent className="pt-4">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-zinc-800 text-zinc-400">
                <th className="text-left py-2 font-medium">Name</th>
                <th className="text-left py-2 font-medium">Email</th>
                <th className="text-left py-2 font-medium">Role</th>
                <th className="text-right py-2 font-medium">Quota (SCU)</th>
                <th className="text-right py-2 font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {data?.users?.map((u) => (
                <tr key={u.id} className="border-b border-zinc-800/50">
                  <td className="py-2">{u.name}</td>
                  <td className="text-zinc-400">{u.email}</td>
                  <td>
                    <Badge variant="outline">{u.role}</Badge>
                  </td>
                  <td className="text-right">{u.quota_scu?.toLocaleString()}</td>
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
