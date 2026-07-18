# Deployment

## Docker Image

The root `Dockerfile` builds the production `trackd` image. The same image is used for both the web app and database migrations.

Build locally:

```bash
docker build -t track-slash-frontend .
```

## Database Migrations

For production platforms such as Dokploy, prefer a separate migration app/job that runs before the frontend app is deployed or restarted.

Use the same Docker image as the frontend and override the command:

```bash
/app/trackd -migrate-only
```

The migration job only requires:

```bash
DATABASE_URL=postgres://...
```

Local source migration:

```bash
make migrate
```

Migration through a built Docker image:

```bash
make docker-migrate IMAGE=track-slash-frontend
```

When running migrations from inside Docker, make sure `DATABASE_URL` names a Postgres host reachable from that container.

## Frontend App

Run the frontend app with the default image command:

```bash
/app/trackd
```

By default, `trackd` runs embedded migrations during normal startup. When a separate migration job owns migrations, set this on the frontend app:

```bash
TRACK_SLASH_AUTO_MIGRATE=false
```

Keep `TRACK_SLASH_AUTO_MIGRATE=true` or unset it for local development and single-container deployments.

Set the public browser origin for passkeys and browser WebSocket checks in deployed environments:

```bash
TRACK_SLASH_PUBLIC_ORIGIN=https://track.example.com
```

The value must be an origin only: scheme, host, and optional port. Production passkeys require HTTPS. The cookie-authenticated UI WebSocket accepts this origin only; it does not depend on `CORS_ALLOWED_ORIGINS`. Browser connections to the bearer-authenticated API WebSocket may also use an explicitly configured CORS origin. Non-browser API WebSocket clients must omit `Origin` and authenticate with a bearer token.

When `TRACK_SLASH_PUBLIC_ORIGIN` is omitted for local development, browser WebSockets accept only `localhost` and loopback origins. Other non-empty browser origins fail closed.

The same setting gates HTTP Strict Transport Security. trackslash emits `Strict-Transport-Security: max-age=31536000` only when `TRACK_SLASH_PUBLIC_ORIGIN` explicitly uses HTTPS; direct TLS and forwarded-protocol headers do not opt a deployment into HSTS. Validate HTTPS redirects, certificates, and rollback procedures on staging before enabling the HTTPS origin in production because browsers retain the policy for one year. The header deliberately omits `includeSubDomains` and `preload`, so sibling services keep independent rollout and recovery paths.

All responses also receive a self-only Content Security Policy, clickjacking protection, `nosniff`, `Referrer-Policy: no-referrer`, and a minimal Permissions Policy. UI scripts, styles, images, forms, and realtime connections are expected to stay on the configured application origin.

Login sessions have a configurable seven-day absolute lifetime by default:

```bash
TRACK_SLASH_SESSION_TTL=168h
```

The value uses Go duration syntax and must be positive. Session activity does not extend the deadline; idle expiry is not currently applied. API tokens remain distinct and do not expire unless an expiry is explicitly supplied when they are created.

Browser mutations use CSRF tokens bound to either the pre-login flow or the authenticated session, plus exact-origin checks when browsers send `Origin`, `Referer`, or Fetch Metadata. Treat sibling subdomains as untrusted: `same-site` requests are rejected unless they are also `same-origin`. Keep `TRACK_SLASH_PUBLIC_ORIGIN` set to the single canonical browser origin in production, and do not route alternate sibling origins to the UI. Bearer-authenticated API and MCP requests do not use browser CSRF tokens.

### Development-preview terms

The canonical `/terms`, `/privacy`, and `/security` pages describe the development preview operated by Bad Bundle Limited at `trackslash.com`. They do not govern independent self-hosted installations.

The Bad Bundle-operated preview should require agreement during password and passkey account creation:

```bash
TRACK_SLASH_PREVIEW_TERMS_REQUIRED=true
```

