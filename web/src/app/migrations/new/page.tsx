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
  const [adoSecretName, setAdoSecretName] = useState("");
  const [githubSecretName, setGithubSecretName] = useState("");

  // Branch filtering
  const [branchFilterMode, setBranchFilterMode] = useState<"" | "include" | "exclude">("");
  const [branchInput, setBranchInput] = useState("");
  const [branches, setBranches] = useState<string[]>([]);

  useEffect(() => {
    if (!api.isAuthenticated()) {
      window.location.href = "/login";
      return;
    }
    api
      .listSecrets()
      .then((res) => setSecrets(res.secrets ?? []))
      .catch(() => {});
  }, []);

  const adoSecrets = secrets.filter(
    (s) => s.secretType === "ado_pat" || s.secretType === "azure_sp"
  );
  const githubSecrets = secrets.filter(
    (s) => s.secretType === "github_token" || s.secretType === "github_app"
  );

  function addBranch() {
    const trimmed = branchInput.trim();
    if (trimmed && !branches.includes(trimmed)) {
      setBranches([...branches, trimmed]);
      setBranchInput("");
    }
  }

  function removeBranch(b: string) {
    setBranches(branches.filter((x) => x !== b));
  }

  function handleBranchKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      addBranch();
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);

    if (!adoSecretName) {
      setError("Please select an ADO credential");
      return;
    }
    if (!githubSecretName) {
      setError("Please select a GitHub credential");
      return;
    }
    if (branchFilterMode && branches.length === 0) {
      setError(`Branch filter mode is set to "${branchFilterMode}" but no branches have been added`);
      return;
    }

    setSubmitting(true);

    try {
      const repos = sourceRepos
        .split(",")
        .map((r) => r.trim())
        .filter(Boolean);

      await api.createMigration({
        displayName,
        sourceOrg,
        sourceProject,
        sourceRepos: repos,
        targetOwner,
        adoSecretName,
        githubSecretName,
        branchFilterMode: branchFilterMode || undefined,
        branches: branches.length > 0 ? branches : undefined,
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

          {/* Source */}
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

          {/* Target */}
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

          {/* Branch Filtering */}
          <div className="border-t border-zinc-800 pt-6">
            <h3 className="mb-1 text-sm font-semibold text-zinc-300">
              Branch Filtering
            </h3>
            <p className="mb-4 text-xs text-zinc-500">
              Control which branches are migrated. By default all branches are included.
            </p>

            <div className="mb-4 flex gap-2">
              <button
                type="button"
                onClick={() => setBranchFilterMode(branchFilterMode === "" ? "" : "")}
                className={`rounded-lg border px-3 py-1.5 text-xs font-medium transition ${
                  branchFilterMode === ""
                    ? "border-emerald-700 bg-emerald-950 text-emerald-400"
                    : "border-zinc-700 text-zinc-400 hover:border-zinc-600"
                }`}
              >
                All branches
              </button>
              <button
                type="button"
                onClick={() => setBranchFilterMode("include")}
                className={`rounded-lg border px-3 py-1.5 text-xs font-medium transition ${
                  branchFilterMode === "include"
                    ? "border-blue-700 bg-blue-950 text-blue-400"
                    : "border-zinc-700 text-zinc-400 hover:border-zinc-600"
                }`}
              >
                Include only
              </button>
              <button
                type="button"
                onClick={() => setBranchFilterMode("exclude")}
                className={`rounded-lg border px-3 py-1.5 text-xs font-medium transition ${
                  branchFilterMode === "exclude"
                    ? "border-amber-700 bg-amber-950 text-amber-400"
                    : "border-zinc-700 text-zinc-400 hover:border-zinc-600"
                }`}
              >
                Exclude
              </button>
            </div>

            {branchFilterMode !== "" && (
              <div>
                <p className="mb-2 text-xs text-zinc-400">
                  {branchFilterMode === "include"
                    ? "Only these branches will be migrated. Supports glob patterns (e.g. feature/*)."
                    : "These branches will be skipped. The default branch is never excluded. Supports glob patterns."}
                </p>
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={branchInput}
                    onChange={(e) => setBranchInput(e.target.value)}
                    onKeyDown={handleBranchKeyDown}
                    placeholder={
                      branchFilterMode === "include"
                        ? "e.g. main, develop, release/*"
                        : "e.g. dependabot/*, feature/legacy-*"
                    }
                    className="input flex-1"
                  />
                  <button
                    type="button"
                    onClick={addBranch}
                    className="rounded-lg bg-zinc-800 px-3 py-1.5 text-xs font-medium text-zinc-300 transition hover:bg-zinc-700"
                  >
                    Add
                  </button>
                </div>

                {branches.length > 0 && (
                  <div className="mt-3 flex flex-wrap gap-2">
                    {branches.map((b) => (
                      <span
                        key={b}
                        className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium ${
                          branchFilterMode === "include"
                            ? "border-blue-800 bg-blue-950/50 text-blue-400"
                            : "border-amber-800 bg-amber-950/50 text-amber-400"
                        }`}
                      >
                        {b}
                        <button
                          type="button"
                          onClick={() => removeBranch(b)}
                          className="ml-0.5 text-zinc-500 hover:text-zinc-300"
                        >
                          &times;
                        </button>
                      </span>
                    ))}
                  </div>
                )}

                {branches.length === 0 && branchFilterMode !== "" && (
                  <p className="mt-2 text-xs text-zinc-600">
                    No branches added yet. Add branch names or patterns above.
                  </p>
                )}
              </div>
            )}
          </div>

          {/* Credentials */}
          <div className="border-t border-zinc-800 pt-6">
            <h3 className="mb-4 text-sm font-semibold text-zinc-300">
              Credentials
            </h3>
            <div className="grid grid-cols-2 gap-4">
              <Field label="ADO Secret">
                <select
                  required
                  value={adoSecretName}
                  onChange={(e) => setAdoSecretName(e.target.value)}
                  className="input"
                >
                  <option value="">Select ADO credential...</option>
                  {adoSecrets.map((s) => (
                    <option key={s.name} value={s.name}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="GitHub Secret">
                <select
                  required
                  value={githubSecretName}
                  onChange={(e) => setGithubSecretName(e.target.value)}
                  className="input"
                >
                  <option value="">Select GitHub credential...</option>
                  {githubSecrets.map((s) => (
                    <option key={s.name} value={s.name}>
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
