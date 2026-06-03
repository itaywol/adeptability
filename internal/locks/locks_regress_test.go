package locks

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression: Save must create the parent directory if it does not exist.
// Previously Save offloaded this to the caller and a Load+Set+Save before
// .adeptability/ existed failed with an opaque ENOENT on the temp write.
func TestSave_CreatesParentDir_Regress(t *testing.T) {
	root := t.TempDir()
	// .adeptability/ does NOT exist yet.
	path := filepath.Join(root, ".adeptability", FileName)
	l := New()
	l.Set("a", Entry{SHA: "1"})
	require.NoError(t, Save(path, l))

	back, err := Load(path)
	require.NoError(t, err)
	got, ok := back.Get("a")
	require.True(t, ok)
	require.Equal(t, "1", got.SHA)
}

// Regression: concurrent writers must not share a fixed "<path>.tmp" name.
// With a shared temp name, one writer's partial temp could be renamed by
// another, producing a torn or stale lockfile. With os.CreateTemp each
// writer gets a unique temp, so the final file is always a complete,
// valid lockfile and no temp leaks behind.
func TestSave_ConcurrentWritersNoSharedTemp_Regress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	const writers = 16
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(n int) {
			defer wg.Done()
			l := New()
			l.Set("skill", Entry{SHA: strings.Repeat("a", n+1)})
			require.NoError(t, Save(path, l))
		}(i)
	}
	wg.Wait()

	// The surviving file must parse cleanly (never torn) and have exactly
	// the one entry every writer wrote.
	back, err := Load(path)
	require.NoError(t, err)
	require.Len(t, back.External, 1)
	_, ok := back.Get("skill")
	require.True(t, ok)

	// No temp files should be left behind by any writer.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.Equal(t, FileName, e.Name(), "only the lockfile should remain, found stray %q", e.Name())
	}
}
