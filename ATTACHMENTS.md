# Issue Attachments

Issue attachments connect issue descriptions to project object storage. Description text stores Markdown source in `issues.description`; files live in object storage and are linked through `issue_attachments`.

## Data Model

`issue_attachments` is a link/audit table. It stores:

- `project_id`: denormalized for same-project foreign keys and realtime routing.
- `issue_id`: live issue being annotated.
- `storage_object_id`: referenced object metadata row.
- `created_by_id`, `created_at`, `updated_at`, `version`: audit and realtime fields.

Do not copy filename, content type, size, hash, backend, bucket, or object key into `issue_attachments`. Those values belong only to `storage_objects`.

## Upload Flow

Issue attachment uploads use multipart field `file`.

1. Server writes bytes to configured object storage backend.
2. Server inserts one `storage_objects` metadata row.
3. Server inserts one `issue_attachments` link to that object.
4. If link creation fails, server soft-deletes the metadata row and deletes backend bytes on a best-effort cleanup path.

Objects attached through the issue upload path are owned by that attachment flow. Removing the attachment removes the link, soft-deletes the object metadata row, and then deletes backend bytes.

## Markdown Resolution

Issue description Markdown may reference attached files by project-local object ref:

```markdown
![Screenshot](object-3)
[Log file](object-4)
```

Rendering resolves `object-N` only against attachments on the current issue. Missing or unattached object refs render inert text instead of links or images.

The issue UI shows each attachment below the description editor/view. Safe image attachments include a compact preview, and every attachment row has a copy action for the Markdown snippet so users can restore an accidentally removed reference.

Safe image content types render inline through the issue attachment content route:

- `image/png`
- `image/jpeg`
- `image/gif`
- `image/webp`
- `image/avif`
- `image/bmp`

SVG is not rendered inline. Non-image attachments render as download links.

## Routes

Project members can use the API routes:

- `POST /api/v1/{owner}/issues/{issueRef}/attachments`
- `GET /api/v1/{owner}/issues/{issueRef}/attachments`
- `GET /api/v1/{owner}/issues/{issueRef}/attachments/{objectRef}/content`
- `DELETE /api/v1/{owner}/issues/{issueRef}/attachments/{objectRef}`

The UI has cookie-authenticated equivalents under:

- `/{owner}/issues/{issueRef}/attachments`
- `/{owner}/issues/{issueRef}/attachments/{objectRef}/content`
- `/{owner}/issues/{issueRef}/attachments/{objectRef}/delete`
- `DELETE /{owner}/issues/{issueRef}/attachments/{objectRef}`

Content responses stream bytes from the backend with stored content type, content length, SHA-256 ETag, content disposition, and `X-Content-Type-Options: nosniff`.

## Security Constraints

- Raw HTML in Markdown remains disabled.
- Markdown links allow safe external URL schemes only, plus same-origin absolute paths and fragments.
- `object-N` refs never cross issue boundaries.
- Inline rendering is limited to safe image content types; downloads use attachment disposition.
