import type { NextConfig } from "next";

const isProd = process.env.NODE_ENV === "production";

const nextConfig: NextConfig = {
  ...(isProd && { output: "export" }),
  basePath: "/admin",
  trailingSlash: true,
  images: {
    unoptimized: true,
  },
  // Dev only: proxy API calls to the Go backend
  async rewrites() {
    return [
      {
        source: "/admin/api/:path*",
        destination: "http://localhost:8080/admin/api/:path*",
      },
    ];
  },
};

export default nextConfig;
