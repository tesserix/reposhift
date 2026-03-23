import type { NextConfig } from "next";

const platformApiUrl =
  process.env.PLATFORM_API_URL || "http://localhost:8090";

const nextConfig: NextConfig = {
  output: "standalone",
  async rewrites() {
    return [
      {
        source: "/api/platform/:path*",
        destination: `${platformApiUrl}/platform/:path*`,
      },
    ];
  },
};

export default nextConfig;
