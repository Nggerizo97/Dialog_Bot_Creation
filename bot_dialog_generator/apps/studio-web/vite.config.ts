/// <reference types="vitest" />
import { existsSync, readFileSync } from "node:fs";
import { Agent } from "node:https";
import { createRequire } from "node:module";
import { homedir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig, searchForWorkspaceRoot, type Plugin, type ProxyOptions } from "vite";
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

// Local Cognito (docs/cognito-setup.md). The AWS Emulator has no CORS and serves HTTPS
// with its own certificate, so the dev server forwards /__cognito to it and trusts that
// certificate; TLS verification stays on. The emulator makes a new certificate each time
// it starts: restart the dev server after restarting the emulator.
function cognitoProxy(): Record<string, ProxyOptions> {
  const target = process.env.COGNITO_EMULATOR_URL ?? "https://127.0.0.1:9297";
  const port = new URL(target).port || "443";
  const caFile = process.env.AWS_CA_BUNDLE ?? join(homedir(), ".aws", "emulator", port, "ca.pem");
  if (!existsSync(caFile)) return {};
  return {
    "/__cognito": {
      target,
      changeOrigin: true,
      agent: new Agent({ ca: readFileSync(caFile) }),
      rewrite: (path) => path.replace(/^\/__cognito/, "") || "/",
    },
  };
}

export default defineConfig({
  plugins: [react(), fontLicenses()],
  server: {
    fs: { allow: [searchForWorkspaceRoot(process.cwd()), legalDocs] },
    proxy: cognitoProxy(),
  },
  test: {
    environment: "happy-dom",
    globals: true,
    // Vitest blanks CSS by default; contrast.test.ts needs the real stylesheet.
    css: { include: [/styles\.css/] },
  },
});
