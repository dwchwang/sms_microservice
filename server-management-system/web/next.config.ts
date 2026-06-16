import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Standalone output for a minimal Docker runtime image.
  output: "standalone",
};

export default nextConfig;
