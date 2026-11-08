package alarmserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPruneExpiredAttachmentFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	old := filepath.Join(dir, "old.jpg")
	oldState := statePath(old)
	active := filepath.Join(dir, "active.part")
	activeState := statePath(active)
	for _, path := range []string{old, oldState, active, activeState} {
		require.NoError(t, os.WriteFile(path, []byte("data"), 0o644))
	}
	past := now.Add(-8 * 24 * time.Hour)
	for _, path := range []string{old, oldState, active, activeState} {
		require.NoError(t, os.Chtimes(path, past, past))
	}

	server := NewServer(Config{Dir: dir, Retention: 7 * 24 * time.Hour})
	defer server.Stop()
	server.assembler.mu.Lock()
	server.assembler.files["active"] = &fileState{partPath: active}
	server.assembler.mu.Unlock()

	server.pruneExpiredFiles(now)

	_, err := os.Stat(old)
	require.True(t, os.IsNotExist(err), "expired attachment file must be pruned")
	_, err = os.Stat(oldState)
	require.True(t, os.IsNotExist(err), "expired attachment state must be pruned")
	_, err = os.Stat(active)
	require.NoError(t, err, "file under assembly must be kept")
	_, err = os.Stat(activeState)
	require.NoError(t, err, "state of active file must be kept")
}

func TestPruneExpiredAttachmentsDisabled(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.jpg")
	require.NoError(t, os.WriteFile(old, []byte("data"), 0o644))
	past := time.Now().Add(-8 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(old, past, past))

	server := NewServer(Config{Dir: dir})
	defer server.Stop()
	server.pruneExpiredFiles(time.Now())

	_, err := os.Stat(old)
	require.NoError(t, err)
}
