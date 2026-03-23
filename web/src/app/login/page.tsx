"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";

export default function LoginPage() {
  const [error, setError] = useState<string | null>(null);
  const [adminToken, setAdminToken] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (api.isAuthenticated()) {
      window.location.href = "/";
    }
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!adminToken.trim()) {
      setError("Please enter an admin token");
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      const res = await fetch("/api/platform/v1/dashboard/stats", {
        headers: { "X-Admin-Token": adminToken.trim() },
      });

      if (!res.ok) {
        setError("Invalid admin token. Please check and try again.");
        setSubmitting(false);
        return;
      }

      api.loginWithAdminToken(adminToken.trim());
      window.location.href = "/";
    } catch {
      setError("Unable to reach the server. Please try again.");
      setSubmitting(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="w-full max-w-sm space-y-8 text-center">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-zinc-50">
            Reposhift
          </h1>
          <p className="mt-2 text-sm text-zinc-400">
            Sign in to your migration dashboard
          </p>
        </div>

        {error && (
          <div className="rounded-lg border border-red-800 bg-red-950/50 px-4 py-3 text-sm text-red-300">
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="text-left">
            <label
              htmlFor="admin-token"
              className="block text-sm font-medium text-zinc-300"
            >
              Admin Token
            </label>
            <input
              id="admin-token"
              type="password"
              value={adminToken}
              onChange={(e) => setAdminToken(e.target.value)}
              placeholder="Enter your admin token"
              className="mt-1 block w-full rounded-lg border border-zinc-700 bg-zinc-800 px-4 py-3 text-sm text-zinc-50 placeholder-zinc-500 focus:border-zinc-500 focus:outline-none focus:ring-1 focus:ring-zinc-500"
              autoComplete="off"
            />
          </div>
          <button
            type="submit"
            disabled={submitting}
            className="flex w-full items-center justify-center rounded-lg bg-zinc-50 px-6 py-3 text-sm font-semibold text-zinc-900 transition hover:bg-zinc-200 disabled:opacity-50"
          >
            {submitting ? "Verifying..." : "Sign in"}
          </button>
        </form>
      </div>
    </div>
  );
}
