# Set up Microsoft Entra ID sign-in

This guide connects the studio to Microsoft Entra ID, so staff sign in with their work account and see only the areas their groups give them. It takes about 20 minutes in the [Microsoft Entra admin center](https://entra.microsoft.com). Menu names can move slightly as Microsoft updates the portal.

**What you need:** an Entra tenant where you can register apps and create groups. Restricting access to groups assigned to the app (step 4) needs **Entra ID P1 or P2**; a P2 trial is enough for testing. Without P1, see the alternative in step 4.

**What you will copy back:** the *Directory (tenant) ID*, the *Application (client) ID* and group *Object IDs*. None of these is a secret: the studio uses the Authorization Code flow with PKCE, which needs no client secret.

## 1. Register the studio app

1. Go to **Identity → Applications → App registrations → New registration**.
2. **Name:** `Bot_Dialog_Generator Studio`.
3. **Supported account types:** *Accounts in this organizational directory only (single tenant)*.
4. **Redirect URI:** platform **Single-page application (SPA)**, value `http://localhost:5175/`. Include the final `/`, because Entra compares redirect addresses exactly. Add your production address later under **Authentication**.
5. Select **Register**, then copy the **Application (client) ID** and the **Directory (tenant) ID** from **Overview**.

## 2. Expose the studio API

The studio calls studio-api with an access token issued for this same app.

1. Open **Expose an API**. Next to **Application ID URI**, select **Add** and keep the default `api://<client-id>`.
2. Select **Add a scope**:
   - Scope name: `access_as_user`
   - Who can consent: **Admins and users**
   - Admin consent display name: `Use the Bot_Dialog_Generator studio`
   - State: **Enabled**
3. Open **API permissions → Add a permission → APIs my organization uses** (or **My APIs**), choose the app, tick `access_as_user` and add it. Then select **Grant admin consent** so staff are not asked to consent.
4. Open **Manifest** and set the access token version to 2, so tokens carry the issuer and audience studio-api expects:
   - Microsoft Graph format: `"api": { "requestedAccessTokenVersion": 2 }`
   - Older AAD Graph format: `"accessTokenAcceptedVersion": 2`

## 3. Put groups in the token

1. Open **Token configuration → Add groups claim**.
2. Select **Groups assigned to the application**. Then the token lists only the studio's groups, stays small and never hits Entra's 200-group limit.
3. Under **Access**, choose **Group ID**. Save.

## 4. Decide who can sign in

1. Go to **Identity → Applications → Enterprise applications** and open `Bot_Dialog_Generator Studio`.
2. **Properties → Assignment required? → Yes**. Only people in assigned groups can sign in.
3. **Users and groups → Add user/group**, and assign each group from step 5.

**Without Entra ID P1:** groups cannot be assigned to apps. In step 3 choose **Security groups** instead, and skip the assignment. Anyone in the tenant can then sign in, but sees nothing until an area grants them access. People in more than 200 groups will see the message "Your account is in too many groups for the sign-in token".

## 5. Create the groups

Go to **Identity → Groups → New group**, type **Security**:

| Group | Purpose | Where its Object ID goes |
|---|---|---|
| `BDG Platform Admins` | Sees every area and the audit log | `PLATFORM_ADMIN_GROUP` in studio-api |
| One per area and role, e.g. `BDG Legal editors`, `BDG HR owners` | Access to one area | The studio's **Admin → Create area** form, or an area's **Members** tab |

Copy each group's **Object ID** from its **Overview** page. To add an individual person to an area instead of a group, use their **Object ID** from **Identity → Users → (person) → Overview**.

## 6. Configure the studio

Create `apps/studio-web/.env.local`. It is ignored by git; see `.env.example`:

```bash
VITE_OIDC_AUTHORITY=https://login.microsoftonline.com/<tenant-id>/v2.0
VITE_OIDC_CLIENT_ID=<client-id>
VITE_OIDC_SCOPE=openid profile offline_access api://<client-id>/access_as_user
```

These values are compiled into the studio, so rebuild after changing them:

```bash
cd apps/studio-web && npx vite build && npx vite preview --port 5175
```

## 7. Configure studio-api

Start studio-api with these settings, from the `bot_dialog_generator` folder:

```bash
AUTH_MODE=oidc OIDC_ISSUER=https://login.microsoftonline.com/<tenant-id>/v2.0 OIDC_AUDIENCE=<client-id> OIDC_SUBJECT_CLAIM=oid PLATFORM_ADMIN_GROUP=<admins-group-object-id> CORS_ALLOWED_ORIGINS=http://localhost:5175 go run ./services/studio-api
```

| Setting | Why |
|---|---|
| `OIDC_ISSUER` | Entra's v2 issuer for your tenant; studio-api downloads its signing keys from there |
| `OIDC_AUDIENCE` | The client ID: v2 access tokens for your own API carry it in `aud` |
| `OIDC_SUBJECT_CLAIM=oid` | Identifies people by their object ID, which admins can look up. Entra's `sub` is different for every app |
| `OIDC_GROUPS_CLAIM` | Leave unset: Entra uses `groups` |
| `PLATFORM_ADMIN_GROUP` | Object ID of `BDG Platform Admins` |

## 8. First sign-in

1. Open http://localhost:5175 and select **Sign in with Microsoft**.
2. Sign in as a member of `BDG Platform Admins`. You land on the admin page.
3. Create your first area with its owner group's Object ID. Members of that group now see the area when they sign in.

The demo areas (Customer service, Human resources) still exist, because the store is seeded in memory until milestone 2. Their demo members (alice, bob…) never match real Entra users, so nobody sees them except platform admins.

## Troubleshooting

| What you see | Likely cause | Fix |
|---|---|---|
| `AADSTS50011` redirect URI mismatch | The address in the browser is not registered exactly | Add it under **Authentication → Single-page application**, including the final `/` |
| `AADSTS50105` user not assigned | Assignment is required and the person is not in an assigned group | Add the person to a group assigned in step 4 |
| `AADSTS65001` consent required | Admin consent was not granted | Step 2.3: **Grant admin consent** |
| Signed in, then sent back to sign-in | studio-api rejected the token (401) | Check that `OIDC_AUDIENCE` is the client ID and the issuer ends in `/v2.0`. If the token's issuer starts with `https://sts.windows.net/`, it is a v1 token: redo step 2.4 |
| Signed in, but no areas | The token has no groups, or the groups have no area | Check step 3; then grant the group an area in the studio |
| "Too many groups for the sign-in token" | The groups claim lists all groups and the person has more than 200 | Use **Groups assigned to the application** (step 3) |

To inspect a token, paste it into https://jwt.ms (Microsoft's decoder, which runs in your browser): check `iss`, `aud`, `oid`, `scp` and `groups`.
