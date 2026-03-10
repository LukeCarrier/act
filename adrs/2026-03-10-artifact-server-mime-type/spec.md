---
status: draft
created: 2026-03-10
updated: 2026-03-11
author: adrian
decision: pending
---

# Artifact Server MIME Type Support

**Date:** 2026-03-10
**Status:** Draft

## Problem Statement

The upstream `@actions/artifact` package added a `mime_type` field to the `CreateArtifactRequest` protobuf message at field number 6. The upstream `CreateArtifactRequest` message has the following fields:

- `workflow_run_backend_id` (string, field 1) — identifies the workflow run
- `workflow_job_run_backend_id` (string, field 2) — identifies the workflow job run
- `name` (string, field 3) — artifact name
- `expires_at` (`google.protobuf.Timestamp`, field 4) — optional expiry time
- `version` (int32, field 5) — protocol version (currently `7`)
- `mime_type` (`google.protobuf.StringValue`, field 6) — MIME type of the uploaded content

The `mime_type` field is always populated in artifact v7 protocol requests — for zipped archives it is set to `application/zip`, and for raw file uploads (when `archive: false`) it is set to the detected MIME type of the file (e.g. `application/pdf`, `image/png`, `application/octet-stream`). The MIME type is determined client-side by the `getMimeType()` helper in `@actions/artifact`, which maps file extensions to MIME types and falls back to `application/octet-stream` for unknown extensions.

act's artifact server currently defines `CreateArtifactRequest` without this field (fields 1–5 only). When the `@actions/artifact` client serializes a `CreateArtifactRequest` with `mime_type` set, protobuf's wire format places the value at field number 6. act's generated Go code ignores this unknown field during deserialization — so it doesn't cause a hard failure — but the information is silently discarded.

The `archive: false` option (exposed as `skipArchive` internally) allows uploading a single file without zipping it first. This is a supported feature in `actions/upload-artifact@v7` and relies on the server accepting and correctly handling raw file uploads alongside their MIME type metadata. Downloading raw artifacts requires `actions/download-artifact@v8`.

When `skipArchive` is true, the upstream `@actions/artifact` client overrides the user-specified artifact `name` to the basename of the uploaded file. For example, given `name: my-artifact, path: output/report.pdf, archive: false`, the `CreateArtifactRequest` is sent with `name: "report.pdf"` — the user's `name: my-artifact` is discarded. This means for raw uploads, `req.Name` on the server is both the artifact name and the original filename. For archive uploads, `req.Name` remains the user-specified artifact name.

## Goals

1. **Protocol compatibility:** act's protobuf schema should match the upstream `CreateArtifactRequest` definition, accepting the `mime_type` field without discarding it. Additionally, add the missing `digest` field (`google.protobuf.StringValue`, field 7) to `MonolithArtifact`.
2. **Correct raw file upload handling:** When a workflow uses `actions/upload-artifact` with `archive: false`, act should store the uploaded file in its original form (not attempt to decompress/unzip it) and preserve its MIME type association.
3. **Accurate download serving:** When an artifact uploaded with a specific MIME type is downloaded, act should serve it with the correct `Content-Type` header. This is critical because `@actions/artifact`'s `streamExtractExternal()` function inspects the download response's `Content-Type` header to determine whether to unzip (`application/zip`, `application/x-zip-compressed`, `application/zip-compressed`) or save the file raw. act's current `downloadArtifact` handler sets no `Content-Type` header, which is unreliable — the handler must always set `Content-Type` explicitly from the stored metadata (defaulting to `application/zip` for legacy artifacts).

## User Journeys

### Journey 1: Upload a single raw file

A user's workflow contains:

```yaml
- uses: actions/upload-artifact@v7
  with:
    path: output/report.pdf
    archive: false
```

Note: the `name` input is not specified. When `archive: false`, the `@actions/artifact` client overrides any user-specified `name` with the basename of the uploaded file (`report.pdf`). Specifying `name` has no effect and is misleading.

The `@actions/artifact` client:

1. Calls `CreateArtifact` with `name: "report.pdf"`, `version: 7`, `mimeType: "application/pdf"`.
2. Uploads the raw PDF bytes to the signed upload URL.
3. Calls `FinalizeArtifact` with the file size and hash.

act should accept all three steps, store the file, and associate `application/pdf` as its MIME type.

### Journey 2: Upload a zipped archive (default behaviour)

```yaml
- uses: actions/upload-artifact@v4
  with:
    name: test-results
    path: test-output/
```

