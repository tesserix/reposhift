"use client";

import { useEffect, useState } from "react";
import { api, type DashboardStats, type Migration } from "@/lib/api";
import Nav from "@/components/nav";
import Link from "next/link";

export default function DashboardPage() {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [migrations, setMigrations] = useState<Migration[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!api.isAuthenticated()) {
      window.location.href = "/login";
      return;
    }

    Promise.all([api.getDashboardStats(), api.listMigrations(1, 5)])
      .then(([s, m]) => {
        setStats(s);
        setMigrations(m.items || []);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-zinc-500">Loading...</p>
      </div>
    );
  }

  const statCards = [
    { label: "Total Migrations", value: stats?.total_migrations ?? 0, color: "text-zinc-50" },
    { label: "Completed", value: stats?.completed ?? 0, color: "text-emerald-400" },
    { label: "In Progress", value: stats?.in_progress ?? 0, color: "text-blue-400" },
    { label: "Failed", value: stats?.failed ?? 0, color: "text-red-400" },
  ];

  return (
    <div className="flex min-h-screen">
      <Nav />
      <main className="ml-60 flex-1 p-8">
        <div className="mb-8">
          <h1 className="text-2xl font-bold">Dashboard</h1>
          <p className="mt-1 text-sm text-zinc-400">
            Overview of your migration activity
          </p>
        </div>

        <div className="mb-8 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {statCards.map((card) => (
            <div
              key={card.label}
              className="rounded-xl border border-zinc-800 bg-zinc-900/50 p-5"
            >
              <p className="text-xs font-medium text-zinc-500">{card.label}</p>
              <p className={`mt-2 text-3xl font-bold ${card.color}`}>
                {card.value}
              </p>
            </div>
          ))}
        </div>

        <div className="rounded-xl border border-zinc-800 bg-zinc-900/50">
          <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-4">
            <h2 className="text-sm font-semibold">Recent Migrations</h2>
            <Link
              href="/migrations"
              className="text-xs text-zinc-400 hover:text-zinc-200"
            >
              View all
            </Link>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-left text-xs text-zinc-500">
                  <th className="px-5 py-3 font-medium">Name</th>
                  <th className="px-5 py-3 font-medium">Status</th>
                  <th className="px-5 py-3 font-medium">Progress</th>
                  <th className="px-5 py-3 font-medium">Created</th>
                </tr>
              </thead>
              <tbody>
                {migrations.length === 0 ? (
                  <tr>
                    <td
                      colSpan={4}
                      className="px-5 py-8 text-center text-zinc-500"
                    >
                      No migrations yet.{" "}
                      <Link
                        href="/migrations/new"
                        className="text-emerald-400 hover:underline"
                      >
                        Create one
                      </Link>
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
                      <td className="px-5 py-3">
                        <StatusBadge status={m.status} />
                      </td>
                      <td className="px-5 py-3">
                        <div className="flex items-center gap-2">
                          <div className="h-1.5 w-24 overflow-hidden rounded-full bg-zinc-800">
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

  const colorClass =
    colors[status] || "bg-zinc-800 text-zinc-400 border-zinc-700";

  return (
    <span
      className={`inline-flex rounded-md border px-2 py-0.5 text-xs font-medium ${colorClass}`}
    >
      {status.replace(/_/g, " ")}
    </span>
  );
}
