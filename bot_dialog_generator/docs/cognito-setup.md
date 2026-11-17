# Sign-in with Amazon Cognito

The studio can sign people in with an Amazon Cognito user pool instead of Microsoft Entra ID ([entra-setup.md](entra-setup.md)). Access works the same way: a person sees the areas where they are a member, or where one of their Cognito **groups** has a role, and members of the `bdg-platform-admins` group are platform admins.

There are two setups:

| | Local (AWS Emulator) | Production (real user pool) |
|---|---|---|
| Studio sign-in | Username and password form, sent to the emulator through the Vite dev server | Cognito managed login (Authorization Code + PKCE) |
| studio-api | `AUTH_MODE=cognito`, pointed at the emulator's issuer | `AUTH_MODE=cognito`, pointed at your pool |
| Users and groups | Created by `go run ./tools/cognito-local` | Managed by IT in the user pool |

## How studio-api checks a Cognito token

`AUTH_MODE=cognito` (`libs/go/platform/auth/cognito.go`) accepts a request only when its bearer token:

1. is signed by a key from the pool's JWKS (`<issuer>/.well-known/jwks.json`), with the pool's issuer and an expiry in the future;
2. is an **access token** (`token_use` = `access`). ID tokens are refused with "Send the Cognito access token, not the ID token.";
3. was issued to one of the app clients in `COGNITO_CLIENT_IDS` (`client_id`). Cognito access tokens have no `aud` claim, which is why the generic `AUTH_MODE=oidc` cannot be used with Cognito.

Groups come from `cognito:groups`. The person is identified by `sub` (a UUID that never changes) or, with `OIDC_SUBJECT_CLAIM=username`, by their sign-in name.

| Variable | Meaning |
|---|---|
| `AUTH_MODE` | `cognito` |
| `COGNITO_USER_POOL_ID` | For example `us-east-1_AbCdEf123` |
| `COGNITO_REGION` | The pool's region (default `AWS_REGION`) |
| `COGNITO_CLIENT_IDS` | The studio's app client ID (comma-separated if there are several) |
| `COGNITO_ISSUER` | Only for the AWS Emulator: `https://127.0.0.1:9297/<pool-id>`. On AWS the issuer is `https://cognito-idp.<region>.amazonaws.com/<pool-id>` |
| `OIDC_SUBJECT_CLAIM` | `sub` (default) or `username` |
| `OIDC_CA_FILE` | Extra certificate to trust, for the emulator's `ca.pem` (default `AWS_CA_BUNDLE`). TLS verification is never turned off |

---

## Local: Cognito in the AWS Emulator

