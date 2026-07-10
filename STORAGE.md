# Object Storage

track-slash stores object metadata in Postgres and object bytes in a configured storage backend. This keeps the API and access-control model in the database while allowing local disk today and bucket-style backends later.

## Current Backends

V1 supports `local` and S3-compatible storage backends.

Local runtime config, with defaults:

```bash
TRACK_SLASH_STORAGE_BACKEND=local
TRACK_SLASH_STORAGE_LOCAL_ROOT=tmp/storage
TRACK_SLASH_STORAGE_BUCKET=local
TRACK_SLASH_STORAGE_MAX_UPLOAD_BYTES=52428800
```

`TRACK_SLASH_STORAGE_LOCAL_ROOT` is a directory on the machine running `trackd`. The default lives under `tmp/`, which is gitignored and suitable for local development. Production deployments should set an explicit durable path.

S3-compatible runtime config:

```bash
TRACK_SLASH_STORAGE_BACKEND=s3
TRACK_SLASH_STORAGE_BUCKET=track-slash
TRACK_SLASH_STORAGE_S3_ENDPOINT=http://garage:3900
TRACK_SLASH_STORAGE_S3_REGION=us-east-1
TRACK_SLASH_STORAGE_S3_FORCE_PATH_STYLE=true
TRACK_SLASH_STORAGE_MAX_UPLOAD_BYTES=52428800
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...
AWS_SESSION_TOKEN=... # optional
```

`TRACK_SLASH_STORAGE_S3_REGION` defaults to `us-east-1`; for S3-compatible services such as Garage this is primarily the SigV4 signing region. For AWS S3, set the real bucket region. `TRACK_SLASH_STORAGE_S3_FORCE_PATH_STYLE` defaults to `true` for compatibility with Garage and similar services. The app does not create buckets or access keys; create them in the storage service first. Garage's admin/API endpoint is not used by `trackd`; only the S3 endpoint is required.

Google Cloud Storage can be used through the same S3-compatible backend by using the Cloud Storage XML API and GCS HMAC keys:

```bash
TRACK_SLASH_STORAGE_BACKEND=s3
TRACK_SLASH_STORAGE_BUCKET=track-slash-main
TRACK_SLASH_STORAGE_S3_ENDPOINT=https://storage.googleapis.com
TRACK_SLASH_STORAGE_S3_REGION=auto
TRACK_SLASH_STORAGE_S3_FORCE_PATH_STYLE=true
TRACK_SLASH_STORAGE_MAX_UPLOAD_BYTES=52428800

AWS_ACCESS_KEY_ID=...        # GCS HMAC access ID
AWS_SECRET_ACCESS_KEY=...    # GCS HMAC secret
AWS_REQUEST_CHECKSUM_CALCULATION=when_required
AWS_RESPONSE_CHECKSUM_VALIDATION=when_required
```

GCS HMAC keys are separate from Google service account JSON credentials. The current `s3` backend does not use `GOOGLE_APPLICATION_CREDENTIALS`; it signs S3/XML API requests with the `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` environment variables. Keep the GCS bucket private. Downloads continue to flow through authenticated track-slash routes, so browser-facing signed URLs and GCS CORS rules are not required.

When the configured endpoint is `storage.googleapis.com`, the S3 backend automatically applies the GCS XML API interoperability rules: `Accept-Encoding` is excluded from SigV4 because Google's frontend rewrites it in transit, and S3's `If-None-Match: *` precondition is omitted from `PUT` requests because GCS rejects mixing AWS-signed `x-amz-*` headers with its `x-goog-*` generation precondition. Object keys are UUID-derived, so collisions remain negligibly unlikely. Other S3-compatible backends retain the create-only precondition.

For failed S3-compatible requests, the backend adds bounded diagnostics to the internal server error log: operation, HTTP request shape, status, API code, request IDs, SigV4 credential scope and signed-header names, and the GCS `Details`, `CanonicalRequest`, and `StringToSign` fields when present. Authorization values, access IDs, signatures, session tokens, cookies, and customer encryption keys are not logged. Error response capture is limited to 64 KiB and is never returned to API clients.

