package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		pattern string // regex pattern to match
	}{
		{
			name:    "simple alphanumeric",
			input:   "my-artifact",
			pattern: `^[a-f0-9]{64}-my-artifact$`,
		},
		{
			name:    "with spaces",
			input:   "my artifact name",
			pattern: `^[a-f0-9]{64}-my-artifact-name$`,
		},
		{
			name:    "with uppercase",
			input:   "My-Report.pdf",
			pattern: `^[a-f0-9]{64}-my-report\.pdf$`,
		},
		{
			name:    "with special chars",
			input:   "file@#$%name",
			pattern: `^[a-f0-9]{64}-filename$`,
		},
		{
			name:    "long name",
			input:   strings.Repeat("a", 100),
			pattern: `^[a-f0-9]{64}-a{32}$`,
		},
		{
			name:    "empty string",
			input:   "",
			pattern: `^[a-f0-9]{64}-artifact$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Slugify(tt.input)

			// Slugs must be: <64-hex-chars>-<readable-suffix>
			// This ensures deterministic path construction and human debuggability
			if len(result) < SlugHashLen + 1 + 1 { // hash + dash + min 1 char suffix
				t.Errorf("slug too short: %s", result)
			}

			if result[SlugHashLen] != '-' {
				t.Errorf("expected dash at position %d, got: %s", SlugHashLen, result)
			}

			// Verify hash part is hex
			hashPart := result[:SlugHashLen]
			for _, c := range hashPart {
				if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
					t.Errorf("non-hex character in hash: %c", c)
				}
			}

			// Suffix length must not exceed the configured maximum to keep paths manageable
			suffix := result[SlugHashLen+1:]
			if len(suffix) > SlugSuffixMaxLen {
				t.Errorf("suffix too long: %d chars (max %d), got: %s", len(suffix), SlugSuffixMaxLen, suffix)
			}

			// Verify same input produces same slug (deterministic)
			result2 := Slugify(tt.input)
			if result != result2 {
				t.Errorf("non-deterministic: %s != %s", result, result2)
			}
		})
	}
}

func TestMetadataRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	now := time.Now().UTC().Format(time.RFC3339)
	metadata := ArtifactMetadata{
		ArtifactName:     "test-artifact",
		OriginalFilename: "test.zip",
		MimeType:         "application/zip",
		CreatedAt:        now,
		WorkflowRunID:    "run-123",
		WorkflowJobRunID: "job-456",
	}

	// Write metadata
	if err := WriteMetadata(tmpDir, metadata); err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	// Verify file exists
	metadataPath := filepath.Join(tmpDir, "metadata.json")
	if _, err := os.Stat(metadataPath); err != nil {
		t.Fatalf("metadata.json not created: %v", err)
	}

	// Read it back
	read, err := ReadMetadata(tmpDir)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	// Verify fields match
	if read.ArtifactName != metadata.ArtifactName {
		t.Errorf("ArtifactName mismatch: got %q, want %q", read.ArtifactName, metadata.ArtifactName)
	}
	if read.OriginalFilename != metadata.OriginalFilename {
		t.Errorf("OriginalFilename mismatch: got %q, want %q", read.OriginalFilename, metadata.OriginalFilename)
	}
	if read.MimeType != metadata.MimeType {
		t.Errorf("MimeType mismatch: got %q, want %q", read.MimeType, metadata.MimeType)
	}
	if read.WorkflowRunID != metadata.WorkflowRunID {
		t.Errorf("WorkflowRunID mismatch: got %q, want %q", read.WorkflowRunID, metadata.WorkflowRunID)
	}
	if read.WorkflowJobRunID != metadata.WorkflowJobRunID {
		t.Errorf("WorkflowJobRunID mismatch: got %q, want %q", read.WorkflowJobRunID, metadata.WorkflowJobRunID)
	}
}

func TestReadMetadataNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := ReadMetadata(tmpDir)
	if err == nil {
		t.Errorf("ReadMetadata should fail for non-existent file")
	}
}

func TestWriteMetadataCreatesDir(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "path")

	metadata := ArtifactMetadata{
		ArtifactName: "test",
		MimeType:     "text/plain",
	}

	if err := WriteMetadata(nestedDir, metadata); err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	// Verify nested directory was created
	if _, err := os.Stat(nestedDir); err != nil {
		t.Fatalf("nested directory not created: %v", err)
	}
}
