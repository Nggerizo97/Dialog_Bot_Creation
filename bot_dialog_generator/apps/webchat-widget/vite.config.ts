/// <reference types="vitest" />
import { fileURLToPath } from "node:url";
import { defineConfig, searchForWorkspaceRoot } from "vite";

// Tests read docs/legal/COOKIES_AND_STORAGE.md to keep it in step with the storage keys.
const legalDocs = fileURLToPath(new URL("../../docs/legal", import.meta.url));

export default defineConfig({
  build: {
    lib: {
      entry: "src/bot-dialog-generator-chat.ts",
      formats: ["es"],
      fileName: "bot-dialog-generator-chat",
    },
  },
  server: {
    fs: { allow: [searchForWorkspaceRoot(process.cwd()), legalDocs] },
  },
  test: {
    environment: "happy-dom",
    globals: true,
  },
});
