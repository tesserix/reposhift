"use client";

import { useEffect, useState } from "react";
import { api, type Secret } from "@/lib/api";
import Nav from "@/components/nav";

export default function SecretsPage() {
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [name, setName] = useState("");
  const [type, setType] = useState("ado_pat");
  const [token, setToken] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
      .then(setSecrets)
      .catch(() => {})
      .finally(() => setLoading(false));
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await api.createSecret(name, type, token);
      setName("");
      setToken("");
      setShowForm(false);
      loadSecrets();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create secret");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleDelete(id: string) {
    if (!confirm("Delete this secret? Migrations using it will fail.")) return;
    await api.deleteSecret(id);
    loadSecrets();
  }

  return (
    <div className="flex min-h-screen">
      <Nav />
      <main className="ml-60 flex-1 p-8">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold">Secrets</h1>
            <p className="mt-1 text-sm text-zinc-400">
              Manage ADO PATs and GitHub tokens
            </p>
          </div>
          <button
            onClick={() => setShowForm(!showForm)}
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
            <div className="grid grid-cols-3 gap-4">
              <label className="block">
                <span className="mb-1.5 block text-xs font-medium text-zinc-400">
                  Name
                </span>
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
                <span className="mb-1.5 block text-xs font-medium text-zinc-400">
                  Type
                </span>
                <select
                  value={type}
                  onChange={(e) => setType(e.target.value)}
                  className="input"
                >
                  <option value="ado_pat">ADO PAT</option>
                  <option value="github_token">GitHub Token</option>
                </select>
              </label>
              <label className="block">
                <span className="mb-1.5 block text-xs font-medium text-zinc-400">
                  Token
                </span>
                <input
                  type="password"
                  required
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="Enter token..."
                  className="input"
                />
              </label>
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
                    <td colSpan={4} className="px-5 py-8 text-center text-zinc-500">
                      Loading...
                    </td>
                  </tr>
                ) : secrets.length === 0 ? (
                  <tr>
                    <td colSpan={4} className="px-5 py-8 text-center text-zinc-500">
                      No secrets configured yet.
                    </td>
                  </tr>
                ) : (
                  secrets.map((s) => (
                    <tr
                      key={s.id}
                      className="border-b border-zinc-800/50 transition hover:bg-zinc-800/30"
                    >
                      <td className="px-5 py-3 font-medium">{s.name}</td>
                      <td className="px-5 py-3">
                        <span
                          className={`inline-flex rounded-md border px-2 py-0.5 text-xs font-medium ${
                            s.type === "ado_pat"
                              ? "border-blue-800 bg-blue-950 text-blue-400"
                              : "border-purple-800 bg-purple-950 text-purple-400"
                          }`}
                        >
                          {s.type === "ado_pat" ? "ADO PAT" : "GitHub Token"}
                        </span>
                      </td>
                      <td className="px-5 py-3 text-zinc-500">
                        {new Date(s.created_at).toLocaleDateString()}
                      </td>
                      <td className="px-5 py-3">
                        <button
                          onClick={() => handleDelete(s.id)}
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
