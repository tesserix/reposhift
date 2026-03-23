"use client";

import { useEffect, useState, useCallback } from "react";
import { useParams } from "next/navigation";
import { api, type Migration } from "@/lib/api";
import { wsClient } from "@/lib/ws";
import Nav from "@/components/nav";
import Link from "next/link";

export default function MigrationDetailPage() {
  const params = useParams();
  const id = params.id as string;
  const [migration, setMigration] = useState<Migration | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  const loadMigration = useCallback(() => {
    api
      .getMigration(id)
      .then(setMigration)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [id]);

  useEffect(() => {
    if (!api.isAuthenticated()) {
      window.location.href = "/login";
      return;
    }
    loadMigration();

    // WebSocket for live updates
    wsClient.connect();
    const unsub = wsClient.on("migration_updated", (data) => {
      const updated = data as Migration;
      if (updated.id === id) {
        setMigration(updated);
      }
    });

    // Fallback polling every 5s
    const interval = setInterval(loadMigration, 5000);

    return () => {
      unsub();
      clearInterval(interval);
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
        <p className="text-sm text-zinc-500">Loading...</p>
      </div>
    );
  }

  if (!migration) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-zinc-500">Migration not found.</p>
      </div>
    );
  }

  const m = migration;
  const isActive = ["running", "in_progress", "pending"].includes(m.status);
  const isPaused = m.status === "paused";
  const isFailed = m.status === "failed";
  const isDone = ["completed", "cancelled"].includes(m.status);

  return (
    <div className="flex min-h-screen">
      <Nav />
      <main className="ml-60 flex-1 p-8">
        <div className="mb-6">
          <Link
            href="/migrations"
            className="text-xs text-zinc-500 hover:text-zinc-300"
          >
            &larr; Back to migrations
          </Link>
          <div className="mt-2 flex items-center gap-4">
            <h1 className="text-2xl font-bold">{m.display_name}</h1>
            <StatusBadge status={m.status} />
          </div>
          {m.phase && (
            <p className="mt-1 text-sm text-zinc-400">Phase: {m.phase}</p>
          )}
        </div>

        {/* Progress */}
        <div className="mb-6 rounded-xl border border-zinc-800 bg-zinc-900/50 p-5">
          <div className="mb-2 flex items-center justify-between text-sm">
            <span className="text-zinc-400">Overall Progress</span>
            <span className="font-mono font-medium">{m.progress}%</span>
          </div>
          <div className="h-2.5 overflow-hidden rounded-full bg-zinc-800">
            <div
              className={`h-full rounded-full transition-all duration-500 ${
                m.status === "failed"
                  ? "bg-red-500"
                  : m.status === "completed"
                  ? "bg-emerald-500"
                  : "bg-blue-500"
              }`}
              style={{ width: `${m.progress}%` }}
            />
          </div>
          {m.error && (
            <p className="mt-3 rounded-lg border border-red-800 bg-red-950/50 px-3 py-2 text-sm text-red-300">
              {m.error}
            </p>
          )}
        </div>

        {/* Info */}
        <div className="mb-6 grid grid-cols-2 gap-4">
          <div className="rounded-xl border border-zinc-800 bg-zinc-900/50 p-5">
            <h3 className="mb-3 text-xs font-semibold text-zinc-500">Source</h3>
            <p className="text-sm">
              {m.source_org}/{m.source_project}
            </p>
            {m.source_repos && m.source_repos.length > 0 && (
              <p className="mt-1 text-xs text-zinc-500">
                Repos: {m.source_repos.join(", ")}
              </p>
            )}
          </div>
          <div className="rounded-xl border border-zinc-800 bg-zinc-900/50 p-5">
            <h3 className="mb-3 text-xs font-semibold text-zinc-500">Target</h3>
            <p className="text-sm">{m.target_owner}</p>
            <p className="mt-1 text-xs text-zinc-500">
              Created {new Date(m.created_at).toLocaleString()}
            </p>
          </div>
        </div>

        {/* Actions */}
        {!isDone && (
          <div className="mb-6 flex gap-2">
            {isActive && (
              <ActionButton
                label="Pause"
                loading={actionLoading === "pause"}
                onClick={() => runAction("pause", api.pauseMigration)}
                className="bg-yellow-900/50 text-yellow-400 border-yellow-800 hover:bg-yellow-900"
              />
            )}
            {isPaused && (
              <ActionButton
                label="Resume"
                loading={actionLoading === "resume"}
                onClick={() => runAction("resume", api.resumeMigration)}
                className="bg-blue-900/50 text-blue-400 border-blue-800 hover:bg-blue-900"
              />
            )}
            {(isActive || isPaused) && (
              <ActionButton
                label="Cancel"
                loading={actionLoading === "cancel"}
                onClick={() => runAction("cancel", api.cancelMigration)}
                className="bg-zinc-800 text-zinc-400 border-zinc-700 hover:bg-zinc-700"
              />
            )}
            {isFailed && (
              <ActionButton
                label="Retry"
                loading={actionLoading === "retry"}
                onClick={() => runAction("retry", api.retryMigration)}
                className="bg-emerald-900/50 text-emerald-400 border-emerald-800 hover:bg-emerald-900"
              />
            )}
          </div>
        )}

        {/* Resources */}
        {m.resources && m.resources.length > 0 && (
          <div className="rounded-xl border border-zinc-800 bg-zinc-900/50">
            <div className="border-b border-zinc-800 px-5 py-4">
              <h2 className="text-sm font-semibold">Resources</h2>
            </div>
            <div className="divide-y divide-zinc-800/50">
              {m.resources.map((r, i) => (
                <div key={i} className="flex items-center gap-4 px-5 py-3">
                  <div className="flex-1">
                    <p className="text-sm font-medium">{r.name}</p>
                    <p className="text-xs text-zinc-500">{r.type}</p>
                  </div>
                  <StatusBadge status={r.status} />
                  <div className="flex w-32 items-center gap-2">
                    <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-zinc-800">
                      <div
                        className="h-full rounded-full bg-emerald-500 transition-all"
                        style={{ width: `${r.progress}%` }}
                      />
                    </div>
                    <span className="text-xs text-zinc-500">{r.progress}%</span>
                  </div>
                  {r.error && (
                    <span className="text-xs text-red-400" title={r.error}>
                      error
                    </span>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    completed: "bg-emerald-950 text-emerald-400 border-emerald-800",
    running: "bg-blue-950 text-blue-400 border-blue-800",
    in_progress: "bg-blue-950 text-blue-400 border-blue-800",
    failed: "bg-red-950 text-red-400 border-red-800",
    paused: "bg-yellow-950 text-yellow-400 border-yellow-800",
    cancelled: "bg-zinc-800 text-zinc-400 border-zinc-700",
    pending: "bg-zinc-800 text-zinc-400 border-zinc-700",
  };
  const colorClass = colors[status] || "bg-zinc-800 text-zinc-400 border-zinc-700";

  return (
    <span className={`inline-flex rounded-md border px-2 py-0.5 text-xs font-medium ${colorClass}`}>
      {status.replace(/_/g, " ")}
    </span>
  );
}

function ActionButton({
  label,
  loading,
  onClick,
  className,
}: {
  label: string;
  loading: boolean;
  onClick: () => void;
  className: string;
}) {
  return (
    <button
      onClick={onClick}
      disabled={loading}
      className={`rounded-lg border px-4 py-1.5 text-sm font-medium transition disabled:opacity-50 ${className}`}
    >
      {loading ? `${label}...` : label}
    </button>
  );
}
