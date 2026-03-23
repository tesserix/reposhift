"use client";

import { useEffect, useState } from "react";
import { api, type Secret } from "@/lib/api";
import Sidebar from "@/components/sidebar";
import { PageHeader } from "@/components/page-header";
import { Card, CardContent } from "@/components/card";
import { Input } from "@/components/input";
import { Select } from "@/components/select";
import { Button } from "@/components/button";

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
      <Sidebar />
      <main className="ml-60 flex-1 p-6">
        <PageHeader
          title="New Migration"
          description="Configure a new ADO to GitHub migration"
          backHref="/migrations"
          backLabel="Back to migrations"
        />

        <form onSubmit={handleSubmit} className="max-w-2xl space-y-6">
          {error && (
            <div className="rounded-lg border border-red-800/60 bg-red-950/50 px-4 py-3 text-sm text-red-400">
              {error}
            </div>
          )}

          {/* General */}
          <Card>
            <CardContent className="pt-5 space-y-3">
              <Input
                label="Display Name"
                type="text"
                required
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="My migration"
              />
            </CardContent>
          </Card>

          {/* Source */}
          <Card>
            <div className="px-5 pt-5 pb-1">
              <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Source (Azure DevOps)
              </p>
            </div>
            <CardContent className="space-y-3 pt-3">
              <div className="grid grid-cols-2 gap-3">
                <Input
                  label="Organization"
                  type="text"
                  required
                  value={sourceOrg}
                  onChange={(e) => setSourceOrg(e.target.value)}
                  placeholder="my-ado-org"
                />
                <Input
                  label="Project"
                  type="text"
                  required
                  value={sourceProject}
                  onChange={(e) => setSourceProject(e.target.value)}
                  placeholder="my-project"
                />
              </div>
              <Input
                label="Repositories"
                type="text"
                value={sourceRepos}
                onChange={(e) => setSourceRepos(e.target.value)}
                placeholder="repo1, repo2, repo3"
                helperText="Comma separated. Leave empty to migrate all repositories."
              />
            </CardContent>
          </Card>

          {/* Target */}
          <Card>
            <div className="px-5 pt-5 pb-1">
              <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Target (GitHub)
              </p>
            </div>
            <CardContent className="space-y-3 pt-3">
              <Input
                label="GitHub Owner"
                type="text"
                required
                value={targetOwner}
                onChange={(e) => setTargetOwner(e.target.value)}
                placeholder="my-github-org"
                helperText="Organization or user account."
              />
            </CardContent>
          </Card>

          {/* Branch Filtering */}
          <Card>
            <div className="px-5 pt-5 pb-1">
              <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Branch Filtering
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                Control which branches are migrated. By default all branches are included.
              </p>
            </div>
            <CardContent className="pt-3">
              <div className="mb-3 flex gap-2">
                {(["", "include", "exclude"] as const).map((mode) => {
                  const labels = { "": "All branches", include: "Include only", exclude: "Exclude" };
                  const isActive = branchFilterMode === mode;
                  const activeStyles: Record<string, string> = {
                    "": "border-primary/50 bg-primary/10 text-primary",
                    include: "border-blue-700/50 bg-blue-950/50 text-blue-400",
                    exclude: "border-amber-700/50 bg-amber-950/50 text-amber-400",
                  };
                  return (
                    <button
                      key={mode}
                      type="button"
                      onClick={() => setBranchFilterMode(mode)}
                      className={`rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors ${
                        isActive
                          ? activeStyles[mode]
                          : "border-border text-muted-foreground hover:border-input-border hover:text-foreground"
                      }`}
                    >
                      {labels[mode]}
                    </button>
                  );
                })}
              </div>

              {branchFilterMode !== "" && (
                <div className="space-y-3">
                  <p className="text-xs text-muted-foreground">
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
                      className="flex-1 rounded-lg border border-input-border bg-input px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground transition-colors focus-ring focus:border-primary"
                    />
                    <Button type="button" variant="secondary" size="default" onClick={addBranch}>
                      Add
                    </Button>
                  </div>

                  {branches.length > 0 && (
                    <div className="flex flex-wrap gap-2">
                      {branches.map((b) => (
                        <span
                          key={b}
                          className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium ${
                            branchFilterMode === "include"
                              ? "border-blue-800/50 bg-blue-950/40 text-blue-400"
                              : "border-amber-800/50 bg-amber-950/40 text-amber-400"
                          }`}
                        >
                          {b}
                          <button
                            type="button"
                            onClick={() => removeBranch(b)}
                            className="text-current opacity-50 transition-opacity hover:opacity-100"
                          >
                            <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
                            </svg>
                          </button>
                        </span>
                      ))}
                    </div>
                  )}

                  {branches.length === 0 && (
                    <p className="text-xs text-muted-foreground/60">
                      No branches added yet. Add branch names or patterns above.
                    </p>
                  )}
                </div>
              )}
            </CardContent>
          </Card>

          {/* Credentials */}
          <Card>
            <div className="px-5 pt-5 pb-1">
              <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Credentials
              </p>
            </div>
            <CardContent className="space-y-3 pt-3">
              <div className="grid grid-cols-2 gap-3">
                <Select
                  label="ADO Secret"
                  required
                  value={adoSecretName}
                  onChange={(e) => setAdoSecretName(e.target.value)}
                >
                  <option value="">Select ADO credential...</option>
                  {adoSecrets.map((s) => (
                    <option key={s.name} value={s.name}>
                      {s.name}
                    </option>
                  ))}
                </Select>
                <Select
                  label="GitHub Secret"
                  required
                  value={githubSecretName}
                  onChange={(e) => setGithubSecretName(e.target.value)}
                >
                  <option value="">Select GitHub credential...</option>
                  {githubSecrets.map((s) => (
                    <option key={s.name} value={s.name}>
                      {s.name}
                    </option>
                  ))}
                </Select>
              </div>
              {secrets.length === 0 && (
                <p className="text-xs text-muted-foreground">
                  No secrets configured.{" "}
                  <a href="/secrets" className="text-primary hover:underline">
                    Add secrets first
                  </a>
                </p>
              )}
            </CardContent>
          </Card>

          {/* Actions */}
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={submitting}>
              {submitting ? "Creating..." : "Create Migration"}
            </Button>
            <Button variant="ghost" type="button" onClick={() => (window.location.href = "/migrations")}>
              Cancel
            </Button>
          </div>
        </form>
      </main>
    </div>
  );
}