## Data Model

`storage_objects` is the metadata table. Each row belongs to exactly one owner scope:

- Project-owned objects have `project_id`, no `owner_user_id`, and a project-local public ref such as `object-1`.
- User-owned objects have `owner_user_id`, no `project_id`, and no public `object-N` ref.

Important fields:

- `project_id`, `owner_user_id`, `number`: exactly one owner scope plus project-local numbering for project objects.
- `backend`, `bucket`, `object_key`: backend locator. Project keys look like `projects/{project_id}/objects/{object_id}`. User profile image keys look like `users/{user_id}/profile-images/{object_id}/{variant}`.
- `filename`, `content_type`, `byte_size`, `sha256`: download metadata and integrity metadata.
- `created_by_id`, timestamps, `deleted_at`: audit and soft-delete state.

Project object rows are listed and fetched only when both the project and object are live. Project soft-delete makes objects inaccessible but does not physically remove backend bytes. User-owned rows are fetched only through feature-specific routes, not project object routes.

Profile images are account-wide user-owned objects. Each user can reference one original image row in `users.profile_image_object_id` and one generated thumbnail row in `users.profile_image_thumbnail_object_id`. Both rows must be live `storage_objects` rows owned by the same user.

## API

Project members can use:

- `POST /api/v1/{owner}/projects/{key}/objects` with multipart field `file`.
- `GET /api/v1/{owner}/projects/{key}/objects`.
- `GET /api/v1/{owner}/projects/{key}/objects/{objectRef}`.
- `GET /api/v1/{owner}/projects/{key}/objects/{objectRef}/content`.
- `DELETE /api/v1/{owner}/projects/{key}/objects/{objectRef}`.

Downloads stream backend bytes and set `Content-Type`, `Content-Length`, `ETag` from SHA-256, and `Content-Disposition` with the stored filename.

Signed-in users can use profile image routes:

- `POST /api/v1/me/profile-image` with multipart field `file`.
- `DELETE /api/v1/me/profile-image`.
- `GET /api/v1/users/{id}/profile-image/content`.
- `GET /api/v1/users/{id}/profile-image/thumbnail/content`.

Any authenticated user may read any live user's profile image content. Profile image objects are not exposed through project `object-N` listing, metadata, content, or delete routes.

## Write And Delete Flow

Uploads write bytes to the backend first, then insert metadata in Postgres. If the metadata insert fails, the server deletes the just-written backend object.

Deletes soft-delete the Postgres row first, then remove backend bytes. This avoids live metadata pointing at a missing file. A crash between those steps can leave orphaned backend bytes, which are inaccessible through the API and should be handled by a future cleanup task.

Profile image replacement writes the original and generated thumbnail to the backend first, creates user-owned metadata rows, then updates the user profile pointers in one transaction. Previous profile image rows are soft-deleted and their backend bytes are deleted best-effort. Removing a profile image clears both user pointers, soft-deletes the old rows, and also deletes backend bytes best-effort.

The server accepts decodable PNG, JPEG, GIF, WebP, and BMP profile uploads. SVG, AVIF, non-images, corrupt images, oversized files, and unreasonable dimensions are rejected before profile metadata is linked. The generated thumbnail is a centered square `128x128` PNG; animated formats use the decoded first frame.

## Remote Backend Path

Remote backends implement `internal/storage.Backend` and keep the same metadata table:

- `backend`: stable backend name such as `s3`.
- `bucket`: remote bucket/container name.
- `object_key`: opaque backend key generated by the storage service, not user input.

Do not store raw object bytes in Postgres. Product features such as issue attachments or image fields should reference `storage_objects.id` from their own tables.

Issue description attachments are documented in `ATTACHMENTS.md`. Attachment links store only issue/object/audit data; object metadata such as filename, content type, byte size, hash, backend, bucket, and object key remains in `storage_objects`.
