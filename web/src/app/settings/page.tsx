"use client";

import { useEffect, useState } from "react";
import { api, type Tenant, type Member } from "@/lib/api";
import Nav from "@/components/nav";

export default function SettingsPage() {
  const [tenant, setTenant] = useState<Tenant | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!api.isAuthenticated()) {
      window.location.href = "/login";
      return;
    }

    Promise.all([api.getTenant(), api.getMembers()])
      .then(([t, m]) => {
        setTenant(t);
        setMembers(m || []);
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

  return (
    <div className="flex min-h-screen">
      <Nav />
      <main className="ml-60 flex-1 p-8">
        <div className="mb-6">
          <h1 className="text-2xl font-bold">Settings</h1>
          <p className="mt-1 text-sm text-zinc-400">
            Tenant configuration and members
          </p>
        </div>

        {tenant && (
          <div className="mb-6 rounded-xl border border-zinc-800 bg-zinc-900/50 p-5">
            <h2 className="mb-4 text-sm font-semibold">Tenant Info</h2>
            <div className="grid grid-cols-3 gap-6">
              <div>
                <p className="text-xs text-zinc-500">Name</p>
                <p className="mt-1 text-sm font-medium">{tenant.name}</p>
              </div>
              <div>
                <p className="text-xs text-zinc-500">Slug</p>
                <p className="mt-1 font-mono text-sm">{tenant.slug}</p>
              </div>
              <div>
                <p className="text-xs text-zinc-500">Plan</p>
                <p className="mt-1 text-sm">
                  <span className="inline-flex rounded-md border border-emerald-800 bg-emerald-950 px-2 py-0.5 text-xs font-medium text-emerald-400">
                    {tenant.plan}
                  </span>
                </p>
              </div>
            </div>
          </div>
        )}

        <div className="rounded-xl border border-zinc-800 bg-zinc-900/50">
          <div className="border-b border-zinc-800 px-5 py-4">
            <h2 className="text-sm font-semibold">
              Members ({members.length})
            </h2>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-left text-xs text-zinc-500">
                  <th className="px-5 py-3 font-medium">User</th>
                  <th className="px-5 py-3 font-medium">Role</th>
                  <th className="px-5 py-3 font-medium">Joined</th>
                </tr>
              </thead>
              <tbody>
                {members.length === 0 ? (
                  <tr>
                    <td colSpan={3} className="px-5 py-8 text-center text-zinc-500">
                      No members found.
                    </td>
                  </tr>
                ) : (
                  members.map((m) => (
                    <tr
                      key={m.id}
                      className="border-b border-zinc-800/50 transition hover:bg-zinc-800/30"
                    >
                      <td className="px-5 py-3">
                        <div className="flex items-center gap-3">
                          {m.avatar_url ? (
                            <img
                              src={m.avatar_url}
                              alt={m.username}
                              className="h-7 w-7 rounded-full"
                            />
                          ) : (
                            <div className="flex h-7 w-7 items-center justify-center rounded-full bg-zinc-700 text-xs font-medium">
                              {m.username[0]?.toUpperCase()}
                            </div>
                          )}
                          <div>
                            <p className="font-medium">{m.username}</p>
                            {m.email && (
                              <p className="text-xs text-zinc-500">{m.email}</p>
                            )}
                          </div>
                        </div>
                      </td>
                      <td className="px-5 py-3">
                        <span
                          className={`inline-flex rounded-md border px-2 py-0.5 text-xs font-medium ${
                            m.role === "owner"
                              ? "border-amber-800 bg-amber-950 text-amber-400"
                              : "border-zinc-700 bg-zinc-800 text-zinc-400"
                          }`}
                        >
                          {m.role}
                        </span>
                      </td>
                      <td className="px-5 py-3 text-zinc-500">
                        {new Date(m.joined_at).toLocaleDateString()}
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
