"use client";

import { useEffect, useState } from "react";
import { api, type Secret } from "@/lib/api";
import Nav from "@/components/nav";

export default function NewMigrationPage() {
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [displayName, setDisplayName] = useState("");
  const [sourceOrg, setSourceOrg] = useState("");
  const [sourceProject, setSourceProject] = useState("");
  const [sourceRepos, setSourceRepos] = useState("");
  const [targetOwner, setTargetOwner] = useState("");
  const [adoSecretId, setAdoSecretId] = useState("");
  const [githubSecretId, setGithubSecretId] = useState("");

  useEffect(() => {
    if (!api.isAuthenticated()) {
      window.location.href = "/login";
      return;
    }
    api.listSecrets().then(setSecrets).catch(() => {});
  }, []);

  const adoSecrets = secrets.filter((s) => s.type === "ado_pat");
  const githubSecrets = secrets.filter((s) => s.type === "github_token");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);

    try {
      const repos = sourceRepos
        .split(",")
        .map((r) => r.trim())
        .filter(Boolean);

      await api.createMigration({
        display_name: displayName,
        source_org: sourceOrg,
        source_project: sourceProject,
        source_repos: repos,
        target_owner: targetOwner,
        ado_secret_id: adoSecretId,
        github_secret_id: githubSecretId,
      });

      window.location.href = "/migrations";
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create migration");
      setSubmitting(false);
    }
  }

  return (
    <div className="flex min-h-screen">
      <Nav />
      <main className="ml-60 flex-1 p-8">
        <div className="mb-6">
          <h1 className="text-2xl font-bold">New Migration</h1>
          <p className="mt-1 text-sm text-zinc-400">
            Configure a new ADO to GitHub migration
          </p>
        </div>

        <form
          onSubmit={handleSubmit}
          className="max-w-2xl space-y-6 rounded-xl border border-zinc-800 bg-zinc-900/50 p-6"
        >
          {error && (
            <div className="rounded-lg border border-red-800 bg-red-950/50 px-4 py-3 text-sm text-red-300">
              {error}
            </div>
          )}

          <Field label="Display Name">
            <input
              type="text"
              required
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder="My migration"
              className="input"
            />
          </Field>

          <div className="border-t border-zinc-800 pt-6">
            <h3 className="mb-4 text-sm font-semibold text-zinc-300">
              Source (Azure DevOps)
            </h3>
            <div className="grid grid-cols-2 gap-4">
              <Field label="Organization">
                <input
                  type="text"
                  required
                  value={sourceOrg}
                  onChange={(e) => setSourceOrg(e.target.value)}
                  placeholder="my-ado-org"
                  className="input"
                />
              </Field>
              <Field label="Project">
                <input
                  type="text"
                  required
                  value={sourceProject}
                  onChange={(e) => setSourceProject(e.target.value)}
                  placeholder="my-project"
                  className="input"
                />
              </Field>
            </div>
            <div className="mt-4">
              <Field label="Repositories (comma separated, leave empty for all)">
                <input
                  type="text"
                  value={sourceRepos}
                  onChange={(e) => setSourceRepos(e.target.value)}
                  placeholder="repo1, repo2, repo3"
                  className="input"
                />
              </Field>
            </div>
          </div>

          <div className="border-t border-zinc-800 pt-6">
            <h3 className="mb-4 text-sm font-semibold text-zinc-300">
              Target (GitHub)
            </h3>
            <Field label="GitHub Owner (org or user)">
              <input
                type="text"
                required
                value={targetOwner}
                onChange={(e) => setTargetOwner(e.target.value)}
                placeholder="my-github-org"
                className="input"
              />
            </Field>
          </div>

          <div className="border-t border-zinc-800 pt-6">
            <h3 className="mb-4 text-sm font-semibold text-zinc-300">
              Credentials
            </h3>
            <div className="grid grid-cols-2 gap-4">
              <Field label="ADO Secret">
                <select
                  required
                  value={adoSecretId}
                  onChange={(e) => setAdoSecretId(e.target.value)}
                  className="input"
                >
                  <option value="">Select ADO PAT...</option>
                  {adoSecrets.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="GitHub Secret">
                <select
                  required
                  value={githubSecretId}
                  onChange={(e) => setGithubSecretId(e.target.value)}
                  className="input"
                >
                  <option value="">Select GitHub token...</option>
                  {githubSecrets.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
            {secrets.length === 0 && (
              <p className="mt-2 text-xs text-zinc-500">
                No secrets configured.{" "}
                <a href="/secrets" className="text-emerald-400 hover:underline">
                  Add secrets first
                </a>
              </p>
            )}
          </div>

          <div className="flex gap-3 border-t border-zinc-800 pt-6">
            <button
              type="submit"
              disabled={submitting}
              className="rounded-lg bg-emerald-600 px-5 py-2 text-sm font-medium text-white transition hover:bg-emerald-500 disabled:opacity-50"
            >
              {submitting ? "Creating..." : "Create Migration"}
            </button>
            <a
              href="/migrations"
              className="rounded-lg px-5 py-2 text-sm text-zinc-400 transition hover:bg-zinc-800"
            >
              Cancel
            </a>
          </div>
        </form>
      </main>
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-xs font-medium text-zinc-400">
        {label}
      </span>
      {children}
    </label>
  );
}
