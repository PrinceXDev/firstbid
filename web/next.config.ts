import type { NextConfig } from "next";

// Static export: `next build` emits plain files that the Go binary embeds.
// Node is a build-time tool only; nothing JavaScript runs at serve time.
// Output lands in ./out and is copied into the Go binary by `npm run embed`.
const nextConfig: NextConfig = {
  output: "export",
  images: { unoptimized: true },
  trailingSlash: true,
  reactStrictMode: true,
};

export default nextConfig;
