import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// In development the Go server runs on :8080 and Vite proxies API/boot routes.
const backend = process.env.IPXEMULTIBOOT_BACKEND ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": { target: backend, changeOrigin: false },
      "/boot.ipxe": backend,
      "/menu.ipxe": backend,
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    chunkSizeWarningLimit: 800,
  },
});