The [AWS Emulator](https://github.com/aws/aws-emulator) runs AWS APIs on your machine, including Cognito user pools. Its user pools issue real signed tokens and serve the discovery and JWKS endpoints, so studio-api verifies them exactly as it would on AWS. What the emulator does **not** have is Cognito's hosted sign-in page, and it sends no CORS headers; that is why the studio uses a password form locally and the Vite dev server forwards it.

> **Use demo users only.** The emulator is for building and testing. Don't put real staff accounts, passwords or data in it, keep it reachable only from your own machine, and don't use it to test security: it doesn't enforce AWS permissions the way AWS does (AWS Emulator User Guide, "Security").

### 1. Install and start the emulator

**Linux** (from the guide's "Getting started on Linux"; check the package signature as described there):

```bash
curl -fsSLo aws-emulator.deb https://downloads.aws.com/aws-emulator/packages/aws-emulator/LTS/latest/aws-emulator_amd64.deb
sudo apt-get install -y ./aws-emulator.deb
EMULATOR_TELEMETRY=off aws-emulator --telemetry off
```

**Windows with WSL:** use the Linux steps above inside your WSL terminal, and run the bootstrap tool, studio-api and the studio there too, so they share `~/.aws/emulator/`. Clone the repository into the WSL home folder (for example `~/code`) rather than `/mnt/c/...`: it is much faster, and it avoids the OneDrive `EPERM` problem with Vite. `podman compose` inside WSL hands off to Docker Desktop's `docker-compose`, which only works after you turn on the distribution under Docker Desktop → Settings → Resources → WSL integration.

**Windows, macOS, or Linux with Docker or Podman** (the guide's "Getting started with Docker"; the image is built from `infra/local/aws-emulator/Dockerfile`):

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\.aws\emulator"
$env:EMULATOR_DIR = "$env:USERPROFILE\.aws\emulator"
podman compose -f infra/local/compose.yml up -d --build aws-emulator
```

On macOS and Linux, run `mkdir -p ~/.aws/emulator` first and leave out `EMULATOR_DIR`. Telemetry is off in both cases; the emulator otherwise sends anonymous usage data to AWS.

The emulator listens on `https://127.0.0.1:9297` and writes its credentials, config and certificate to `~/.aws/emulator/9297/` (`%USERPROFILE%\.aws\emulator\9297\` on Windows). It keeps everything in memory: **when it stops, the user pool is gone.**

### 2–4 in one step (bash, Linux, macOS, WSL)

Keep the emulator in a terminal of its own. In a **second** terminal, from `bot_dialog_generator`:

```bash
source infra/local/cognito-env.sh
go run ./services/studio-api
```

The script checks that the emulator is running, clears any real AWS credentials from that terminal, points the AWS settings at the emulator, creates the pool and demo users, writes the studio's settings, and sets studio-api's (`AUTH_MODE=cognito` and the rest). It must be run with `source`, so the settings stay in that terminal. Then, in a **third** terminal, run `npm --prefix apps/studio-web run dev:cognito` and sign in at http://localhost:5173. The steps below do the same by hand, and are the way to go in PowerShell.

### 2. Point your shell at the emulator

In the terminal where you run the bootstrap tool and studio-api (from the guide's "Point your SDK at the emulator"):

```bash
export AWS_ENDPOINT_URL=https://127.0.0.1:9297
export AWS_SHARED_CREDENTIALS_FILE=~/.aws/emulator/9297/emulator_credentials
export AWS_CONFIG_FILE=~/.aws/emulator/9297/config
export AWS_CA_BUNDLE=~/.aws/emulator/9297/ca.pem
export AWS_EC2_METADATA_DISABLED=true
export AWS_REGION=us-east-1
unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE
```

```powershell
$env:AWS_ENDPOINT_URL = "https://127.0.0.1:9297"
$env:AWS_SHARED_CREDENTIALS_FILE = "$env:USERPROFILE\.aws\emulator\9297\emulator_credentials"
$env:AWS_CONFIG_FILE = "$env:USERPROFILE\.aws\emulator\9297\config"
$env:AWS_CA_BUNDLE = "$env:USERPROFILE\.aws\emulator\9297\ca.pem"
$env:AWS_EC2_METADATA_DISABLED = "true"
$env:AWS_REGION = "us-east-1"
Remove-Item Env:AWS_ACCESS_KEY_ID, Env:AWS_SECRET_ACCESS_KEY, Env:AWS_SESSION_TOKEN, Env:AWS_PROFILE -ErrorAction SilentlyContinue
```

Unsetting the `AWS_ACCESS_KEY_ID` family matters: real credentials in the environment win over the emulator's file, and the emulator then rejects every request.

### 3. Create the pool, groups and demo users

```powershell
go run ./tools/cognito-local -write-env
```

This creates the `bdg-local` user pool, the studio's app client, the `bdg-platform-admins` and `hr-team` groups, and the demo users `alice`, `erin`, `carol`, `bob`, `frank` (in `hr-team`) and `dana` (in `bdg-platform-admins`), all with the password `Demo-pass-2026` (change it with `-password`). It signs in once to check that everything works, prints the settings for studio-api in bash and PowerShell, and writes `apps/studio-web/.env.cognito.local`. Running it again reuses what exists.

The tool refuses to run unless `AWS_ENDPOINT_URL` is on `127.0.0.1`, so it can never create demo users with a known password in a real AWS account.

### 4. Start studio-api and the studio

In the same terminal, paste the `studio-api` lines the tool printed, then:

```powershell
go run ./services/studio-api
```

The startup line says `auth: cognito`. In another terminal:

```powershell
npm --prefix apps/studio-web run dev:cognito
```

Open http://localhost:5173, tick the terms box, and sign in as `alice` / `Demo-pass-2026`. Each demo user sees the same areas as with the development sign-in, because studio-api's demo data names them by username (`OIDC_SUBJECT_CLAIM=username`) and grants `hr-team` the editor role in Human resources.

**When the emulator restarts**, run step 3 again, update studio-api's settings, and restart the dev server: the pool ID changes and the emulator makes a new certificate.

### Checking it from the terminal

```bash
curl --cacert ~/.aws/emulator/9297/ca.pem https://127.0.0.1:9297/__admin/health
go test ./tools/cognito-local/                          # unit tests
COGNITO_EMULATOR_TEST=1 go test ./tools/cognito-local/  # also signs every demo user in and verifies their tokens
```

### Troubleshooting

| Message | Fix |
|---|---|
| "the AWS Emulator is not running: its certificate … is missing" | The emulator stopped (it deletes `ca.pem` when it does). Start it again in a terminal of its own and leave that terminal open, then repeat the steps |
| studio-api says `auth: dev` | The Cognito settings are not set in that terminal: use `source infra/local/cognito-env.sh`, or paste the `export` lines the tool printed, before `go run ./services/studio-api` |
| The dev server prints `[cognito] No AWS Emulator certificate…`, or sign-in says "Can't reach Cognito" | Start the emulator first, then restart `npm run dev:cognito` |
| `AccessDenied: Sign the request with the emulator credentials` | Step 2: set the credential and config files and unset `AWS_ACCESS_KEY_ID` and the others |
| `certificate signed by unknown authority` (studio-api or the tool) | Set `AWS_CA_BUNDLE` or `OIDC_CA_FILE` to the emulator's `ca.pem` |
| Studio: "Incorrect username or password." | Use a demo user and the password the tool printed |
| studio-api: 401 "Send the Cognito access token, not the ID token." | Send `AccessToken` from the sign-in result, not `IdToken` |
| `emulator serve failed: … Address already in use` | Another emulator is running; stop it or start with `-p 9300` and use that port everywhere |

---

## Production: a real Cognito user pool

1. **User pool.** Create a user pool for staff. Turn off self sign-up (only administrators create users), require MFA if your policy asks for it, and set the password policy.
2. **Groups.** Create `bdg-platform-admins` for platform administrators, and one group per area team (for example `legal-editors`). In the studio, owners grant a group a role by its **group name**: type the Cognito group name where the Members page asks for the group ID (the labels still mention Entra ID).
3. **App client.** A public client (no client secret) with only the **Authorization code grant**, scopes `openid`, `email` and `profile`, callback and sign-out URLs set to the studio's address, and the refresh-token flow. **Do not enable `USER_PASSWORD_AUTH`** on this client: in production, passwords are typed only on Cognito's own sign-in page.
4. **Managed login.** Give the pool a domain (Cognito domain or your own) and turn on managed login for the app client.
5. **Studio** (`apps/studio-web/.env.local`):

   ```
   VITE_OIDC_AUTHORITY=https://cognito-idp.<region>.amazonaws.com/<pool-id>
   VITE_OIDC_CLIENT_ID=<app-client-id>
   VITE_OIDC_SCOPE=openid email profile
   VITE_OIDC_PROVIDER_NAME=<your company>
   ```

   The studio then shows **Sign in with &lt;your company&gt;** and uses the same OpenID Connect code (`companySignIn.ts`) as with Entra ID.
6. **studio-api:** `AUTH_MODE=cognito`, `COGNITO_REGION`, `COGNITO_USER_POOL_ID`, `COGNITO_CLIENT_IDS`. Keep `OIDC_SUBJECT_CLAIM=sub`: memberships then name people by their Cognito `sub`, which never changes and is never reused, unlike a username.

**Not verified yet:** the AWS Emulator has no managed login page, so the production browser flow (step 5) was not run end to end in this repository. Before rollout, sign in once against the real pool and check that `/me` lists the expected areas. Signing out of the studio ends the studio's session; Cognito's managed login keeps its own session cookie until it expires, unless you also send people to the domain's `/logout` endpoint.
