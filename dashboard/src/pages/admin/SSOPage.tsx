import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../../api/client";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Badge } from "../../components/ui/badge";

interface SSOConfig {
  tenant_id: string;
  issuer: string;
  client_id: string;
  client_secret: string;
  redirect_uri: string;
  enabled: boolean;
}

const getSSOConfig = () =>
  apiClient.get<SSOConfig>("/api/admin/sso/config").then((r) => r.data);

const putSSOConfig = (data: Omit<SSOConfig, "tenant_id">) =>
  apiClient.put("/api/admin/sso/config", data).then((r) => r.data);

const testSSOConnection = () =>
  apiClient.post<{ status: string; issuer: string }>("/api/admin/sso/test").then((r) => r.data);

const emptyForm: Omit<SSOConfig, "tenant_id"> = {
  issuer: "",
  client_id: "",
  client_secret: "",
  redirect_uri: "",
  enabled: false,
};

export function SSOPage() {
  const qc = useQueryClient();
  const [form, setForm] = useState<Omit<SSOConfig, "tenant_id">>(emptyForm);
  const [editMode, setEditMode] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string } | null>(null);

  const { data: cfg, isLoading, error } = useQuery({
    queryKey: ["sso-config"],
    queryFn: getSSOConfig,
    retry: false,
  });

  const saveMutation = useMutation({
    mutationFn: putSSOConfig,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["sso-config"] });
      setEditMode(false);
      setTestResult(null);
    },
  });

  const testMutation = useMutation({
    mutationFn: testSSOConnection,
    onSuccess: (data) => setTestResult({ ok: true, message: `Connected to ${data.issuer}` }),
    onError: (err: unknown) => {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        "Connection test failed";
      setTestResult({ ok: false, message: msg });
    },
  });

  function handleEdit() {
    if (cfg) {
      setForm({
        issuer: cfg.issuer,
        client_id: cfg.client_id,
        client_secret: "",
        redirect_uri: cfg.redirect_uri,
        enabled: cfg.enabled,
      });
    } else {
      setForm(emptyForm);
    }
    setTestResult(null);
    setEditMode(true);
  }

  function handleSave(e: React.FormEvent) {
    e.preventDefault();
    saveMutation.mutate(form);
  }

  function handleTest() {
    setTestResult(null);
    testMutation.mutate();
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Single Sign-On (SSO)</h1>
        {!editMode && (
          <Button onClick={handleEdit}>
            {cfg ? "Edit Configuration" : "Configure SSO"}
          </Button>
        )}
      </div>

      {/* Current config card */}
      {!editMode && (
        <Card>
          <CardHeader>
            <CardTitle>Current Configuration</CardTitle>
          </CardHeader>
          <CardContent>
            {isLoading && <p className="text-zinc-400 text-sm">Loading...</p>}
            {error && !isLoading && (
              <p className="text-zinc-500 text-sm py-4 text-center">
                No SSO configuration found. Click "Configure SSO" to set one up.
              </p>
            )}
            {cfg && (
              <dl className="space-y-3 text-sm">
                <div className="flex justify-between">
                  <dt className="text-zinc-400">Status</dt>
                  <dd>
                    <Badge variant={cfg.enabled ? "default" : "secondary"}>
                      {cfg.enabled ? "Enabled" : "Disabled"}
                    </Badge>
                  </dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-zinc-400">Issuer URL</dt>
                  <dd className="text-zinc-100 truncate max-w-xs">{cfg.issuer}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-zinc-400">Client ID</dt>
                  <dd className="text-zinc-100 font-mono text-xs">{cfg.client_id}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-zinc-400">Client Secret</dt>
                  <dd className="text-zinc-400 font-mono text-xs">{cfg.client_secret}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-zinc-400">Redirect URI</dt>
                  <dd className="text-zinc-100 text-xs truncate max-w-xs">{cfg.redirect_uri}</dd>
                </div>
              </dl>
            )}
            {cfg && (
              <div className="mt-4">
                <Button variant="outline" onClick={handleTest} disabled={testMutation.isPending}>
                  {testMutation.isPending ? "Testing..." : "Test Connection"}
                </Button>
                {testResult && (
                  <p className={`mt-2 text-sm ${testResult.ok ? "text-green-400" : "text-red-400"}`}>
                    {testResult.message}
                  </p>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Edit / create form */}
      {editMode && (
        <Card>
          <CardHeader>
            <CardTitle>{cfg ? "Update SSO Configuration" : "Configure SSO"}</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSave} className="space-y-4">
              <div className="space-y-1">
                <Label>Issuer URL</Label>
                <Input
                  placeholder="https://login.microsoftonline.com/{tenant}/v2.0"
                  value={form.issuer}
                  onChange={(e) => setForm({ ...form, issuer: e.target.value })}
                  required
                />
                <p className="text-xs text-zinc-500">
                  The OIDC issuer — e.g. Okta, Azure AD, Google Workspace.
                </p>
              </div>

              <div className="space-y-1">
                <Label>Client ID</Label>
                <Input
                  placeholder="Application (client) ID"
                  value={form.client_id}
                  onChange={(e) => setForm({ ...form, client_id: e.target.value })}
                  required
                />
              </div>

              <div className="space-y-1">
                <Label>Client Secret</Label>
                <Input
                  type="password"
                  placeholder={cfg ? "Leave blank to keep existing secret" : "Client secret"}
                  value={form.client_secret}
                  onChange={(e) => setForm({ ...form, client_secret: e.target.value })}
                  required={!cfg}
                />
              </div>

              <div className="space-y-1">
                <Label>Redirect URI</Label>
                <Input
                  placeholder="https://your-domain/api/auth/oidc/callback"
                  value={form.redirect_uri}
                  onChange={(e) => setForm({ ...form, redirect_uri: e.target.value })}
                  required
                />
                <p className="text-xs text-zinc-500">
                  Register this URI in your identity provider's app settings.
                </p>
              </div>

              <div className="flex items-center gap-3">
                <input
                  id="sso-enabled"
                  type="checkbox"
                  className="h-4 w-4 rounded border-zinc-700 bg-zinc-800 accent-indigo-500"
                  checked={form.enabled}
                  onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
                />
                <Label htmlFor="sso-enabled">Enable SSO for this tenant</Label>
              </div>

              {saveMutation.isError && (
                <p className="text-red-400 text-sm">
                  {(saveMutation.error as { response?: { data?: { error?: string } } })?.response
                    ?.data?.error ?? "Failed to save configuration"}
                </p>
              )}

              <div className="flex gap-2">
                <Button type="submit" disabled={saveMutation.isPending}>
                  {saveMutation.isPending ? "Saving..." : "Save Configuration"}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => { setEditMode(false); setTestResult(null); }}
                >
                  Cancel
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      )}

      {/* Provider guide */}
      <Card>
        <CardHeader>
          <CardTitle>Setup Guide</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-zinc-400 space-y-2">
          <p>Register an OIDC application in your identity provider and configure:</p>
          <ul className="list-disc list-inside space-y-1">
            <li>
              <span className="text-zinc-200">Okta</span>: Applications &rarr; Create App Integration &rarr; OIDC
            </li>
            <li>
              <span className="text-zinc-200">Azure AD</span>: App registrations &rarr; New registration
            </li>
            <li>
              <span className="text-zinc-200">Google Workspace</span>: APIs &amp; Services &rarr; Credentials &rarr; OAuth 2.0 Client
            </li>
          </ul>
          <p className="pt-1">
            Set the <span className="text-zinc-200">Redirect URI</span> to the value above. Scopes required:{" "}
            <code className="text-xs bg-zinc-800 px-1 rounded">openid email profile</code>.
          </p>
          <p>
            Login URL for tenants:{" "}
            <code className="text-xs bg-zinc-800 px-1 rounded">
              /api/auth/oidc/login?tenant_id=YOUR_TENANT_ID
            </code>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
