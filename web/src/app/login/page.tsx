"use client";

import { useEffect, useState } from "react";
import { api, type PlatformMode } from "@/lib/api";

export default function LoginPage() {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [mode, setMode] = useState<PlatformMode | null>(null);
  const [adminToken, setAdminToken] = useState("");
  const [tokenSubmitting, setTokenSubmitting] = useState(false);

  // Detect platform mode on mount
  useEffect(() => {
    api.getMode().then(setMode).catch(() => {
      // Fallback: assume saas with GitHub OAuth
      setMode({ mode: "saas", githubOAuthEnabled: true });
    });
  }, []);

  // Handle GitHub OAuth callback (?code=...&state=...)
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const code = params.get("code");
    const state = params.get("state");

    if (code && state) {
      setLoading(true);
      api
        .getToken(code, state)
        .then(() => {
          window.location.href = "/";
        })
        .catch((err) => {
          setError(err.message || "Authentication failed");
          setLoading(false);
        });
    } else if (api.isAuthenticated()) {
      window.location.href = "/";
    }
  }, []);

  const handleAdminTokenSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!adminToken.trim()) {
      setError("Please enter an admin token");
      return;
    }

    setTokenSubmitting(true);
    setError(null);

    // Validate the token by making an authenticated request directly
    // (avoid using api.request which auto-redirects on 401)
    try {
      const res = await fetch("/api/platform/v1/dashboard/stats", {
        headers: { Authorization: `Bearer ${adminToken.trim()}` },
      });

      if (!res.ok) {
        setError("Invalid admin token. Please check and try again.");
        setTokenSubmitting(false);
        return;
      }

      // Token is valid — store it and redirect
      api.loginWithAdminToken(adminToken.trim());
      window.location.href = "/";
    } catch {
      setError("Unable to reach the server. Please try again.");
      setTokenSubmitting(false);
    }
  };

  const showGitHub = mode?.githubOAuthEnabled || mode?.mode === "saas";
  const showToken = mode?.mode === "selfhosted" || (mode !== null && !mode.githubOAuthEnabled);
  // If both OAuth and self-hosted are configured, show both
  const showBoth = showGitHub && showToken;

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="w-full max-w-sm space-y-8 text-center">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-zinc-50">
            Reposhift
          </h1>
          <p className="mt-2 text-sm text-zinc-400">
            ADO to GitHub migration platform
          </p>
        </div>

        {error && (
          <div className="rounded-lg border border-red-800 bg-red-950/50 px-4 py-3 text-sm text-red-300">
            {error}
          </div>
        )}

        {!mode ? (
          <div className="text-sm text-zinc-400">Loading...</div>
        ) : loading ? (
          <div className="text-sm text-zinc-400">Authenticating...</div>
        ) : (
          <div className="space-y-6">
            {/* GitHub OAuth button */}
            {showGitHub && (
              <button
                onClick={() => api.login()}
                className="flex w-full items-center justify-center gap-3 rounded-lg bg-zinc-50 px-6 py-3 text-sm font-semibold text-zinc-900 transition hover:bg-zinc-200"
              >
                <svg
                  className="h-5 w-5"
                  fill="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
                </svg>
                Sign in with GitHub
              </button>
            )}

            {/* Divider when both options are shown */}
            {showBoth && (
              <div className="flex items-center gap-4">
                <div className="h-px flex-1 bg-zinc-700" />
                <span className="text-xs text-zinc-500">or</span>
                <div className="h-px flex-1 bg-zinc-700" />
              </div>
            )}

            {/* Admin token form */}
            {showToken && (
              <form onSubmit={handleAdminTokenSubmit} className="space-y-4">
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
                  disabled={tokenSubmitting}
                  className="flex w-full items-center justify-center rounded-lg bg-zinc-50 px-6 py-3 text-sm font-semibold text-zinc-900 transition hover:bg-zinc-200 disabled:opacity-50"
                >
                  {tokenSubmitting ? "Verifying..." : "Sign in with Token"}
                </button>
              </form>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
