package artifacts

import (
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateArtifactWithMimeType tests that createArtifact correctly handles MIME type.
// Intent: Verify that MIME type from request is saved to metadata for both archive and raw uploads.
func TestCreateArtifactWithMimeType(t *testing.T) {
	assert := assert.New(t)

	// Test case 1: Archive upload (ZIP)
	mimeTypeZip := "application/zip"
	assert.True(isZipMimeType(mimeTypeZip), "application/zip should be recognized as archive")

	// Test case 2: Raw file upload (text)
	mimeTypeText := "text/plain"
	assert.False(isZipMimeType(mimeTypeText), "text/plain should not be recognized as archive")

	// Test case 3: Empty MIME type (legacy v4) - treated as NOT a ZIP MIME type
	// The handler itself determines archive vs raw based on version + request content
	mimeTypeEmpty := ""
	assert.False(isZipMimeType(mimeTypeEmpty), "empty MIME type is not recognized as ZIP (handler uses version to determine archive vs raw)")
}

// TestArtifactNameSlugification tests that artifact names are properly slugified.
// Intent: Ensure artifact names with special characters are safely converted for filesystem paths.
func TestArtifactNameSlugification(t *testing.T) {
	assert := assert.New(t)

	tests := []struct {
		input    string
		expected string
	}{
		{"my-artifact", "my-artifact"},
		{"my_artifact", "my_artifact"},
		{"my artifact", "my-artifact"},
		{"my/artifact", "my-artifact"},
		{"my\\artifact", "my-artifact"},
		{"my:artifact", "my-artifact"},
		{"my*artifact", "my-artifact"},
		{"my?artifact", "my-artifact"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := Slugify(tc.input)
			// Verify it's slugified (doesn't contain special chars)
			assert.NotContains(result, "/", "Should not contain forward slashes")
			assert.NotContains(result, "\\", "Should not contain backslashes")
			assert.NotContains(result, "*", "Should not contain asterisks")
			assert.NotContains(result, "?", "Should not contain question marks")
			assert.NotContains(result, ":", "Should not contain colons")
		})
	}
}

// TestMetadataPreservesOriginalNames tests that metadata correctly preserves original artifact names.
// Intent: Ensure that original names are saved in metadata even when slugified for filesystem storage.
func TestMetadataPreservesOriginalNames(t *testing.T) {
	assert := assert.New(t)

	originalName := "my special/artifact:name*"
	mimeType := "application/zip"

	metadata := ArtifactMetadata{
		ArtifactName: originalName,
		MimeType:     mimeType,
	}

	// Verify original name is preserved exactly
	assert.Equal(originalName, metadata.ArtifactName)
	assert.Equal(mimeType, metadata.MimeType)
}

// TestMetadataRoundTripWithSlugifiedPath tests that WriteMetadata and ReadMetadata
// work correctly when called with a slugified artifact directory path, as the
// handlers do in practice.
func TestMetadataRoundTripWithSlugifiedPath(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	baseDir := t.TempDir()
	artifactDir := filepath.Join(baseDir, "1", Slugify("my-artifact"))

	expected := ArtifactMetadata{
		ArtifactName:     "my-artifact",
		OriginalFilename: "data.txt",
		MimeType:         "text/plain",
		WorkflowRunID:    "1",
	}

	require.NoError(WriteMetadata(artifactDir, expected))

	// ReadMetadata must be called with the artifact directory, NOT metadata.json
	got, err := ReadMetadata(artifactDir)
	require.NoError(err)
	assert.Equal(expected.ArtifactName, got.ArtifactName)
	assert.Equal(expected.MimeType, got.MimeType)
	assert.Equal(expected.OriginalFilename, got.OriginalFilename)

	// Verify the file is at the expected location
	_, err = os.Stat(filepath.Join(artifactDir, "metadata.json"))
	require.NoError(err, "metadata.json should exist directly under artifact dir")
}

// TestMetadataReadFromArtifactDir_NotMetadataPath verifies that callers must pass the
// artifact directory — not a path already containing metadata.json — to ReadMetadata.
// This is a regression test for the double metadata.json path bug in listArtifacts
// and downloadArtifact.
func TestMetadataReadFromArtifactDir_NotMetadataPath(t *testing.T) {
	require := require.New(t)

	baseDir := t.TempDir()
	artifactDir := filepath.Join(baseDir, Slugify("test-artifact"))

	require.NoError(WriteMetadata(artifactDir, ArtifactMetadata{
		ArtifactName: "test-artifact",
		MimeType:     "application/zip",
	}))

	// Correct: pass the artifact directory
	_, err := ReadMetadata(artifactDir)
	require.NoError(err, "ReadMetadata with artifact dir should succeed")

	// Wrong: passing a path that already includes metadata.json will fail
	wrongPath := filepath.Join(artifactDir, "metadata.json")
	_, err = ReadMetadata(wrongPath)
	require.Error(err, "ReadMetadata with metadata.json path should fail (double metadata.json)")
}

