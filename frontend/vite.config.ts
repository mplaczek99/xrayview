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
    // Wails ships a Chromium WebView (WebView2) on Windows and a WebKit WebView
    // (WebKitGTK) on Linux — both modern, evergreen engines — and macOS is not a
    // build target (see .github/workflows). Target each engine's real floor instead
    // of the old `safari13` baseline, which forced esbuild to down-level class
    // fields, logical-assignment, and optional chaining that every shipped WebView
    // runs natively. Windows builds run on Windows hosts; otherwise fall back to
    // the safe WebKit floor.
    target: process.platform === "win32" ? "chrome105" : "safari15",
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
