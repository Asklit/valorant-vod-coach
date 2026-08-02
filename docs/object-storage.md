# Object Storage

The product uses an S3-compatible bucket as durable storage and local directories only as seekable processing caches. This allows `vod-web` and any number of `vod-worker` processes to run on different hosts without sharing a filesystem.

## Key Layout

```text
uploads/<owner-id>/<vod-label>/video.<ext>
artifacts/users/<owner-id>/analyses/<vod-label>/...
```

`S3_PREFIX` may prepend an environment name such as `production/`. Keys are generated from authenticated ownership and server-created resource IDs; clients cannot provide arbitrary keys.

## Upload Flow

1. `vod-web` streams the multipart body into a private staging file with a hard byte limit.
2. ffprobe validates that the file contains a usable video stream.
3. The API uploads the file with AWS S3 Transfer Manager, which uses multipart transfer for large VODs.
4. PostgreSQL commits metadata and the canonical object key only after object upload succeeds.
5. The local file remains a cache and can be removed without losing the VOD.

## Analysis Flow

1. The worker resolves tenant-owned metadata in PostgreSQL.
2. If its local VOD cache is cold, it downloads the canonical object into a temporary file and atomically renames it into place.
3. ffmpeg, OCR, and CPU analysis operate on local seekable files.
4. Report publication uploads all frames, manifests, contact sheets, clips, evidence, and report files with bounded concurrency.
5. PostgreSQL marks the Temporal job complete only after publication and report snapshot persistence succeed.

## Read And Delete Semantics

The authenticated `/artifacts/` gateway checks tenant access before touching object storage. A missing local artifact is materialized from S3 and subsequently served from the warm cache. Lexical traversal and existing symlink components are rejected before download and verified again afterward.

Deletion removes the PostgreSQL catalog entry first, immediately revoking normal access. Local, VOD-object, and artifact-prefix cleanup follows. A transient cleanup failure returns `202 Accepted` with a warning and leaves only an inaccessible orphan for lifecycle cleanup rather than exposing deleted data.

## Configuration

Set `S3_BUCKET` to enable object storage. Local MinIO additionally uses `S3_ENDPOINT=http://localhost:9002`, static credentials, and `S3_USE_PATH_STYLE=true`. With AWS S3, omit `S3_ENDPOINT`, use virtual-hosted addressing, and use workload identity instead of static credentials where available.
