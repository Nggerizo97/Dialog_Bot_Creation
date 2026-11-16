# Licenses and assets

Every font, icon and image the studio and the chat widget show, and the license it is used under. Audited on 16 Nov 2026 (`find apps -name '*.png' -o -name '*.svg' -o -name '*.woff*' …` outside `node_modules`, and the license file of every package below).

## Fonts

| Asset | Used in | Author | License | How it is shipped |
|---|---|---|---|---|
| Manrope (400–800) | Studio | The Manrope Project Authors (Mikhail Sharanda) | [SIL Open Font License 1.1](https://openfontlicense.org) | `@fontsource/manrope` 5.3.0, bundled into the build; served from our own origin |
| DM Mono (400, 500) | Studio | The DM Mono Project Authors (Colophon Foundry) | SIL Open Font License 1.1 | `@fontsource/dm-mono` 5.3.0, bundled into the build |
| System fonts (`-apple-system`, `Segoe UI`, `Roboto`, `sans-serif`) | Chat widget, unsubscribe page | The visitor's operating system | Not distributed by us | Not shipped |

The OFL requires the license to travel with the fonts. The studio build writes each font's `LICENSE` file to `dist/licenses/` (see `apps/studio-web/vite.config.ts`), so every deployment carries them. The fonts are not modified and are not sold on their own.

No font is loaded from Google Fonts or any other CDN, so opening the studio sends nobody's IP address to a font provider.

## Icons and images

| Asset | Used in | Source | License |
|---|---|---|---|
| Interface icons (Bot, GitBranch, Play, Save, Send, ShieldCheck, Trash2, Users, X, …) | Studio | `lucide-react` 0.468.0, Lucide Contributors; portions from Feather by Cole Bemis | ISC (Lucide), MIT (Feather portions) |
| Chat-bubble launcher icon | Chat widget | Drawn for this project (one SVG path in `bot-dialog-generator-chat.ts`) | Ours |
| "⋮" (options) and "×" (close) | Chat widget | Unicode characters rendered in the system font | Not a licensed asset |
| Photographs, illustrations, logos, avatars | — | None are used | — |

Every icon that carries meaning has a text label (`aria-label` on icon-only buttons); decorative icons are hidden from screen readers.

## Software in the shipped bundles

| Package | Version | License | Shipped in |
|---|---|---|---|
| react, react-dom, scheduler | 19.3.0 / 0.28.0 | MIT | Studio |
| oidc-client-ts | 3.5.0 | Apache-2.0 | Studio |
| jwt-decode | 4.0.0 | MIT | Studio (via oidc-client-ts) |
| lucide-react | 0.468.0 | ISC | Studio |
| @fontsource/manrope, @fontsource/dm-mono | 5.3.0 | OFL-1.1 | Studio |
| lit, lit-html, lit-element, @lit/reactive-element, @lit-labs/ssr-dom-shim | 3.3.3 / 3.3.3 / 4.2.2 / 2.1.2 / 1.6.0 | BSD-3-Clause | Chat widget |
| @bufbuild/protobuf | 2.16.0 | Apache-2.0 AND BSD-3-Clause | TypeScript contracts library |

## Go modules in the services

| Module | Version | License |
|---|---|---|
| github.com/coreos/go-oidc/v3 | 3.21.0 | Apache-2.0 |
| github.com/go-jose/go-jose/v4 | 4.1.5 | Apache-2.0 |
| github.com/jackc/pgx/v5, puddle/v2, pgpassfile, pgservicefile | 5.11.0 / 2.2.2 / 1.0.0 / 2024-06-06 | MIT |
| golang.org/x/oauth2, x/sync, x/text | 0.36.0 / 0.17.0 / 0.29.0 | BSD-3-Clause |
| google.golang.org/protobuf | 1.36.12 | BSD-3-Clause |

Build and test tools (Vite, TypeScript, Vitest, happy-dom, Testing Library, Playwright, Babel) are not shipped to users.

## Keeping this list true

Before adding a font, icon set, image or runtime package:

1. Use only assets under a license that allows commercial use and redistribution: SIL OFL, MIT, ISC, BSD, Apache-2.0, or CC0/CC BY with the attribution added here.
2. Bundle it; do not hot-link it from a CDN (see [THIRD_PARTY_INVENTORY.md](THIRD_PARTY_INVENTORY.md)).
3. Add a row to the tables above in the same pull request.
