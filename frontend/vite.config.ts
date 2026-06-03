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
    // Tauri ships a Chromium WebView (WebView2) on Windows and a WebKit WebView
    // (WebKitGTK) on Linux — both modern, evergreen engines — and macOS is not a
    // build target (see .github/workflows). Target each engine's real floor instead
    // of the old `safari13` baseline, which forced esbuild to down-level class
    // fields, logical-assignment, and optional chaining that every shipped WebView
    // runs natively. Tauri sets TAURI_ENV_PLATFORM during its build; a plain
    // `vite build` with no env falls back to the safe WebKit floor.
    target: process.env.TAURI_ENV_PLATFORM === "windows" ? "chrome105" : "safari15",
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
