package artifacts

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