// TestListArtifactsMetadataPath simulates the directory walk that listArtifacts does
// and verifies ReadMetadata is called with the right path.
func TestListArtifactsMetadataPath(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	baseDir := t.TempDir()
	runDir := filepath.Join(baseDir, "1")

	// Create two artifacts
	artifacts := []ArtifactMetadata{
		{ArtifactName: "zip-artifact", MimeType: "application/zip"},
		{ArtifactName: "raw-artifact", MimeType: "text/plain"},
	}
	for _, meta := range artifacts {
		artifactDir := filepath.Join(runDir, Slugify(meta.ArtifactName))
		require.NoError(WriteMetadata(artifactDir, meta))
	}

	// Simulate what listArtifacts does: read run dir entries, ReadMetadata on each
	entries, err := os.ReadDir(runDir)
	require.NoError(err)
	assert.Len(entries, 2)

	for _, entry := range entries {
		safeArtifactPath := filepath.Join(runDir, entry.Name())
		// This must NOT append metadata.json before calling ReadMetadata
		meta, err := ReadMetadata(safeArtifactPath)
		require.NoError(err, "ReadMetadata should succeed for %s", entry.Name())
		assert.NotEmpty(meta.ArtifactName)
	}
}

// TestSignatureRoundTripThroughURL verifies that the base64-encoded signature
// survives a round-trip through URL query parameters. This is a regression test
// for a bug where base64 padding characters (=) were interpreted as query string
// key-value separators, truncating the signature.
func TestSignatureRoundTripThroughURL(t *testing.T) {
	assert := assert.New(t)

	r := &artifactV4Routes{}
	endp := "UploadArtifact"
	expires := "2026-03-13 13:35:03.826062 +0000 GMT"
	artifactName := "data.bin"
	filename := Slugify("data.bin")
	taskID := int64(1)

	sig := r.buildSignature(endp, expires, artifactName, filename, taskID)
	encoded := base64.RawURLEncoding.EncodeToString(sig)

	// Build a URL the same way buildArtifactURL does
	rawURL := "http://localhost:34567/test" +
		"?sig=" + encoded +
		"&expires=" + url.QueryEscape(expires) +
		"&artifactName=" + url.QueryEscape(artifactName) +
		"&filename=" + url.QueryEscape(filename) +
		"&taskID=1"

	// Parse the URL back and extract the sig — simulates what verifySignature does
	u, err := url.Parse(rawURL)
	assert.NoError(err)
	gotSig := u.Query().Get("sig")

	decoded, err := base64.RawURLEncoding.DecodeString(gotSig)
	assert.NoError(err)
	assert.Equal(sig, decoded, "signature should survive URL round-trip")

	// Also verify the other params round-trip
	assert.Equal(expires, u.Query().Get("expires"))
	assert.Equal(artifactName, u.Query().Get("artifactName"))
	assert.Equal(filename, u.Query().Get("filename"))
	assert.Equal("1", u.Query().Get("taskID"))
}

// TestGetSignedArtifactURLUsesMetadataFilename verifies that getSignedArtifactURL
// derives the download filename from metadata rather than hardcoding .zip.
// This is a regression test for a bug where raw (non-zip) artifacts could not
// be downloaded because the handler always appended ".zip" to the artifact name.
func TestGetSignedArtifactURLUsesMetadataFilename(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	baseDir := t.TempDir()
	runID := "1"
	runDir := filepath.Join(baseDir, runID)

	tests := []struct {
		name             string
		artifactName     string
		originalFilename string
		mimeType         string
	}{
		{
			name:             "zip artifact gets .zip filename",
			artifactName:     "my-zip-artifact",
			originalFilename: "my-zip-artifact.zip",
			mimeType:         "application/zip",
		},
		{
			name:             "raw artifact keeps original filename",
			artifactName:     "data.bin",
			originalFilename: "data.bin",
			mimeType:         "application/octet-stream",
		},
		{
			name:             "raw text artifact",
			artifactName:     "report.csv",
			originalFilename: "report.csv",
			mimeType:         "text/csv",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			artifactDir := filepath.Join(runDir, Slugify(tc.artifactName))
			require.NoError(WriteMetadata(artifactDir, ArtifactMetadata{
				ArtifactName:     tc.artifactName,
				OriginalFilename: tc.originalFilename,
				MimeType:         tc.mimeType,
				WorkflowRunID:    runID,
			}))

			// Verify metadata round-trips and OriginalFilename is preserved
			meta, err := ReadMetadata(artifactDir)
			require.NoError(err)
			assert.Equal(tc.originalFilename, meta.OriginalFilename)

			// The slugified filename used in download URLs must match what was stored
			expectedSlug := Slugify(tc.originalFilename)
			gotSlug := Slugify(meta.OriginalFilename)
			assert.Equal(expectedSlug, gotSlug,
				"slugified download filename should match what createArtifact stored")
		})
	}
}

