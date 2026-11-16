/// <reference types="vitest" />
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { defineConfig, searchForWorkspaceRoot, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

const require = createRequire(import.meta.url);

// The SIL Open Font License requires the license to travel with the fonts, so the
// build writes each bundled font's LICENSE next to the app (dist/licenses/).
// docs/legal/LICENSES_AND_ASSETS.md lists them.
const BUNDLED_FONTS = ["@fontsource/manrope", "@fontsource/dm-mono"];

function fontLicenses(): Plugin {
  return {
    name: "bdg-font-licenses",
    apply: "build",
    generateBundle() {
      for (const pkg of BUNDLED_FONTS) {
        const dir = require.resolve(`${pkg}/package.json`).replace(/package\.json$/, "");
        this.emitFile({
          type: "asset",
          fileName: `licenses/${pkg.replace("@fontsource/", "")}-OFL.txt`,
          source: readFileSync(`${dir}LICENSE`, "utf8"),
        });
      }
    },
  };
}

// The legal documents live in docs/legal and are rendered by the studio at #/legal/…
const legalDocs = fileURLToPath(new URL("../../docs/legal", import.meta.url));

export default defineConfig({
  plugins: [react(), fontLicenses()],
  server: {
    fs: { allow: [searchForWorkspaceRoot(process.cwd()), legalDocs] },
  },
  test: {
    environment: "happy-dom",
    globals: true,
    // Vitest blanks CSS by default; contrast.test.ts needs the real stylesheet.
    css: { include: [/styles\.css/] },
  },
});
