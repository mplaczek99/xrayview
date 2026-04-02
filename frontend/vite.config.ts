import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Tauri's native shell expects the dev server on a fixed port, so keep Vite
// deterministic instead of auto-selecting an alternative.
export default defineConfig({
  plugins: [react()],
  clearScreen: false,
  server: {
    port: 1420,
    strictPort: true,
  },
  envPrefix: ["VITE_", "TAURI_"],
  build: {
    target: ["es2022", "chrome105", "safari13"],
  },
});