The client:

1. Calls `CreateArtifact` with `name: "test-results"`, `version: 4`. No `mimeType` field is sent.
2. Uploads the zip archive to the signed upload URL.
3. Calls `FinalizeArtifact`.

act should accept this request without error. The absence of `mimeType` signals the server to treat the upload as `application/zip` (the historical default). Existing zip-handling behaviour should be unchanged.

### Journey 3: Download an artifact

A subsequent step or external consumer fetches the artifact via act's download endpoint. The response must include a `Content-Type` header matching the stored MIME type (e.g. `application/pdf` for a raw PDF upload, `application/zip` for a zipped archive).

This is not cosmetic — the `@actions/artifact` download client (`streamExtractExternal()`) uses the `Content-Type` response header to decide how to handle the response body:

- If the `Content-Type` is `application/zip`, `application/x-zip-compressed`, or `application/zip-compressed` (or the URL path ends with `.zip`), the client pipes the response through `unzip.Extract()`.
- Otherwise, the client saves the response body as a raw file, deriving the filename from the `Content-Disposition` header (falling back to `"artifact"` if absent).

act's current `downloadArtifact` handler has no notion of content type — it streams the file bytes directly to the response without setting a `Content-Type` header, as it was previously assumed all artifacts would be zip archives. This will cause the download client to mishandle raw file artifacts.

## Functional Requirements

### FR-1: Extend protobuf schema to match upstream

The `CreateArtifactRequest` message must include `mime_type` as a `google.protobuf.StringValue` at field number 6, matching the upstream definition:

```
message CreateArtifactRequest {
  string workflow_run_backend_id = 1;
  string workflow_job_run_backend_id = 2;
  string name = 3;
  google.protobuf.Timestamp expires_at = 4;
  int32 version = 5;
  google.protobuf.StringValue mime_type = 6;
}
```

The `MonolithArtifact` message must include the missing `digest` field:

```
message MonolithArtifact {
  string workflow_run_backend_id = 1;
  string workflow_job_run_backend_id = 2;
  int64 database_id = 3;
  string name = 4;
  int64 size = 5;
  google.protobuf.Timestamp created_at = 6;
  google.protobuf.StringValue digest = 7;
}
```

The generated Go code (`artifact.pb.go`) must be regenerated to include these changes.

### FR-2: Parse and store the MIME type

The `createArtifact` handler must read the `MimeType` field from the deserialized `CreateArtifactRequest` and persist it alongside the artifact metadata. This value must be retrievable when the artifact is later downloaded.

### FR-3: Serve correct `Content-Type` and `Content-Disposition` on download

The `downloadArtifact` handler must set the `Content-Type` response header to the stored MIME type when serving artifact content. It must also set a `Content-Disposition` header with the artifact filename (the download client uses this to determine the output filename for raw files). If no MIME type was stored (e.g. artifacts created before this change), fall back to `application/zip` to preserve existing behaviour.

The V1 `downloads()` file server should similarly respect stored MIME types when serving artifacts from `outdir`, though `http.ServeFile` already handles content type detection for extracted files. Changes to the V1 routes are out of scope for this change.

### FR-4: Support raw (non-archive) file uploads

The upload path must correctly handle raw file uploads — when `mime_type` is not `application/zip`, the uploaded bytes should be stored as-is without any assumption of zip format. This includes:

- Storing the file at the expected path on disk.
- Not attempting gzip decompression or zip extraction on raw uploads.
- Correctly finalizing and reporting the artifact.

### FR-5: Backward compatibility

- Requests from older clients that do not send `mime_type` (field 6 absent) must continue to work. The server should treat a missing `mime_type` as `application/zip` (the historical default).
- Existing API endpoints must be unaffected. There is no backwards-incompatible change — the server continues to accept requests without `mime_type` and defaults to zip behaviour.

## Non-Functional Requirements

### NFR-1: No new dependencies

The change should use existing protobuf and standard library facilities. No new Go module dependencies should be introduced.

### NFR-2: Test coverage

New and modified behaviour must be covered by unit tests, particularly:

- Deserialization of `CreateArtifactRequest` with and without `mime_type`.
- Upload and download of raw files with MIME type preservation.
- Backward compatibility with requests missing the field.

### NFR-3: On-disk format

#### Current layout

```
<dir>/<runId>/<artifactName>/
  <artifactName>.zip
```

