---
status: draft
created: 2026-03-10
updated: 2026-03-11
author: adrian
decision: pending
---

# Plan: artifact server MIME type support

Implements the specification in [spec.md](spec.md).

## Overview

The artifact server needs three categories of change:

1. **Protocol** — extend the protobuf schema to include `mime_type` and `digest` fields.
2. **Storage** — migrate from a single `<artifactName>.zip` blob to a `metadata.json` + `content/` directory structure.
3. **Handlers** — update `createArtifact`, `uploadArtifact`, `downloadArtifact`, and `listArtifacts` to read/write MIME metadata and serve correct HTTP headers.

As a prerequisite, the existing `V4` suffix on types and functions introduced by the Gitea authors will be dropped. This suffix has no relation to the GitHub Actions protocol versioning and creates confusion. See the [naming changes](#naming-changes) section.

## Version mapping

The `version` field on `CreateArtifactRequest` corresponds to the `upload-artifact` action major version:

| Action | `@actions/artifact` dep | `version` sent | `mime_type` field | `archive` input |
|---|---|---|---|---|
| `upload-artifact@v4` | `^2.3.2` | `4` | Not sent | Not available |
| `upload-artifact@v7` | `^6.2.0` | `7` | Always sent | Yes (default `true`) |

The `download-artifact` action is versioned independently of `upload-artifact`:

| Action | Notes |
|---|---|
| `download-artifact@v4` | Downloads zip archives (existing behaviour) |
| `download-artifact@v8` | Required for downloading raw (non-archive) artifacts uploaded with `archive: false` |

The Twirp RPC endpoints and protobuf package (`github.actions.results.api.v1`) are the same for both versions. The `version` field is informational — the client does not branch on it, and the server does not need to dispatch based on it. The protocol differences are purely additive (new optional fields).

## Naming changes

The `V4` suffix on handler types, functions, and constants was introduced by the Gitea authors when implementing compatibility with `upload-artifact@v4`. It does not correspond to any GitHub Actions protocol version. Dropping it avoids confusion with the `version` field semantics.

| Current | Renamed |
|---|---|
| `ArtifactV4RouteBase` | `ArtifactRouteBase` |
| `ArtifactV4ContentEncoding` | `ArtifactContentEncoding` |
| `artifactV4Routes` | `artifactRoutes` |
| `validateRunIDV4` | `validateRunID` |
| `RoutesV4` | `Routes` |

Handler methods (`createArtifact`, `uploadArtifact`, etc.) are already suffix-free — they're methods on the struct, so renaming `artifactV4Routes` → `artifactRoutes` is sufficient.

The single external call site is `server.go` where `RoutesV4(router, ...)` becomes `Routes(router, ...)`.

The source file `artifacts_v4.go` should be renamed to `artifacts.go`.

## 1. Protobuf schema

### 1.1 Update `pkg/artifacts/artifact.proto`

The checked-in `artifact.proto` reproduces the existing schema from Gitea. Add the missing upstream fields:

- `CreateArtifactRequest.mime_type` — `google.protobuf.StringValue`, field 6
- `ListArtifactsResponse_MonolithArtifact.digest` — `google.protobuf.StringValue`, field 7

The `version` field is already `int32` at field 5 — no change needed.

Use `google/protobuf/wrappers.proto` for `StringValue` to match upstream. The existing `.pb.go` already imports `wrapperspb`.

### 1.2 Add `generate.go` directive

Create `pkg/artifacts/generate.go` containing a `go:generate` directive for `protoc`:

```go
package artifacts

//go:generate protoc --go_out=. --go_opt=paths=source_relative artifact.proto
```

### 1.3 Regenerate `artifact.pb.go`

Run `go generate ./pkg/artifacts/` to produce the updated `.pb.go`. Verify the generated output compiles and the new fields are accessible. Commit both the updated `artifact.proto` and the regenerated `artifact.pb.go`.

### 1.4 Verification

- `CreateArtifactRequest` has `MimeType *wrapperspb.StringValue` getter
- `ListArtifactsResponse_MonolithArtifact` has `Digest *wrapperspb.StringValue` getter
- All existing fields retain their numbers — no wire-format breakage
- `go vet ./pkg/artifacts/...` passes

## 2. On-disk storage format

### 2.1 Metadata file

Introduce a `metadata.json` file written atomically during `createArtifact`:

```json
{
  "mime_type": "text/plain",
  "artifact_name": "my-report",
  "original_filename": "report.txt"
}
```

Fields:
| Field | Source | Required |
|---|---|---|
| `mime_type` | `CreateArtifactRequest.mime_type` | Yes (default `"application/zip"`) |
| `artifact_name` | `CreateArtifactRequest.name` | Yes |
| `original_filename` | v7 raw: `req.Name`; v7 archive: `req.Name + ".zip"`; v4/legacy: `<artifactName>.zip` | Yes |

Use `encoding/json` from the standard library. No new dependencies (NFR-1).

### 2.2 Directory layout migration

**Current:**
```
<baseDir>/<runId>/<artifactName>/
  <artifactName>.zip
```

**Proposed:**
```
<baseDir>/<runId>/<slugifiedArtifactName>/
  metadata.json
  content/
    <slugifiedFilename>
```

Both `<slugifiedArtifactName>` and `<slugifiedFilename>` are produced by a deterministic hash + suffix algorithm (see section 2.5). The full SHA-256 hex prefix enables direct path construction — no directory scanning. Original names are preserved in `metadata.json`. Artifacts are scoped to the workflow run, not the job — GitHub's API lists artifacts by run ID and `WorkflowJobRunBackendId` is used only for auth scoping on the server side.

### 2.3 No legacy fallback

Artifacts created before this change (unslugged directory names, no `metadata.json`) will not be found by the new code. No migration or fallback logic is needed — users delete existing artifact state before upgrading. Act's artifact storage is ephemeral by nature.

### 2.4 Metadata struct

Define in `pkg/artifacts/artifacts.go` (renamed from `artifacts_v4.go`):

```go
type artifactMetadata struct {
    MimeType         string `json:"mime_type"`
    ArtifactName     string `json:"artifact_name"`
    OriginalFilename string `json:"original_filename"`
}
```

Helper functions:

```go
func writeMetadata(dir string, meta *artifactMetadata) error
func readMetadata(dir string) (*artifactMetadata, error)
```

`writeMetadata` writes atomically (write to temp file, then `os.Rename`). `readMetadata` returns an error if `metadata.json` does not exist — there is no legacy fallback.

### 2.5 Name slugification

Both artifact directory names and content filenames are slugified before use as filesystem paths. This prevents issues with user-controlled strings containing spaces, special characters, null bytes, or filesystem-reserved names.

#### Algorithm

```go
func slugify(name string) string
```

1. **Hash prefix**: Compute SHA-256 of the original name. Encode as a full 64-character lowercase hex string. This becomes the prefix.
2. **Human-readable suffix**: Lowercase the name, replace non-alphanumeric characters (except `.` and `-`) with hyphens, collapse consecutive hyphens, trim leading/trailing hyphens. Truncate to 32 characters.
3. **Result**: `<hash>-<suffix>` (e.g. `e3b0c44298fc1c149afbf4c8996fb924...f2ca1bb6c7-my-report-final-v2`)

Properties:
- **Deterministic**: Same input always produces the same slug. Lookups recompute the slug from the requested name — no directory scanning.
- **Collision-proof**: Full SHA-256 prefix guarantees uniqueness.
- **Direct path construction**: The hash prefix enables locating an artifact directory without scanning — compute the slug and go straight to the path.
- **Filesystem-safe**: Only lowercase alphanumeric, hyphens, and dots.

#### Usage in handlers

- `createArtifact`: `slugify(req.Name)` for directory, `slugify(contentFilename)` for file within `content/`
- `listArtifacts`: scans directories for `metadata.json`, returns `artifact_name` from metadata (not the slugified directory name)
- `downloadArtifact` / `getSignedArtifactURL`: recomputes slug from artifact name to locate directory
- `deleteArtifact`: recomputes slug to locate directory

## 3. Handler changes

### 3.1 `createArtifact`

Current behaviour: creates empty `<runId>/<artifactName>/<artifactName>.zip`.

Changes:
1. Read `mime_type` from `CreateArtifactRequest`. Default to `"application/zip"` if nil/empty.
2. Compute `slugify(req.Name)` for the artifact directory. Create `<runId>/<slugifiedName>/content/`.
3. Determine the content filename. The MIME type is the _only_ reliable
   signal for distinguishing raw vs archive uploads — there is no separate
   `skipArchive` field in the protocol. The `@actions/artifact` client
   always sends `mime_type: "application/zip"` for archive uploads and
   the detected file MIME type for raw uploads:
   - If `mime_type == "application/zip"` and `version ≥ 7` (v7 archive upload): use `req.Name + ".zip"`
   - If `mime_type != "application/zip"` and `version ≥ 7` (v7 raw upload): use `req.Name` (the upstream client overrides the artifact name to the file's basename)
   - If `mime_type` is absent (legacy/v4 client): use `<artifactName>.zip`
4. Compute `slugify(contentFilename)` for the on-disk filename.
5. Write `metadata.json` via `writeMetadata` (stores original artifact name, original filename, MIME type).
6. Return signed upload URL pointing to `UploadArtifact` endpoint. Include the slugified content filename in the URL query parameter so `uploadArtifact` knows where to write.

### 3.2 `uploadArtifact`

Current behaviour: appends bytes to `<runId>/<artifactName>/<artifactName>.zip` using `comp` query parameter protocol.

The chunked upload protocol uses a `comp` query parameter:

| `comp` value | Action |
|-------------|--------|
| `"block"` | Create/overwrite — `OpenAppendable` (O_CREATE\|O_RDWR, seek to end) + copy body |
| `"appendBlock"` | Append — same `OpenAppendable` + copy body |
| `"blocklist"` | Finalize — returns 201, no file I/O |

Chunks arrive sequentially; there is no Content-Range parsing. Each
chunk is appended via `Seek(0, io.SeekEnd)`.

Changes:
1. Extract the `filename` query parameter from the signed URL (added by T9, set during `createArtifact`).
2. Slugify the `artifactName` (from `verifySignature`) to resolve the directory.
3. Write to `<runId>/<slugifiedArtifactName>/content/<filename>` instead of the old `.zip` path.
4. The `comp` handling, `OpenAppendable` calls, and body copy logic remain **entirely unchanged**.

### 3.3 `finalizeArtifact`

No changes required. The handler currently returns success without performing extraction, and this behaviour is correct for both zip and raw artifacts.

### 3.4 `listArtifacts`

Current behaviour: scans directories under `<runId>/`, derives artifact metadata from directory names.

Changes:
1. Walk directories under `<runId>/`.
2. For each artifact directory, read `metadata.json` via `readMetadata`.
3. Populate `MonolithArtifact` with values from metadata (using `artifact_name`, not the slugified directory name).
4. Filter by `name` and `id` query semantics as before.

### 3.5 `downloadArtifact`

Current behaviour: opens `<runId>/<artifactName>/<artifactName>.zip`, calls `io.Copy` to response with no `Content-Type` header.

Note: the current code sets **no headers at all**. `net/http` may sniff
`Content-Type`, but this is unreliable and must not be depended upon.

Changes:
1. Resolve the artifact directory: `verifySignature` returns the original
   artifact name from the signed URL's `artifactName` query parameter.
   Slugify it to find the directory: `<runId>/<slugify(artifactName)>/`.
2. Read `metadata.json` via `readMetadata`.
3. Set `Content-Type` from `metadata.mime_type`.
4. Set `Content-Disposition: attachment; filename="<original_filename>"` using the original filename from metadata (RFC 6266 format).
5. Open file from `content/<slugifiedFilename>` (recompute slug from `original_filename`).
6. Stream file contents to response via `io.Copy`.

The handler must always set `Content-Type` explicitly — `net/http` content sniffing must not be relied upon. This is the critical change for download client compatibility.

### 3.6 `deleteArtifact`

No functional changes required. `os.RemoveAll` on the artifact directory already removes all contents regardless of layout.

### 3.7 `getSignedArtifactURL`

May need minor adjustments if the signed URL parameters change (e.g. adding the content filename), but `jobId` is not needed — artifacts are resolved by `runId` + `artifactName`.

## 4. Signed URL changes

The current signed URL scheme uses `buildArtifactURL(endpoint, artifactName, taskID)` to produce:

```
http://<AppURL>/<prefix>/<endpoint>?sig=<hmac>&expires=<time>&artifactName=<name>&taskID=<id>
```

The HMAC is computed over `endpoint + expires + artifactName + taskID`
using a hardcoded key (`{0xba, 0xdb, 0xee, 0xf0}`). Verification in
`verifySignature` extracts these query parameters and recomputes the HMAC.

### Upload URLs

Add a `filename` query parameter containing the slugified content
filename so that `uploadArtifact` knows the target path within
`content/`:

```
...&artifactName=<name>&taskID=<id>&filename=<slugFile>
```

The `filename` parameter MUST be included in the HMAC computation to
prevent path tampering. Update `buildArtifactURL` signature to accept
an optional filename, and update `verifySignature` to extract and
return it.

### Download URLs

Download URLs do NOT include `filename` — the download handler resolves
the content filename from `metadata.json` instead. Pass an empty string
for the filename parameter when building download URLs. The HMAC must
handle this gracefully (sign the empty string).

### Backward compatibility

This changes the HMAC input structure, invalidating any previously
signed URLs. This is acceptable because signed URLs are ephemeral —
they are generated during `createArtifact` and consumed within the same
workflow run. There is no cross-run URL reuse.

## 5. Testing strategy

### 5.1 Unit tests

Add to `pkg/artifacts/server_test.go` or a new `pkg/artifacts/artifacts_test.go`:

| Test | Validates |
|---|---|
| `TestSlugify` | Deterministic slugification, filesystem safety, collision resistance |
| `TestWriteReadMetadata` | Round-trip `writeMetadata`/`readMetadata` |
| `TestCreateArtifactWithMimeType` | `createArtifact` writes `metadata.json` with correct MIME, uses slugified paths |
| `TestCreateArtifactDefaultMimeType` | Absent `mime_type` defaults to `application/zip` |
| `TestDownloadArtifactContentType` | Response has correct `Content-Type` and `Content-Disposition` with original filename |
| `TestListArtifactsNewLayout` | Enumerates artifacts, returns original names from metadata |
| `TestUploadRawFile` | Raw file stored at correct slugified path in `content/` |
| `TestArtifactNameWithSpecialChars` | Names with spaces, parens, unicode slugify correctly |

### 5.2 Integration tests

Add workflow fixtures under `pkg/artifacts/testdata/artifacts/`:

1. **`zip-upload.yml`** — uses `actions/upload-artifact@v4` to upload a zip archive (existing behaviour, regression test).
2. **`raw-upload.yml`** — uses `actions/upload-artifact@v7` with `archive: false` to upload a raw file, then `actions/download-artifact@v8` to download it. Verifies the downloaded file matches the original content and is not zip-wrapped.

Wire both into `TestArtifactFlow` in `server_test.go` alongside the existing test cases.

### 5.3 Proto verification

After regenerating `artifact.pb.go`, verify:
- `go vet ./pkg/artifacts/...` passes
- `go build ./pkg/artifacts/...` passes
- Existing tests still pass (`go test ./pkg/artifacts/...`)

## 6. Implementation order

Work is sequenced to keep the tree green at each step:

| Step | Description | Files | Depends on |
|---|---|---|---|
| 1 | Rename: drop `V4` suffix from types, functions, constants; rename `artifacts_v4.go` → `artifacts.go` | `artifacts.go`, `server.go` | — |
| 2 | Update `artifact.proto`, add `generate.go`, regenerate `.pb.go` | `artifact.proto`, `generate.go`, `artifact.pb.go` | — |
| 3 | Add `artifactMetadata` struct, `writeMetadata`/`readMetadata` helpers, and `slugify` function | `artifacts.go` | — |
| 4 | Unit tests for metadata helpers and slugify | `artifacts_test.go` or `server_test.go` | 3 |
| 5 | Update `createArtifact` to write `metadata.json` and new directory layout | `artifacts.go` | 2, 3 |
| 6 | Update `uploadArtifact` to write to `content/<filename>` | `artifacts.go` | 5 |
| 7 | Update `downloadArtifact` to serve correct `Content-Type` and `Content-Disposition` | `artifacts.go` | 3 |
| 8 | Update `listArtifacts` to enumerate new layout | `artifacts.go` | 3 |
| 9 | Update signed URL generation to include filename parameter | `artifacts.go` | 5, 6 |
| 10 | Unit tests for handler changes | `artifacts_test.go` or `server_test.go` | 5–9 |
| 11 | Integration tests with `testdata/artifacts/` fixtures | `server_test.go`, `testdata/artifacts/*.yml` | 5–9 |

## 7. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Proto regeneration changes field ordering or Go types | Build breakage, wire incompatibility | Diff generated output carefully; verify field numbers match upstream |
| `protoc` version mismatch with CI | CI build failure | Document required `protoc` version; check `go generate` in CI |
| Legacy artifact directories break with new path scanning | Existing tests fail | No fallback — clean break. Users delete existing state. Document in release notes |
| Filename in signed URL introduces path traversal | Security vulnerability | All paths use `slugify` — user-controlled strings never appear directly in filesystem paths |
| `archive: false` not available in pinned `upload-artifact` version | Integration test fails | Pin to `upload-artifact@v7` which supports `archive: false` |
| Rename breakage from dropping V4 suffix | Compilation errors | Mechanical rename; verify with `go build ./...` before further changes |

## 8. Out of scope

- **`outdir` extraction**: The finalize handler does not perform extraction today. Adding outdir-aware extraction for raw files is deferred (see EC-7 in spec).
- **Multi-file raw uploads**: `archive: false` uploads a single file. Multi-file raw upload support is not part of this change.
- **Compression negotiation**: No `Accept-Encoding` / `Content-Encoding` negotiation is added for raw file downloads.
- **V1 artifact routes**: Only the Twirp-based handlers are affected. V1 routes remain unchanged.
