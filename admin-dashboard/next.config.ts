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
  // basePath is auto-prepended to source, so use /api/ not /admin/api/
  ...(!isProd && {
    async rewrites() {
      return [
        {
          source: "/api/:path*",
          destination: "http://localhost:8080/admin/api/:path*",
        },
      ];
    },
  }),
};

export default nextConfig;
