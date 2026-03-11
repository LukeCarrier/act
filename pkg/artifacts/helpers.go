package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	// SlugHashLen is the length of the SHA256 hash prefix in a slug
	SlugHashLen = 64
	// SlugSuffixMaxLen is the maximum length of the human-readable suffix in a slug
	SlugSuffixMaxLen = 32
)

// ArtifactMetadata stores artifact metadata in metadata.json
type ArtifactMetadata struct {
	ArtifactName      string `json:"artifact_name"`
	OriginalFilename  string `json:"original_filename"`
	MimeType          string `json:"mime_type"`
	CreatedAt         string `json:"created_at"`
	WorkflowRunID     string `json:"workflow_run_id"`
	WorkflowJobRunID  string `json:"workflow_job_run_id"`
}

// Slugify converts an artifact name to a slug: <64-char-hash>-<readable-suffix>
// The hash prefix enables deterministic directory construction.
// The suffix preserves human readability for debugging.
func Slugify(input string) string {
	// Compute SHA256 hash of the input
	hash := sha256.Sum256([]byte(input))
	hashHex := hex.EncodeToString(hash[:])

	// Create human-readable suffix: convert to lowercase, keep alphanumeric + dots/hyphens
	suffix := ""
	for _, r := range strings.ToLower(input) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' {
			suffix += string(r)
		} else if unicode.IsSpace(r) {
			suffix += "-"
		}
	}

	// Truncate suffix to max 32 characters to keep paths reasonable
	if len(suffix) > SlugSuffixMaxLen {
		suffix = suffix[:SlugSuffixMaxLen]
	}

	// If suffix is empty, use a default
	if suffix == "" {
		suffix = "artifact"
	}

	return fmt.Sprintf("%s-%s", hashHex, suffix)
}

// WriteMetadata writes artifact metadata to metadata.json in the artifact directory
func WriteMetadata(baseDir string, metadata ArtifactMetadata) error {
	metadataPath := filepath.Join(baseDir, "metadata.json")

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(metadataPath), os.ModePerm); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// ReadMetadata reads artifact metadata from metadata.json
func ReadMetadata(baseDir string) (ArtifactMetadata, error) {
	metadataPath := filepath.Join(baseDir, "metadata.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return ArtifactMetadata{}, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata ArtifactMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return ArtifactMetadata{}, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return metadata, nil
}