- The upload handler (`uploadArtifact`) appends all incoming bytes to `<artifactName>.zip` regardless of content type.
- The list handler scans for directories to enumerate artifacts.
- The download handler has no notion of content type — it streams file bytes without setting `Content-Type`.
- There are no metadata files — all metadata (name, size, timestamps, IDs) is derived dynamically from the filesystem.

#### Proposed layout

```
<dir>/<runId>/<slugifiedArtifactName>/
  metadata.json
  content/
    <slugifiedFilename>
```

- `metadata.json` — stores the MIME type, original artifact name, and original filename (e.g. `{"mimeType": "application/pdf", "artifactName": "report.pdf", "originalFilename": "report.pdf"}`). Written by the `CreateArtifact` handler. The original names are preserved here so they can be used in responses (e.g. `Content-Disposition` header) even though the on-disk names are slugified.
- `content/` — subdirectory holding the uploaded file under a slugified version of its filename. The original filename is determined by: if `mime_type` is present and equals `application/zip` (v7 archive upload), `req.Name + ".zip"`; if `mime_type` is present and is not `application/zip` (v7 raw upload), `req.Name`; if `mime_type` is absent (legacy/v4), `<artifactName>.zip`.
- Both the artifact directory name and the content filename are slugified for filesystem safety (see NFR-5). Lookups use deterministic recomputation of the slug from the original name.

The `content/` subdirectory is necessary because the upload handler needs to write the file before knowing whether it's an archive or raw — it receives the bytes via a separate `UploadArtifact` PUT, not the `CreateArtifact` RPC. The `CreateArtifact` handler writes `metadata.json` with the MIME type and filename, then passes the filename through to the signed upload URL as a query parameter. The upload handler reads that parameter to determine the target path within `content/`.

This layout affects several handlers:

- **`createArtifact`**: slugifies the artifact name for the directory, writes `metadata.json` (with original name), includes the slugified content filename in the signed upload URL.
- **`uploadArtifact`**: writes to `content/<slugifiedFilename>` instead of `<artifactName>.zip`.
- **`finalizeArtifact`**: no changes required (currently returns success without extraction).
- **`downloadArtifact`**: reads `metadata.json` to set `Content-Type` and `Content-Disposition` (using the original filename), serves from `content/<slugifiedFilename>`.
- **`listArtifacts`**: scans for directories containing `metadata.json`. No fallback to legacy layout — pre-change artifacts are not supported (see EC-6).
- **`Downloads()` / `outdir`**: out of scope for this change. The V1 `downloads()` file server uses `http.ServeFile`, which already handles content type detection. If outdir extraction is added to `finalizeArtifact` in the future, it would need to consult `metadata.json` (see EC-7).

### NFR-4: Integration test coverage

An integration test should exercise the full upload/download flow against act's artifact server, covering both:

- `actions/upload-artifact@v4` (zip archive, existing behaviour)
- `actions/upload-artifact@v7` with `archive: false` (raw file upload, new behaviour)

This validates the end-to-end flow: the action's client constructing a `CreateArtifactRequest` with `mime_type`, uploading a raw file, finalizing, and confirming the artifact is stored correctly.

The test should use committed workflow YAML fixtures in the test fixtures directory, similar to other act integration tests. The raw upload fixture should:

- Upload a single file with `archive: false`.
- Verify the artifact is stored correctly and retrievable.
- Confirm that the download response has the correct `Content-Type` for the uploaded file.

### NFR-5: Filesystem-safe name slugification

Artifact names and content filenames are user-controlled strings that must not be used directly as filesystem paths. The current code uses `safeResolve()` to prevent path traversal but does not sanitise names — meaning an artifact name like `my report (final) v2` creates a directory literally called `my report (final) v2`, and names containing filesystem-unsafe characters (null bytes, control characters, reserved names on Windows) could cause failures.

Both the artifact directory name and the content filename within `content/` must be slugified before use as filesystem paths. The original names are preserved in `metadata.json` for use in API responses and `Content-Disposition` headers.

#### Slugification algorithm

Use a hash prefix + human-readable suffix approach:

1. **Hash prefix**: Compute SHA-256 of the original name. Encode as a full 64-character lowercase hex string.
2. **Suffix**: Take the original name, lowercase it, replace non-alphanumeric characters (except `.` and `-`) with hyphens, collapse consecutive hyphens, trim leading/trailing hyphens. Truncate to 32 characters.
3. **Slug**: Concatenate as `<hash>-<suffix>` (e.g. `e3b0c44298fc1c149afbf4c8996fb924...f2ca1bb6c7-my-report-final-v2`).

