package pipeline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultFileBackingThreshold is the size threshold above which artifacts are file-backed.
// Artifacts larger than this (in bytes) are written to disk rather than kept in memory.
const DefaultFileBackingThreshold = 100 * 1024 // 100KB

// ArtifactInfo contains metadata about a stored artifact.
type ArtifactInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	SizeBytes    int       `json:"size_bytes"`
	StoredAt     time.Time `json:"stored_at"`
	IsFileBacked bool      `json:"is_file_backed"`
}

// artifactEntry holds the artifact info and its data (in-memory or file path).
type artifactEntry struct {
	Info ArtifactInfo
	Data any // in-memory data or file path string for file-backed
}

// ArtifactStore provides named, typed storage for large stage outputs.
// It supports both in-memory and file-backed storage based on size thresholds.
type ArtifactStore struct {
	mu        sync.RWMutex
	artifacts map[string]artifactEntry
	baseDir   string // filesystem directory for file-backed artifacts
	threshold int    // file backing threshold in bytes
}

// NewArtifactStore creates a new ArtifactStore with the given base directory.
// If baseDir is empty, file-backed storage is disabled.
func NewArtifactStore(baseDir string) *ArtifactStore {
	return &ArtifactStore{
		artifacts: make(map[string]artifactEntry),
		baseDir:   baseDir,
		threshold: DefaultFileBackingThreshold,
	}
}

// SetThreshold sets the file backing threshold in bytes.
// Artifacts larger than this size will be stored on disk.
func (s *ArtifactStore) SetThreshold(threshold int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threshold = threshold
}

// Store saves an artifact with the given ID, name, and data.
// Large artifacts are automatically file-backed if a baseDir is configured.
func (s *ArtifactStore) Store(id, name string, data any) (*ArtifactInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Calculate size by JSON encoding
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	size := len(jsonData)

	// Determine if we should file-back this artifact
	isFileBacked := size > s.threshold && s.baseDir != ""

	var storedData any
	if isFileBacked {
		// Write to file
		artifactDir := filepath.Join(s.baseDir, "artifacts")
		if err := os.MkdirAll(artifactDir, 0755); err != nil {
			return nil, err
		}

		filePath := filepath.Join(artifactDir, id+".json")
		if err := os.WriteFile(filePath, jsonData, 0644); err != nil {
			return nil, err
		}
		storedData = filePath
	} else {
		storedData = data
	}

	info := ArtifactInfo{
		ID:           id,
		Name:         name,
		SizeBytes:    size,
		StoredAt:     time.Now(),
		IsFileBacked: isFileBacked,
	}

	s.artifacts[id] = artifactEntry{
		Info: info,
		Data: storedData,
	}

	return &info, nil
}

// Retrieve returns the data for the given artifact ID.
// For file-backed artifacts, it reads from disk and unmarshals the JSON.
func (s *ArtifactStore) Retrieve(id string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.artifacts[id]
	if !ok {
		return nil, errors.New("artifact not found: " + id)
	}

	if entry.Info.IsFileBacked {
		// Read from file
		filePath, ok := entry.Data.(string)
		if !ok {
			return nil, errors.New("invalid file path for artifact: " + id)
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}

		var result any
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, err
		}
		return result, nil
	}

	return entry.Data, nil
}

// Has returns true if an artifact with the given ID exists.
func (s *ArtifactStore) Has(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.artifacts[id]
	return ok
}

// List returns metadata for all stored artifacts.
func (s *ArtifactStore) List() []ArtifactInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ArtifactInfo, 0, len(s.artifacts))
	for _, entry := range s.artifacts {
		result = append(result, entry.Info)
	}
	return result
}

// Remove deletes an artifact by ID.
// For file-backed artifacts, this also deletes the file.
func (s *ArtifactStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.artifacts[id]
	if !ok {
		return errors.New("artifact not found: " + id)
	}

	// Delete file if file-backed
	if entry.Info.IsFileBacked {
		filePath, ok := entry.Data.(string)
		if ok {
			_ = os.Remove(filePath) // Ignore error if file doesn't exist
		}
	}

	delete(s.artifacts, id)
	return nil
}

// Clear removes all artifacts from the store.
// For file-backed artifacts, this also deletes the files.
func (s *ArtifactStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Delete all file-backed artifacts
	for _, entry := range s.artifacts {
		if entry.Info.IsFileBacked {
			if filePath, ok := entry.Data.(string); ok {
				_ = os.Remove(filePath)
			}
		}
	}

	s.artifacts = make(map[string]artifactEntry)
}
