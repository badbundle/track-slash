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

Set the public browser origin for passkeys in deployed environments:

```bash
TRACK_SLASH_PUBLIC_ORIGIN=https://track.example.com
```

The value must be an origin only: scheme, host, and optional port. Production passkeys require HTTPS; localhost development can omit this and use the request-derived local origin.

Login sessions have a configurable seven-day absolute lifetime by default:

```bash
TRACK_SLASH_SESSION_TTL=168h
```

The value uses Go duration syntax and must be positive. Session activity does not extend the deadline; idle expiry is not currently applied. API tokens remain distinct and do not expire unless an expiry is explicitly supplied when they are created.

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

Keep the GCS bucket private. The app streams object bytes through authenticated track-slash routes, so browser-facing signed URLs and bucket CORS rules are not required.

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
