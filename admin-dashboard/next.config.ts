import type { NextConfig } from "next";

const isProd = process.env.NODE_ENV === "production";
// In dev: use mock API by default, set NEXT_PUBLIC_API_PROXY=true to proxy to Go backend
const useGoBackend = process.env.NEXT_PUBLIC_API_PROXY === "true";

const nextConfig: NextConfig = {
  ...(isProd && { output: "export" }),
  basePath: "/admin",
  trailingSlash: true,
  images: {
    unoptimized: true,
  },
  ...(!isProd && {
    async rewrites() {
      if (useGoBackend) {
        // Proxy to the Go backend at localhost:8080
        return [
          {
            source: "/api/:path*",
            destination: "http://localhost:8080/admin/api/:path*",
          },
        ];
      }

      // Default: use in-app mock API (no Go backend needed)
      return [
        {
          source: "/api/:path*",
          destination: "/mock-api/:path*",
        },
      ];
    },
  }),
};

export default nextConfig;
