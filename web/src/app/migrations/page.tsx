"use client";

import { useEffect, useState } from "react";
import { api, type Migration } from "@/lib/api";
import Nav from "@/components/nav";
import Link from "next/link";

export default function MigrationsPage() {
  const [migrations, setMigrations] = useState<Migration[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
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
    api
      .listMigrations(page, limit)
      .then((data) => {
        setMigrations(data.items || []);
        setTotal(data.total || 0);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }

  const totalPages = Math.ceil(total / limit);

  return (
    <div className="flex min-h-screen">
      <Nav />
      <main className="ml-60 flex-1 p-8">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold">Migrations</h1>
            <p className="mt-1 text-sm text-zinc-400">
              Manage your ADO to GitHub migrations
            </p>
          </div>
          <Link
            href="/migrations/new"
            className="rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-emerald-500"
          >
            New Migration
          </Link>
        </div>

        <div className="rounded-xl border border-zinc-800 bg-zinc-900/50">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-left text-xs text-zinc-500">
                  <th className="px-5 py-3 font-medium">Name</th>
                  <th className="px-5 py-3 font-medium">Source</th>
                  <th className="px-5 py-3 font-medium">Target</th>
                  <th className="px-5 py-3 font-medium">Status</th>
                  <th className="px-5 py-3 font-medium">Progress</th>
                  <th className="px-5 py-3 font-medium">Created</th>
                  <th className="px-5 py-3 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {loading ? (
                  <tr>
                    <td colSpan={7} className="px-5 py-8 text-center text-zinc-500">
                      Loading...
                    </td>
                  </tr>
                ) : migrations.length === 0 ? (
                  <tr>
                    <td colSpan={7} className="px-5 py-8 text-center text-zinc-500">
                      No migrations found.
                    </td>
                  </tr>
                ) : (
                  migrations.map((m) => (
                    <tr
                      key={m.id}
                      className="border-b border-zinc-800/50 transition hover:bg-zinc-800/30"
                    >
                      <td className="px-5 py-3">
                        <Link
                          href={`/migrations/${m.id}`}
                          className="font-medium hover:text-emerald-400"
                        >
                          {m.display_name}
                        </Link>
                      </td>
                      <td className="px-5 py-3 text-zinc-400">
                        {m.source_org}/{m.source_project}
                      </td>
                      <td className="px-5 py-3 text-zinc-400">
                        {m.target_owner}
                      </td>
                      <td className="px-5 py-3">
                        <StatusBadge status={m.status} />
                      </td>
                      <td className="px-5 py-3">
                        <div className="flex items-center gap-2">
                          <div className="h-1.5 w-20 overflow-hidden rounded-full bg-zinc-800">
                            <div
                              className="h-full rounded-full bg-emerald-500 transition-all"
                              style={{ width: `${m.progress}%` }}
                            />
                          </div>
                          <span className="text-xs text-zinc-500">
                            {m.progress}%
                          </span>
                        </div>
                      </td>
                      <td className="px-5 py-3 text-zinc-500">
                        {new Date(m.created_at).toLocaleDateString()}
                      </td>
                      <td className="px-5 py-3">
                        <button
                          onClick={() => {
                            if (confirm("Delete this migration?")) {
                              api.deleteMigration(m.id).then(loadMigrations);
                            }
                          }}
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

          {totalPages > 1 && (
            <div className="flex items-center justify-between border-t border-zinc-800 px-5 py-3">
              <p className="text-xs text-zinc-500">
                Page {page} of {totalPages} ({total} total)
              </p>
              <div className="flex gap-2">
                <button
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  disabled={page === 1}
                  className="rounded-lg px-3 py-1 text-xs text-zinc-400 transition hover:bg-zinc-800 disabled:opacity-50"
                >
                  Previous
                </button>
                <button
                  onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                  disabled={page === totalPages}
                  className="rounded-lg px-3 py-1 text-xs text-zinc-400 transition hover:bg-zinc-800 disabled:opacity-50"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </div>
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
