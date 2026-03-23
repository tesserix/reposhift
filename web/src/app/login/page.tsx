"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { Button } from "@/components/button";
import { Input } from "@/components/input";

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
    <div className="flex min-h-screen items-center justify-center px-4">
      <div className="w-full max-w-sm">
        {/* Logo + Header */}
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-10 w-10 items-center justify-center rounded-xl bg-primary">
            <svg className="h-5 w-5 text-primary-foreground" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M7.5 21L3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" />
            </svg>
          </div>
          <h1 className="text-xl font-semibold text-foreground">
            Welcome back
          </h1>
          <p className="mt-1.5 text-sm text-muted-foreground">
            Enter your admin token to continue
          </p>
        </div>

        {/* Error */}
        {error && (
          <div className="mb-4 rounded-lg border border-red-800/60 bg-red-950/50 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {/* Form */}
        <form onSubmit={handleSubmit} className="space-y-4">
          <Input
            label="Admin Token"
            type="password"
            value={adminToken}
            onChange={(e) => setAdminToken(e.target.value)}
            placeholder="Enter your admin token"
            autoComplete="off"
          />
          <Button
            type="submit"
            disabled={submitting}
            className="w-full"
            size="lg"
          >
            {submitting ? "Verifying..." : "Sign in"}
          </Button>
        </form>

        {/* Footer */}
        <p className="mt-8 text-center text-xs text-muted-foreground">
          Open-source migration platform
        </p>
      </div>
    </div>
  );
}
