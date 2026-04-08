import { fileURLToPath, URL } from "node:url";
import { defineConfig, mergeConfig } from "vite";
import baseConfig from "./vite.config";

export default mergeConfig(
  baseConfig,
  defineConfig({
    build: {
      outDir: fileURLToPath(new URL("../desktop/build/frontend/dist", import.meta.url)),
      emptyOutDir: true,
      rollupOptions: {
        input: fileURLToPath(new URL("./index.html", import.meta.url)),
      },
    },
  }),
);
