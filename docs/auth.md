---
title: Authentication
---

# Authentication

[Back to index](index.md)

BabelSuite supports two login modes: local email/password and OIDC single sign-on. Both produce a local JWT — the rest of the application sees the same session either way.

---

## Local Authentication

### Setup

Seed the initial admin account with environment variables:

```bash
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=changeme
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ADMIN_EMAIL` | — | Email for the seeded admin account |
| `ADMIN_PASSWORD` | — | Password for the seeded admin account |
| `AUTH_PASSWORD_LOGIN_ENABLED` | `true` | Show the password login form |
| `AUTH_SIGNUP_ENABLED` | `true` | Allow new users to self-register |

---

## OIDC Single Sign-On

### How It Works

1. User clicks **Sign in with SSO** — the frontend calls `GET /api/v1/auth/oidc/login`.
2. The backend generates a PKCE verifier and a state cookie, then redirects the browser to the provider's authorization endpoint.
3. The provider authenticates the user and redirects back to `GET /api/v1/auth/oidc/callback`.
4. The backend verifies the state cookie and exchanges the authorization code for tokens using the PKCE verifier.
5. The backend reads identity claims (`email`, `name`, and optionally `groups`) from the ID token.
6. If the user belongs to a group listed in `OIDC_ADMIN_GROUPS`, the session is issued with admin privileges.
7. The backend issues a local JWT and redirects the browser to `OIDC_FRONTEND_CALLBACK_URL` with the token.

### Example Configuration

```bash
OIDC_ENABLED=true
OIDC_PROVIDER_ID=okta
OIDC_PROVIDER_NAME=Okta
OIDC_ISSUER_URL=https://your-org.okta.com
OIDC_CLIENT_ID=0oa...
OIDC_CLIENT_SECRET=...
OIDC_REDIRECT_URL=http://localhost:8090/api/v1/auth/oidc/callback
OIDC_FRONTEND_CALLBACK_URL=http://localhost:5173/auth/callback
OIDC_SCOPES=openid email profile
OIDC_PKCE_ENABLED=true
OIDC_EMAIL_CLAIM=email
OIDC_NAME_CLAIM=name
OIDC_GROUPS_CLAIM=groups
OIDC_ADMIN_GROUPS=platform-admins,devops
AUTH_STATE_SECRET=random-32-char-string
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `OIDC_ENABLED` | `false` | Enable OIDC login |
| `OIDC_PROVIDER_ID` | — | Internal identifier for the provider |
| `OIDC_PROVIDER_NAME` | — | Display name shown on the sign-in button |
| `OIDC_ISSUER_URL` | — | OIDC discovery URL (without `/.well-known/...`) |
| `OIDC_CLIENT_ID` | — | Client ID registered with the provider |
| `OIDC_CLIENT_SECRET` | — | Client secret registered with the provider |
| `OIDC_REDIRECT_URL` | — | Backend callback URL — must match the provider |
| `OIDC_FRONTEND_CALLBACK_URL` | — | Frontend route that receives the token after login |
| `OIDC_SCOPES` | `openid email profile` | Space-separated scopes to request |
| `OIDC_PKCE_ENABLED` | `true` | Use PKCE (recommended) |
| `OIDC_EMAIL_CLAIM` | `email` | Token claim for the user's email |
| `OIDC_NAME_CLAIM` | `name` | Token claim for the user's display name |
| `OIDC_GROUPS_CLAIM` | `groups` | Token claim that lists the user's group memberships |
| `OIDC_ADMIN_GROUPS` | — | Comma-separated list of groups that grant admin access |
| `AUTH_STATE_SECRET` | — | Secret used to sign the OIDC state cookie |

!!! note
    `OIDC_REDIRECT_URL` is the backend callback — it must be registered in your identity provider's allowed redirect URIs. `OIDC_FRONTEND_CALLBACK_URL` is where the browser lands after the backend finishes — it must match the frontend's `/auth/callback` route.

---

## Session Model

After successful login (either mode), the backend issues a signed JWT. All subsequent requests present this token in the `Authorization: Bearer` header.

Protected route middleware:

- Verifies the JWT signature and expiry
- Populates request context with the caller's identity and role
- Accepts the token as a query parameter (`?token=...`) for SSE streaming endpoints, where headers cannot be set by the browser

---

## API Endpoints

| Method | Path | Auth required | Description |
|--------|------|--------------|-------------|
| `GET` | `/api/v1/auth/config` | No | Returns enabled login modes |
| `POST` | `/api/v1/auth/sign-up` | No | Register a new local user |
| `POST` | `/api/v1/auth/sign-in` | No | Sign in with email and password |
| `GET` | `/api/v1/auth/sso/providers` | No | List configured SSO providers |
| `GET` | `/api/v1/auth/oidc/login` | No | Start the OIDC login flow |
| `GET` | `/api/v1/auth/oidc/callback` | No | OIDC provider callback |
| `GET` | `/api/v1/auth/me` | Yes | Return the current session's user record |

---

## Frontend Routes

| Route | Description |
|-------|-------------|
| `/sign-in` | Login page — shows local form and/or SSO button based on config |
| `/sign-up` | Registration page (visible when `AUTH_SIGNUP_ENABLED=true`) |
| `/auth/callback` | Receives the token from the OIDC backend redirect |

---

## Common Issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| Redirect loop after SSO | `OIDC_FRONTEND_CALLBACK_URL` points to backend instead of frontend | Set it to your frontend URL (`http://localhost:5173/auth/callback`) |
| `state mismatch` error | `AUTH_STATE_SECRET` changed between login start and callback | Use a stable, persistent secret |
| Admin rights not granted after SSO | Group claim not in token, or claim name wrong | Check `OIDC_GROUPS_CLAIM` and `OIDC_ADMIN_GROUPS` match your provider's token |
| Sign-up page hidden | `AUTH_SIGNUP_ENABLED=false` | Enable it, or create accounts via the admin API |

---

## See Also

- [Configuration](configuration.md) — full environment variable reference
- [API](api.md) — all route definitions
- [Platform Settings](platform.md) — secrets management
