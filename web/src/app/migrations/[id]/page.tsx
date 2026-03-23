"use client";

import { useEffect, useState, useCallback } from "react";
import { useParams } from "next/navigation";
import { api, type Migration } from "@/lib/api";
import { wsClient } from "@/lib/ws";
import Sidebar from "@/components/sidebar";
import { PageHeader } from "@/components/page-header";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/card";
import { StatusBadge } from "@/components/badge";
import { Button } from "@/components/button";

export default function MigrationDetailPage() {
  const params = useParams();
  const id = params.id as string;
  const [migration, setMigration] = useState<Migration | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const loadMigration = useCallback(() => {
    api
      .getMigration(id)
      .then(setMigration)
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load migration");
      })
      .finally(() => setLoading(false));
  }, [id]);

  useEffect(() => {
    if (!api.isAuthenticated()) {
      window.location.href = "/login";
      return;
    }
    loadMigration();

    wsClient.connect();
    const unsub = wsClient.on("migration_updated", (data) => {
      const updated = data as Migration;
      if (updated.id === id) {
        setMigration(updated);
      }
    });

    const interval = setInterval(loadMigration, 5000);

    return () => {
      unsub();
      clearInterval(interval);
      wsClient.disconnect();
    };
  }, [id, loadMigration]);

  async function runAction(
    action: string,
    fn: (id: string) => Promise<Migration>
  ) {
    setActionLoading(action);
    try {
      const updated = await fn(id);
      setMigration(updated);
    } catch {
      // ignore
    } finally {
      setActionLoading(null);
    }
  }

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="flex items-center gap-3 text-sm text-muted-foreground">
          <svg className="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z" />
          </svg>
          Loading...
        </div>
      </div>
    );
  }

  if (!migration) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-center">
          {error && (
            <div className="mb-4 rounded-lg border border-red-800/60 bg-red-950/50 px-4 py-3 text-sm text-red-400">
              {error}
            </div>
          )}
          <p className="text-sm text-muted-foreground">Migration not found.</p>
        </div>
      </div>
    );
  }

  const m = migration;
  const isActive = ["running", "in_progress", "pending"].includes(m.status);
  const isPaused = m.status === "paused";
  const isFailed = m.status === "failed";
  const isDone = ["completed", "cancelled"].includes(m.status);

  const progressColor =
    m.status === "failed"
      ? "bg-red-500"
      : m.status === "completed"
      ? "bg-primary"
      : "bg-blue-500";

  return (
    <div className="flex min-h-screen">
      <Sidebar />
      <main className="ml-60 flex-1 p-6">
        <PageHeader
          title={m.display_name}
          backHref="/migrations"
          backLabel="Back to migrations"
        />

        {/* Status + Phase bar */}
        <div className="mb-6 flex items-center gap-3">
          <StatusBadge status={m.status} />
          {m.phase && (
            <span className="text-xs text-muted-foreground">
              Phase: {m.phase}
            </span>
          )}
          <div className="ml-auto flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-primary animate-pulse" />
            Auto-refreshing
          </div>
        </div>

        {error && (
          <div className="mb-6 rounded-lg border border-red-800/60 bg-red-950/50 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {/* Progress */}
        <Card className="mb-6">
          <CardContent className="pt-5">
            <div className="mb-3 flex items-center justify-between text-sm">
              <span className="text-muted-foreground">Overall Progress</span>
              <span className="font-mono font-medium tabular-nums">{m.progress}%</span>
            </div>
            <div className="h-2.5 overflow-hidden rounded-full bg-secondary">
              <div
                className={`h-full rounded-full transition-all duration-700 ease-out ${progressColor}`}
                style={{ width: `${m.progress}%` }}
              />
            </div>
            {m.error && (
              <div className="mt-4 rounded-lg border border-red-800/60 bg-red-950/40 px-4 py-3 text-sm text-red-400">
                {m.error}
              </div>
            )}
          </CardContent>
        </Card>

        {/* Info cards */}
        <div className="mb-6 grid grid-cols-2 gap-4">
          <Card>
            <CardContent className="pt-5">
              <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Source</p>
              <p className="mt-2 text-sm font-medium text-foreground">
                {m.source_org}/{m.source_project}
              </p>
              {m.source_repos && m.source_repos.length > 0 && (
                <p className="mt-1 text-xs text-muted-foreground">
                  Repos: {m.source_repos.join(", ")}
                </p>
              )}
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-5">
              <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Target</p>
              <p className="mt-2 text-sm font-medium text-foreground">{m.target_owner}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                Created {new Date(m.created_at).toLocaleString()}
              </p>
            </CardContent>
          </Card>
        </div>

        {/* Action toolbar */}
        {!isDone && (
          <div className="mb-6 flex gap-2">
            {isActive && (
              <Button
                variant="outline"
                size="sm"
                disabled={actionLoading === "pause"}
                onClick={() => runAction("pause", api.pauseMigration)}
              >
                <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 5.25v13.5m-7.5-13.5v13.5" />
                </svg>
                {actionLoading === "pause" ? "Pausing..." : "Pause"}
              </Button>
            )}
            {isPaused && (
              <Button
                variant="outline"
                size="sm"
                disabled={actionLoading === "resume"}
                onClick={() => runAction("resume", api.resumeMigration)}
              >
                <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M5.25 5.653c0-.856.917-1.398 1.667-.986l11.54 6.348a1.125 1.125 0 010 1.971l-11.54 6.347a1.125 1.125 0 01-1.667-.985V5.653z" />
                </svg>
                {actionLoading === "resume" ? "Resuming..." : "Resume"}
              </Button>
            )}
            {(isActive || isPaused) && (
              <Button
                variant="ghost"
                size="sm"
                disabled={actionLoading === "cancel"}
                onClick={() => runAction("cancel", api.cancelMigration)}
              >
                <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
                </svg>
                {actionLoading === "cancel" ? "Cancelling..." : "Cancel"}
              </Button>
            )}
            {isFailed && (
              <Button
                size="sm"
                disabled={actionLoading === "retry"}
                onClick={() => runAction("retry", api.retryMigration)}
              >
                <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182" />
                </svg>
                {actionLoading === "retry" ? "Retrying..." : "Retry"}
              </Button>
            )}
          </div>
        )}

        {/* Resources table */}
        {m.resources && m.resources.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Resources</CardTitle>
            </CardHeader>
            <div className="divide-y divide-card-border/50">
              {m.resources.map((r, i) => (
                <div key={i} className="flex items-center gap-4 px-5 py-3">
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-foreground truncate">{r.name}</p>
                    <p className="text-xs text-muted-foreground">{r.type}</p>
                  </div>
                  <StatusBadge status={r.status} />
                  <div className="flex w-32 items-center gap-2.5">
                    <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-secondary">
                      <div
                        className={`h-full rounded-full transition-all duration-500 ${
                          r.status === "failed" ? "bg-red-500" :
                          r.status === "completed" ? "bg-primary" : "bg-blue-500"
                        }`}
                        style={{ width: `${r.progress}%` }}
                      />
                    </div>
                    <span className="text-xs tabular-nums text-muted-foreground">{r.progress}%</span>
                  </div>
                  {r.error && (
                    <span
                      className="flex items-center gap-1 text-xs text-red-400"
                      title={r.error}
                    >
                      <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                        <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" />
                      </svg>
                      Error
                    </span>
                  )}
                </div>
              ))}
            </div>
          </Card>
        )}
      </main>
    </div>
  );
}
