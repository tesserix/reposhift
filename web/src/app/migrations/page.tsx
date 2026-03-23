"use client";

import { useEffect, useState } from "react";
import { api, type Migration } from "@/lib/api";
import Sidebar from "@/components/sidebar";
import { PageHeader } from "@/components/page-header";
import { Card, CardFooter } from "@/components/card";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/table";
import { StatusBadge } from "@/components/badge";
import { Button } from "@/components/button";
import { EmptyState } from "@/components/empty-state";
import Link from "next/link";

export default function MigrationsPage() {
  const [migrations, setMigrations] = useState<Migration[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const limit = 20;

  useEffect(() => {
    if (!api.isAuthenticated()) {
      window.location.href = "/login";
      return;
    }
    loadMigrations();
  }, [page]);

  function loadMigrations() {
    setLoading(true);
    setError(null);
    api
      .listMigrations(page, limit)
      .then((data) => {
        setMigrations(data.items || []);
        setTotal(data.total || 0);
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load migrations");
      })
      .finally(() => setLoading(false));
  }

  const totalPages = Math.ceil(total / limit);

  return (
    <div className="flex min-h-screen">
      <Sidebar />
      <main className="ml-60 flex-1 p-6">
        <PageHeader
          title="Migrations"
          description="Manage your ADO to GitHub migrations"
          action={
            <Link href="/migrations/new">
              <Button>Create Migration</Button>
            </Link>
          }
        />

        {error && (
          <div className="mb-6 rounded-lg border border-red-800/60 bg-red-950/50 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        <Card>
          {loading ? (
            <div className="flex items-center justify-center py-16">
              <div className="flex items-center gap-3 text-sm text-muted-foreground">
                <svg className="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z" />
                </svg>
                Loading migrations...
              </div>
            </div>
          ) : migrations.length === 0 ? (
            <EmptyState
              icon={
                <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M7.5 21L3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" />
                </svg>
              }
              title="No migrations found"
              description="Create your first migration to start moving repositories from Azure DevOps to GitHub."
              action={
                <Link href="/migrations/new">
                  <Button size="sm">Create Migration</Button>
                </Link>
              }
            />
          ) : (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Source</TableHead>
                    <TableHead>Target</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Progress</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead className="w-20">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {migrations.map((m) => (
                    <TableRow key={m.id}>
                      <TableCell>
                        <Link
                          href={`/migrations/${m.id}`}
                          className="font-medium text-foreground transition-colors hover:text-primary"
                        >
                          {m.display_name}
                        </Link>
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {m.source_org}/{m.source_project}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {m.target_owner}
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={m.status} />
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2.5">
                          <div className="h-1.5 w-20 overflow-hidden rounded-full bg-secondary">
                            <div
                              className="h-full rounded-full bg-primary transition-all duration-500"
                              style={{ width: `${m.progress}%` }}
                            />
                          </div>
                          <span className="text-xs tabular-nums text-muted-foreground">
                            {m.progress}%
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {new Date(m.created_at).toLocaleDateString()}
                      </TableCell>
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            if (confirm("Delete this migration?")) {
                              api.deleteMigration(m.id).then(loadMigrations);
                            }
                          }}
                          className="text-destructive hover:text-destructive"
                        >
                          Delete
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              {totalPages > 1 && (
                <CardFooter className="justify-between">
                  <p className="text-xs text-muted-foreground">
                    Page {page} of {totalPages} ({total} total)
                  </p>
                  <div className="flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setPage((p) => Math.max(1, p - 1))}
                      disabled={page === 1}
                    >
                      Previous
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                      disabled={page === totalPages}
                    >
                      Next
                    </Button>
                  </div>
                </CardFooter>
              )}
            </>
          )}
        </Card>
      </main>
    </div>
  );
}
