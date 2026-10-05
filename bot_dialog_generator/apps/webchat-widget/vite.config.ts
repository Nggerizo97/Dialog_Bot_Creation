/// <reference types="vitest" />
import { defineConfig } from "vite";

export default defineConfig({
  build: {
    lib: {
      entry: "src/bot-dialog-generator-chat.ts",
      formats: ["es"],
      fileName: "bot-dialog-generator-chat",
    },
  },
  test: {
    environment: "happy-dom",
    globals: true,
  },
});