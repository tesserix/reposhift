"use client";

import { useEffect, useState } from "react";
import { api, type Secret, type SecretValidation } from "@/lib/api";
import Nav from "@/components/nav";

const SECRET_TYPES = [
  { value: "ado_pat", label: "ADO PAT", fields: ["token", "organization"] },
  { value: "github_token", label: "GitHub Token", fields: ["token", "owner"] },
  { value: "github_app", label: "GitHub App", fields: ["app_id", "installation_id", "private_key"] },
  { value: "azure_sp", label: "Azure Service Principal", fields: ["client_id", "client_secret", "tenant_id", "organization"] },
];

function getFieldsForType(type: string) {
  return SECRET_TYPES.find((t) => t.value === type)?.fields ?? ["token"];
}

function getTypeLabel(type: string) {
  return SECRET_TYPES.find((t) => t.value === type)?.label ?? type;
}

export default function SecretsPage() {
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [name, setName] = useState("");
  const [type, setType] = useState("ado_pat");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validating, setValidating] = useState<string | null>(null);
  const [validation, setValidation] = useState<SecretValidation | null>(null);

  useEffect(() => {
    if (!api.isAuthenticated()) {
      window.location.href = "/login";
      return;
    }
    loadSecrets();
  }, []);

  function loadSecrets() {
    setLoading(true);
    api
      .listSecrets()
      .then((res) => setSecrets(res.secrets ?? []))
      .catch(() => {})
      .finally(() => setLoading(false));
  }

  function handleTypeChange(newType: string) {
    setType(newType);
    setFields({});
  }

  function updateField(key: string, value: string) {
    setFields((prev) => ({ ...prev, [key]: value }));
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await api.createSecret(name, type, fields);
      setName("");
      setFields({});
      setShowForm(false);
      loadSecrets();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create secret");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleValidate(secretName: string) {
    setValidating(secretName);
    setValidation(null);
    try {
      const res = await api.validateSecret(secretName);
      setValidation(res.validation);
    } catch (err) {
      const matchedSecret = secrets.find((s) => s.name === secretName);
      setValidation({
        name: secretName,
        secretType: matchedSecret?.secretType ?? "",
        valid: false,
        checks: [{ check: "request", status: "failed", message: err instanceof Error ? err.message : "Validation failed" }],
      });
    } finally {
      setValidating(null);
    }
  }

  async function handleDelete(secretName: string) {
    if (!confirm("Delete this secret? Migrations using it will fail.")) return;
    await api.deleteSecret(secretName);
    if (validation?.name === secretName) setValidation(null);
    loadSecrets();
  }

  const currentFields = getFieldsForType(type);

  return (
    <div className="flex min-h-screen">
      <Nav />
      <main className="ml-60 flex-1 p-8">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold">Secrets</h1>
            <p className="mt-1 text-sm text-zinc-400">
              Store and validate ADO PATs, GitHub tokens, and service credentials
            </p>
          </div>
          <button
            onClick={() => { setShowForm(!showForm); setError(null); }}
            className="rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-emerald-500"
          >
            {showForm ? "Cancel" : "Add Secret"}
          </button>
        </div>

        {showForm && (
          <form
            onSubmit={handleCreate}
            className="mb-6 rounded-xl border border-zinc-800 bg-zinc-900/50 p-5"
          >
            {error && (
              <div className="mb-4 rounded-lg border border-red-800 bg-red-950/50 px-4 py-3 text-sm text-red-300">
                {error}
              </div>
            )}
            <div className="grid grid-cols-2 gap-4">
              <label className="block">
                <span className="mb-1.5 block text-xs font-medium text-zinc-400">Name</span>
                <input
                  type="text"
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="my-ado-pat"
                  className="input"
                />
              </label>
              <label className="block">
                <span className="mb-1.5 block text-xs font-medium text-zinc-400">Type</span>
                <select value={type} onChange={(e) => handleTypeChange(e.target.value)} className="input">
                  {SECRET_TYPES.map((t) => (
                    <option key={t.value} value={t.value}>{t.label}</option>
                  ))}
                </select>
              </label>
            </div>
            <div className="mt-4 grid grid-cols-2 gap-4">
              {currentFields.map((field) => (
                <label key={field} className="block">
                  <span className="mb-1.5 block text-xs font-medium text-zinc-400">
                    {field.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase())}
                    {(field === "token" || field === "client_secret" || field === "private_key") ? "" : " (optional)"}
                  </span>
                  {field === "private_key" ? (
                    <textarea
                      value={fields[field] ?? ""}
                      onChange={(e) => updateField(field, e.target.value)}
                      placeholder={`Enter ${field}...`}
                      rows={3}
                      className="input"
                      required={field === "private_key"}
                    />
                  ) : (
                    <input
                      type={field.includes("secret") || field === "token" || field === "private_key" ? "password" : "text"}
                      value={fields[field] ?? ""}
                      onChange={(e) => updateField(field, e.target.value)}
                      placeholder={`Enter ${field}...`}
                      className="input"
                      required={field === "token" || field === "client_id" || field === "client_secret" || field === "tenant_id" || field === "app_id" || field === "installation_id" || field === "private_key"}
                    />
                  )}
                </label>
              ))}
            </div>
            <div className="mt-4">
              <button
                type="submit"
                disabled={submitting}
                className="rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-emerald-500 disabled:opacity-50"
              >
                {submitting ? "Saving..." : "Save Secret"}
              </button>
            </div>
          </form>
        )}

        {/* Validation results panel */}
        {validation && (
          <div className="mb-6 rounded-xl border border-zinc-800 bg-zinc-900/50 p-5">
            <div className="mb-3 flex items-center justify-between">
              <div className="flex items-center gap-3">
                <span className={`inline-flex h-6 w-6 items-center justify-center rounded-full text-xs font-bold ${validation.valid ? "bg-emerald-950 text-emerald-400" : "bg-red-950 text-red-400"}`}>
                  {validation.valid ? "\u2713" : "\u2717"}
                </span>
                <h3 className="font-medium">
                  Validation: <span className="text-zinc-400">{validation.name}</span>
                </h3>
              </div>
              <button onClick={() => setValidation(null)} className="text-xs text-zinc-500 hover:text-zinc-300">Dismiss</button>
            </div>
            <div className="space-y-2">
              {validation.checks.map((check, i) => (
                <div key={i} className="flex items-start gap-3 rounded-lg border border-zinc-800 bg-zinc-950/50 px-4 py-2.5">
                  <span className={`mt-0.5 text-xs font-bold ${
                    check.status === "passed" ? "text-emerald-400" :
                    check.status === "failed" ? "text-red-400" :
                    check.status === "warning" ? "text-amber-400" : "text-zinc-500"
                  }`}>
                    {check.status === "passed" ? "PASS" :
                     check.status === "failed" ? "FAIL" :
                     check.status === "warning" ? "WARN" : "SKIP"}
                  </span>
                  <div>
                    <span className="text-xs font-medium text-zinc-300">{check.check}</span>
                    <p className="text-xs text-zinc-500">{check.message}</p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        <div className="rounded-xl border border-zinc-800 bg-zinc-900/50">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-left text-xs text-zinc-500">
                  <th className="px-5 py-3 font-medium">Name</th>
                  <th className="px-5 py-3 font-medium">Type</th>
                  <th className="px-5 py-3 font-medium">Created</th>
                  <th className="px-5 py-3 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {loading ? (
                  <tr>
                    <td colSpan={4} className="px-5 py-8 text-center text-zinc-500">Loading...</td>
                  </tr>
                ) : secrets.length === 0 ? (
                  <tr>
                    <td colSpan={4} className="px-5 py-8 text-center text-zinc-500">No secrets configured yet.</td>
                  </tr>
                ) : (
                  secrets.map((s) => (
                    <tr key={s.id} className="border-b border-zinc-800/50 transition hover:bg-zinc-800/30">
                      <td className="px-5 py-3 font-medium">{s.name}</td>
                      <td className="px-5 py-3">
                        <span className={`inline-flex rounded-md border px-2 py-0.5 text-xs font-medium ${
                          s.secretType === "ado_pat" ? "border-blue-800 bg-blue-950 text-blue-400" :
                          s.secretType === "github_token" ? "border-purple-800 bg-purple-950 text-purple-400" :
                          s.secretType === "github_app" ? "border-violet-800 bg-violet-950 text-violet-400" :
                          "border-amber-800 bg-amber-950 text-amber-400"
                        }`}>
                          {getTypeLabel(s.secretType)}
                        </span>
                      </td>
                      <td className="px-5 py-3 text-zinc-500">
                        {new Date(s.created_at).toLocaleDateString()}
                      </td>
                      <td className="px-5 py-3 space-x-3">
                        <button
                          onClick={() => handleValidate(s.name)}
                          disabled={validating === s.name}
                          className="text-xs text-emerald-400 hover:text-emerald-300 disabled:opacity-50"
                        >
                          {validating === s.name ? "Testing..." : "Test"}
                        </button>
                        <button
                          onClick={() => handleDelete(s.name)}
                          className="text-xs text-red-400 hover:text-red-300"
                        >
                          Delete
                        </button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      </main>
    </div>
  );
}
