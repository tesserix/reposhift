"use client";

import { useEffect, useState } from "react";
import { api, type Secret, type SecretValidation } from "@/lib/api";
import Sidebar from "@/components/sidebar";
import { PageHeader } from "@/components/page-header";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/card";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/table";
import { Badge } from "@/components/badge";
import { Button } from "@/components/button";
import { Input } from "@/components/input";
import { Select } from "@/components/select";
import { EmptyState } from "@/components/empty-state";

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

function getTypeBadgeVariant(type: string): "info" | "success" | "warning" | "default" {
  const map: Record<string, "info" | "success" | "warning" | "default"> = {
    ado_pat: "info",
    github_token: "success",
    github_app: "success",
    azure_sp: "warning",
  };
  return map[type] || "default";
}

function isRequiredField(field: string): boolean {
  return ["token", "client_id", "client_secret", "tenant_id", "app_id", "installation_id", "private_key"].includes(field);
}

export default function SecretsPage() {
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [loading, setLoading] = useState(true);
  const [showPanel, setShowPanel] = useState(false);
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
      setShowPanel(false);
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
      <Sidebar />
      <main className="ml-60 flex-1 p-6">
        <PageHeader
          title="Secrets"
          description="Store and validate ADO PATs, GitHub tokens, and service credentials"
          action={
            <Button
              onClick={() => {
                setShowPanel(!showPanel);
                setError(null);
              }}
              variant={showPanel ? "outline" : "primary"}
            >
              {showPanel ? "Cancel" : "Add Secret"}
            </Button>
          }
        />

        {/* Slide-down panel for adding secrets */}
        {showPanel && (
          <Card className="mb-6 overflow-hidden">
            <CardHeader>
              <CardTitle>Add New Secret</CardTitle>
            </CardHeader>
            <CardContent>
              <form onSubmit={handleCreate} className="space-y-4">
                {error && (
                  <div className="rounded-lg border border-red-800/60 bg-red-950/50 px-4 py-3 text-sm text-red-400">
                    {error}
                  </div>
                )}
                <div className="grid grid-cols-2 gap-3">
                  <Input
                    label="Name"
                    type="text"
                    required
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="my-ado-pat"
                  />
                  <Select
                    label="Type"
                    value={type}
                    onChange={(e) => handleTypeChange(e.target.value)}
                  >
                    {SECRET_TYPES.map((t) => (
                      <option key={t.value} value={t.value}>
                        {t.label}
                      </option>
                    ))}
                  </Select>
                </div>

                <div className="grid grid-cols-2 gap-3">
                  {currentFields.map((field) => {
                    const label = field.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
                    const required = isRequiredField(field);

                    if (field === "private_key") {
                      return (
                        <div key={field} className="col-span-2 space-y-1.5">
                          <label className="block text-xs font-medium text-muted-foreground">
                            {label}
                          </label>
                          <textarea
                            value={fields[field] ?? ""}
                            onChange={(e) => updateField(field, e.target.value)}
                            placeholder={`Enter ${field}...`}
                            rows={3}
                            required={required}
                            className="w-full rounded-lg border border-input-border bg-input px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground transition-colors focus-ring focus:border-primary resize-y min-h-[80px]"
                          />
                        </div>
                      );
                    }

                    return (
                      <Input
                        key={field}
                        label={`${label}${!required ? " (optional)" : ""}`}
                        type={field.includes("secret") || field === "token" ? "password" : "text"}
                        value={fields[field] ?? ""}
                        onChange={(e) => updateField(field, e.target.value)}
                        placeholder={`Enter ${field}...`}
                        required={required}
                      />
                    );
                  })}
                </div>

                <div className="flex gap-2 pt-1">
                  <Button type="submit" disabled={submitting}>
                    {submitting ? "Saving..." : "Save Secret"}
                  </Button>
                  <Button type="button" variant="ghost" onClick={() => setShowPanel(false)}>
                    Cancel
                  </Button>
                </div>
              </form>
            </CardContent>
          </Card>
        )}

        {/* Validation results */}
        {validation && (
          <Card className="mb-6">
            <CardHeader className="flex flex-row items-center justify-between">
              <div className="flex items-center gap-3">
                <div
                  className={`flex h-7 w-7 items-center justify-center rounded-full ${
                    validation.valid
                      ? "bg-emerald-950/50 text-emerald-400"
                      : "bg-red-950/50 text-red-400"
                  }`}
                >
                  {validation.valid ? (
                    <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}>
                      <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
                    </svg>
                  ) : (
                    <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}>
                      <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  )}
                </div>
                <div>
                  <CardTitle>
                    Validation: <span className="text-muted-foreground font-normal">{validation.name}</span>
                  </CardTitle>
                </div>
              </div>
              <Button variant="ghost" size="sm" onClick={() => setValidation(null)}>
                Dismiss
              </Button>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                {validation.checks.map((check, i) => (
                  <div
                    key={i}
                    className="flex items-start gap-3 rounded-lg border border-card-border bg-background px-4 py-3"
                  >
                    <span className={`mt-0.5 shrink-0 ${
                      check.status === "passed" ? "text-emerald-400" :
                      check.status === "failed" ? "text-red-400" :
                      check.status === "warning" ? "text-amber-400" : "text-muted-foreground"
                    }`}>
                      {check.status === "passed" ? (
                        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                          <path strokeLinecap="round" strokeLinejoin="round" d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                        </svg>
                      ) : check.status === "failed" ? (
                        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                          <path strokeLinecap="round" strokeLinejoin="round" d="M9.75 9.75l4.5 4.5m0-4.5l-4.5 4.5M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                        </svg>
                      ) : check.status === "warning" ? (
                        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                          <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
                        </svg>
                      ) : (
                        <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                          <path strokeLinecap="round" strokeLinejoin="round" d="M15 12H9m12 0a9 9 0 11-18 0 9 9 0 0118 0z" />
                        </svg>
                      )}
                    </span>
                    <div>
                      <p className="text-xs font-medium text-foreground">{check.check}</p>
                      <p className="text-xs text-muted-foreground">{check.message}</p>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        )}

        {/* Secrets table */}
        <Card>
          {loading ? (
            <div className="flex items-center justify-center py-16">
              <div className="flex items-center gap-3 text-sm text-muted-foreground">
                <svg className="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z" />
                </svg>
                Loading secrets...
              </div>
            </div>
          ) : secrets.length === 0 ? (
            <EmptyState
              icon={
                <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M16.5 10.5V6.75a4.5 4.5 0 10-9 0v3.75m-.75 11.25h10.5a2.25 2.25 0 002.25-2.25v-6.75a2.25 2.25 0 00-2.25-2.25H6.75a2.25 2.25 0 00-2.25 2.25v6.75a2.25 2.25 0 002.25 2.25z" />
                </svg>
              }
              title="No secrets configured"
              description="Add your ADO PATs, GitHub tokens, or service principal credentials to get started."
              action={
                <Button size="sm" onClick={() => setShowPanel(true)}>
                  Add Secret
                </Button>
              }
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-36">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {secrets.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-medium text-foreground">
                      {s.name}
                    </TableCell>
                    <TableCell>
                      <Badge variant={getTypeBadgeVariant(s.secretType)}>
                        {getTypeLabel(s.secretType)}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {new Date(s.created_at).toLocaleDateString()}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleValidate(s.name)}
                          disabled={validating === s.name}
                          className="text-primary hover:text-primary"
                        >
                          {validating === s.name ? "Testing..." : "Test"}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDelete(s.name)}
                          className="text-destructive hover:text-destructive"
                        >
                          Delete
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </Card>
      </main>
    </div>
  );
}
