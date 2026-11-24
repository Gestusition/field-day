# Casdoor setup (local development)

Casdoor is the identity provider for people (web and mobile). Readers do not
use Casdoor; they use per-reader API keys.

1. `docker compose up -d postgres casdoor`
2. Open <http://localhost:8000> and sign in with Casdoor's default admin
   account (see the Casdoor docs), then **change that password immediately**.
3. Create an **organization**, e.g. `field-day`.
4. Create two **applications** in that organization:

   | Application | Client ID (set it explicitly) | Redirect URIs | Grant |
   |---|---|---|---|
   | Web | `field-day-web` | `http://localhost:5173/callback` | Authorization code + PKCE |
   | Mobile | `field-day-mobile` | the Expo/app scheme, e.g. `fieldday://callback` | Authorization code + PKCE |

   Token format: **JWT**. These client ids must match `OIDC_AUDIENCES`.
5. Create a **role** named `admin` (matches `OIDC_ADMIN_ROLE`) and add the
   admin users to it. The role list reaches the API in the `roles` claim.
6. Create test users. The first time a user calls `GET /api/me`, the API
   creates their local record. If an admin pre-registered them by email
   (`POST /api/admin/users`) and the email is verified in Casdoor, the
   accounts are linked.

## How the API checks tokens

- Signature against Casdoor's JWKS (discovery from `OIDC_ISSUER`, or
  `OIDC_JWKS_URL` when the API reaches Casdoor under another host name).
- `iss` must equal `OIDC_ISSUER` exactly (`http://localhost:8000` locally).
- `aud` must contain one of `OIDC_AUDIENCES`.
- `exp` must be in the future.
- Suspended users (`PUT /api/admin/users/{id}` with `status: suspended`) are
  rejected even with a valid token.