The accepted Preview Terms version and timestamp are stored transactionally with the new account. Independent operators should leave this setting false or unset unless they have replaced the canonical documents with policies appropriate to their own deployment and users.

## Authentication and request limits

Password, passkey, signup, and reauthentication endpoints are limited to 30 requests per resolved client IP per minute and 10 requests per normalized username or account identifier per five minutes. Exhausted limits return `429 Too Many Requests` with `Retry-After`.

Forwarded client IPs are ignored by default, so an unconfigured deployment behind a reverse proxy shares one application-level IP bucket. Configure the exact internal source ranges used by trusted proxies when requests normally arrive through a proxy:

```bash
TRACK_SLASH_TRUSTED_PROXY_CIDRS=10.0.0.0/8,172.16.0.0/12
```

Only an immediate peer inside one of these CIDR ranges may supply `X-Forwarded-For`. The application walks that chain from right to left, skips trusted proxy hops, and uses the first untrusted IP as the client. Invalid chains fall back to the immediate peer. Configure the proxy to replace or safely append `X-Forwarded-For`, use the narrowest practical CIDRs, and prevent clients from reaching trackslash directly. `X-Real-IP` and other forwarding headers are not trusted.

Request contexts and body reads use these deadlines:

- 15 seconds for ordinary form and JSON requests;
- 30 seconds for passkey/WebAuthn requests;
- 2 minutes for multipart uploads.

HTTP headers retain a separate five-second read deadline. WebSocket, MCP, and development reload streams are explicitly excluded from request deadlines; idle keep-alive HTTP connections are closed after 60 seconds.

## Object Storage

Production object storage is configured on the frontend app, not on the `-migrate-only` job. The migration job only needs `DATABASE_URL`.

For GCS via the existing S3-compatible backend, create a Google service account with bucket-scoped object access to `track-slash-main`, then create a GCS HMAC key for that service account. Configure the Dokploy frontend app with:

```bash
TRACK_SLASH_STORAGE_BACKEND=s3
TRACK_SLASH_STORAGE_BUCKET=track-slash-main
TRACK_SLASH_STORAGE_S3_ENDPOINT=https://storage.googleapis.com
TRACK_SLASH_STORAGE_S3_REGION=auto
TRACK_SLASH_STORAGE_S3_FORCE_PATH_STYLE=true
TRACK_SLASH_STORAGE_MAX_UPLOAD_BYTES=52428800

AWS_ACCESS_KEY_ID=<GCS_HMAC_ACCESS_ID>
AWS_SECRET_ACCESS_KEY=<GCS_HMAC_SECRET>
AWS_REQUEST_CHECKSUM_CALCULATION=when_required
AWS_RESPONSE_CHECKSUM_VALIDATION=when_required
```

Do not use `GOOGLE_APPLICATION_CREDENTIALS` for this path; the app is using the S3/XML API compatibility layer. Remove old Garage values such as `TRACK_SLASH_STORAGE_S3_ENDPOINT=http://garage:3900` and the Garage access key pair when switching production.

Keep the GCS bucket private. The app streams object bytes through authenticated trackslash routes, so browser-facing signed URLs and bucket CORS rules are not required.

## Garage To GCS Cutover

Copy existing Garage objects into `gs://track-slash-main` while preserving the exact `storage_objects.object_key` values. Use a short maintenance window or pause writes for the final sync so no object is uploaded only to the old backend.

Existing rows already use `backend = 's3'`, so no backend rename is needed. If old rows point at the previous Garage bucket name, update only the bucket metadata after bytes are copied:

```sql
UPDATE storage_objects
SET bucket = 'track-slash-main',
    updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
WHERE backend = 's3'
  AND bucket <> 'track-slash-main'
  AND deleted_at IS NULL;
```

After deploying the Dokploy environment change, smoke test upload, download, issue attachment inline preview, profile thumbnail rendering, deletion, and missing-object 404 behavior.
