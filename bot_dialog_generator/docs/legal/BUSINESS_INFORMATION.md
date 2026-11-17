<!--
Fill these values once and set the same ones in the studio build (the VITE_LEGAL_*
variables in apps/studio-web/.env.example), which prints them in the footer and on
the "Business and legal contact" page. Until they are set, the studio shows the
placeholder names, never invented values.
-->

# Business and legal contact

| | |
|---|---|
| **Legal entity** | [[COMPANY_LEGAL_NAME]] |
| **Tax ID** | [[TAX_ID]] (NIT in Colombia) |
| **Registered address** | [[REGISTERED_ADDRESS]] |
| **Support** | [[SUPPORT_EMAIL]] · [[PHONE]] |
| **Privacy requests** | [[PRIVACY_EMAIL]] |
| **Data Protection Officer** | [[DPO_CONTACT]] |
| **Security incidents** | [[SECURITY_CONTACT]] |
| **Supervisory authority** | [[SUPERVISORY_AUTHORITY]] (Superintendencia de Industria y Comercio in Colombia) |

## Where this information appears

| Place | How |
|---|---|
| Studio footer, every page including sign-in | Legal entity, tax ID and links to every document below, from `VITE_LEGAL_*` |
| Studio page "Business and legal contact" | The full table above, from `VITE_LEGAL_*` |
| Chat widget | Footer and **Chat options** menu link the Privacy Policy, Terms of Use and Cookies pages (`privacy-url`, `terms-url`, `cookies-url`) |
| Campaign emails | Every email ends with the legal name and postal address (`SENDER_LEGAL_NAME`, `SENDER_POSTAL_ADDRESS`); the campaigns service refuses to start without them |

## Legal documents

- [Privacy Policy](PRIVACY_POLICY.md)
- [Terms of Use](TERMS_OF_USE.md)
- [Refund, Cancellation and Withdrawal Policy](REFUND_POLICY.md)
- [Cookies and browser storage](COOKIES_AND_STORAGE.md)
- [Third-party software and services](THIRD_PARTY_INVENTORY.md)
- [Licenses and assets](LICENSES_AND_ASSETS.md)
