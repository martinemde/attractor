package pipeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactStore_StoreAndRetrieveInMemory(t *testing.T) {
	store := NewArtifactStore("")

	data := map[string]any{
		"key":   "value",
		"count": 42,
	}

	info, err := store.Store("artifact-1", "test artifact", data)
	require.NoError(t, err)

	assert.Equal(t, "artifact-1", info.ID)
	assert.Equal(t, "test artifact", info.Name)
	assert.False(t, info.IsFileBacked)
	assert.Greater(t, info.SizeBytes, 0)
	assert.WithinDuration(t, time.Now(), info.StoredAt, time.Second)

	// Retrieve
	retrieved, err := store.Retrieve("artifact-1")
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)
}

func TestArtifactStore_StoreAndRetrieveFileBacked(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewArtifactStore(tmpDir)
	store.SetThreshold(10) // Very low threshold to force file backing

	data := map[string]any{
		"large": "this is a larger string that exceeds the threshold",
		"more":  "even more data to make it larger",
	}

	info, err := store.Store("large-artifact", "big data", data)
	require.NoError(t, err)

	assert.Equal(t, "large-artifact", info.ID)
	assert.True(t, info.IsFileBacked)

	// Retrieve
	retrieved, err := store.Retrieve("large-artifact")
	require.NoError(t, err)

	// JSON round-trip converts to map[string]interface{} with float64 numbers
	retrievedMap, ok := retrieved.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, data["large"], retrievedMap["large"])
	assert.Equal(t, data["more"], retrievedMap["more"])
}

func TestArtifactStore_HasReturnsTrueForStored(t *testing.T) {
	store := NewArtifactStore("")

	_, err := store.Store("exists", "test", "data")
	require.NoError(t, err)

	assert.True(t, store.Has("exists"))
}

func TestArtifactStore_HasReturnsFalseForMissing(t *testing.T) {
	store := NewArtifactStore("")

	assert.False(t, store.Has("does-not-exist"))
}

func TestArtifactStore_ListReturnsAllArtifacts(t *testing.T) {
	store := NewArtifactStore("")

	_, err := store.Store("id1", "artifact 1", "data1")
	require.NoError(t, err)
	_, err = store.Store("id2", "artifact 2", "data2")
	require.NoError(t, err)
	_, err = store.Store("id3", "artifact 3", "data3")
	require.NoError(t, err)

	infos := store.List()
	assert.Len(t, infos, 3)

	ids := make(map[string]bool)
	for _, info := range infos {
		ids[info.ID] = true
	}
	assert.True(t, ids["id1"])
	assert.True(t, ids["id2"])
	assert.True(t, ids["id3"])
}

func TestArtifactStore_RemoveDeletesArtifact(t *testing.T) {
	store := NewArtifactStore("")

	_, err := store.Store("to-remove", "test", "data")
	require.NoError(t, err)
	assert.True(t, store.Has("to-remove"))

	err = store.Remove("to-remove")
	require.NoError(t, err)

	assert.False(t, store.Has("to-remove"))
}

func TestArtifactStore_RemoveDeletesFileBackedArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewArtifactStore(tmpDir)
	store.SetThreshold(10)

	_, err := store.Store("file-backed", "test", "this is a large string to force file backing")
	require.NoError(t, err)
	assert.True(t, store.Has("file-backed"))

	err = store.Remove("file-backed")
	require.NoError(t, err)

	assert.False(t, store.Has("file-backed"))
}

func TestArtifactStore_RemoveReturnsErrorForMissing(t *testing.T) {
	store := NewArtifactStore("")

	err := store.Remove("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "artifact not found")
}

func TestArtifactStore_ClearRemovesAll(t *testing.T) {
	store := NewArtifactStore("")

	_, err := store.Store("id1", "artifact 1", "data1")
	require.NoError(t, err)
	_, err = store.Store("id2", "artifact 2", "data2")
	require.NoError(t, err)

	store.Clear()

	assert.False(t, store.Has("id1"))
	assert.False(t, store.Has("id2"))
	assert.Empty(t, store.List())
}

func TestArtifactStore_ClearRemovesFileBackedArtifacts(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewArtifactStore(tmpDir)
	store.SetThreshold(10)

	_, err := store.Store("fb1", "file backed 1", "large data string 1 that exceeds threshold")
	require.NoError(t, err)
	_, err = store.Store("fb2", "file backed 2", "large data string 2 that exceeds threshold")
	require.NoError(t, err)

	store.Clear()

	assert.False(t, store.Has("fb1"))
	assert.False(t, store.Has("fb2"))
	assert.Empty(t, store.List())
}

func TestArtifactStore_FileBackingThresholdWorksCorrectly(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewArtifactStore(tmpDir)
	store.SetThreshold(50) // 50 bytes

	// Small data should be in-memory
	smallInfo, err := store.Store("small", "small", "tiny")
	require.NoError(t, err)
	assert.False(t, smallInfo.IsFileBacked)

	// Large data should be file-backed
	largeData := "this is a much larger string that definitely exceeds fifty bytes threshold"
	largeInfo, err := store.Store("large", "large", largeData)
	require.NoError(t, err)
	assert.True(t, largeInfo.IsFileBacked)
}

func TestArtifactStore_RetrieveReturnsErrorForMissing(t *testing.T) {
	store := NewArtifactStore("")

	_, err := store.Retrieve("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "artifact not found")
}

func TestArtifactStore_DefaultThreshold(t *testing.T) {
	// Verify default threshold is 100KB
	assert.Equal(t, DefaultFileBackingThreshold, 100*1024)
}

func TestArtifactStore_StoreComplexData(t *testing.T) {
	store := NewArtifactStore("")

	data := map[string]any{
		"nested": map[string]any{
			"key": "value",
		},
		"array": []any{1, 2, 3},
		"bool":  true,
		"nil":   nil,
	}

	_, err := store.Store("complex", "complex artifact", data)
	require.NoError(t, err)

	retrieved, err := store.Retrieve("complex")
	require.NoError(t, err)

	retrievedMap := retrieved.(map[string]any)
	assert.NotNil(t, retrievedMap["nested"])
	assert.NotNil(t, retrievedMap["array"])
	assert.Equal(t, true, retrievedMap["bool"])
}