This ensures:
- **Deterministic**: The same original name always produces the same slug, enabling O(1) lookups by direct path construction — no directory scanning.
- **Collision-proof**: The full SHA-256 prefix guarantees uniqueness.
- **Human-readable**: The suffix provides context for debugging and filesystem inspection.
- **Filesystem-safe**: Only lowercase alphanumeric characters, dots, and hyphens are used.

#### Lookup by name

When the server receives a request referencing an artifact by name (e.g. `listArtifacts` with `name_filter`, `getSignedArtifactURL`, `deleteArtifact`), it recomputes the slug from the requested name and resolves the directory path directly — no directory scanning required.

#### No legacy fallback

Artifacts created before this change (using unslugged directory names) will not be found by the new code. Users should delete existing artifact state before upgrading. This is acceptable because act's artifact storage is ephemeral by nature.

## Acceptance Criteria

1. **AC-1:** A `CreateArtifactRequest` with `mime_type` set to `"application/pdf"` is deserialized correctly, and the value is accessible in the handler.
2. **AC-2:** A `CreateArtifactRequest` without `mime_type` (field absent) is deserialized without error, and the handler treats it as having no explicit MIME type.
3. **AC-3:** A raw PDF file uploaded via the v4 upload flow (signed URL) is stored on disk as a valid PDF (byte-identical to the original).
4. **AC-4:** Downloading the artifact from AC-3 returns a response with `Content-Type: application/pdf` and an appropriate `Content-Disposition` header.
5. **AC-5:** A zipped archive uploaded via the standard flow continues to work identically to current behaviour.
6. **AC-6:** The V1 `downloads()` file server is unaffected by this change (out of scope; `http.ServeFile` already handles content type detection).
7. **AC-7:** All existing v4 artifact server tests continue to pass.
8. **AC-8:** New tests cover raw file upload, MIME type round-trip, and missing MIME type fallback.
9. **AC-9:** The `version` field on `CreateArtifactRequest` is `int32`, matching upstream.
10. **AC-10:** The `digest` field is present on `MonolithArtifact` at field number 7 as `google.protobuf.StringValue`.
11. **AC-11:** Committed workflow YAML fixtures exercise both `actions/upload-artifact@v4` (zip upload) and `actions/upload-artifact@v7` (with `archive: false`) as integration tests.
12. **AC-12:** Artifact directory names and content filenames are slugified — no user-controlled strings are used directly as filesystem paths.
13. **AC-13:** The slugification algorithm is deterministic — the same artifact name always produces the same slug.
14. **AC-14:** Original artifact names and filenames are preserved in `metadata.json` and used in API responses and `Content-Disposition` headers.

## Edge Cases and Error Handling

### EC-1: Missing `mime_type` field

Older `@actions/artifact` versions or custom clients may not send `mime_type`. The server must not reject these requests. Behaviour should default to current semantics (treat as `application/zip`).

### EC-2: Empty `mime_type` value

The `StringValue` wrapper may be present but contain an empty string. Treat as equivalent to absent — fall back to default.

### EC-3: Invalid or unusual MIME types

The server should accept any string value in `mime_type` without validation. MIME type validation is the client's responsibility. The server stores and echoes back whatever value was provided.

### EC-4: Multiple files in a raw upload

The `@actions/artifact` client [enforces single-file-only for `skipArchive` uploads](https://github.com/actions/toolkit/blob/main/packages/artifact/src/internal/upload/upload-artifact.ts). If multiple files are provided with `skipArchive: true`, the client throws before making any server requests. If multiple files are somehow uploaded to the same artifact name via direct API calls, the server should handle them using existing multi-file artifact semantics. The MIME type applies to the artifact as a whole, not individual files.

### EC-5: Artifact overwrite

If an artifact with the same name is finalized again (overwrite scenario), the MIME type should be updated to reflect the new upload's value.

### EC-6: Artifacts created before this change

Artifacts stored on disk before this change use unslugged directory names and lack `metadata.json`. These artifacts will not be found by the new code. No migration or fallback is provided — users should delete existing artifact state before upgrading. This is acceptable because act's artifact storage is ephemeral by nature.

### EC-7: `outdir` copy behaviour for raw files

The current finalize handler does not perform extraction — it returns success without touching the filesystem. If `outdir` support is added in the future, it would need to consult `metadata.json` to decide whether to extract (zip) or copy (raw) the artifact content. This is out of scope for the current change but should be considered if outdir extraction is implemented later.
