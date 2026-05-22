import { defineConfig } from "vite";

// The browser/mock workflow uses a fixed port so desktop-facing dev helpers can
// target it deterministically when needed.
export default defineConfig({
  clearScreen: false,
  server: {
    host: "127.0.0.1",
    port: 1420,
    strictPort: true,
  },
  envPrefix: ["VITE_"],
  build: {
    target: ["es2022", "chrome105", "safari13"],
    cssCodeSplit: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("node_modules/htmx.org")) {
            return "vendor";
          }
        },
      },
    },
  },
});
