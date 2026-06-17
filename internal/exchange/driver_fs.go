package exchange

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/itaywol/adeptability/internal/fsutil"
)

// boardFileName is the single JSON file the fs driver persists into dataDir.
const boardFileName = "board.json"

// openFSStore loads (or initializes) a board at dataDir and returns a Store
// that writes through to <dataDir>/board.json after every mutation, reusing
// fsutil's atomic write so a crash mid-write can't corrupt the board.
func openFSStore(dataDir string) (Store, error) {
	if dataDir == "" {
		return nil, errors.New("exchange fs driver: empty data dir")
	}
	path := filepath.Join(dataDir, boardFileName)

	var seed *snapshot
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var s snapshot
		if uerr := json.Unmarshal(data, &s); uerr != nil {
			return nil, fmt.Errorf("exchange fs driver: parse %s: %w", path, uerr)
		}
		seed = &s
	case errors.Is(err, fs.ErrNotExist):
		// Fresh board.
	default:
		return nil, fmt.Errorf("exchange fs driver: read %s: %w", path, err)
	}

	w := fsutil.NewWriter()
	m := newMemStore(seed)
	m.persist = func(s snapshot) error {
		b, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return fmt.Errorf("exchange fs driver: encode board: %w", err)
		}
		return w.AtomicWrite(path, b, 0o600)
	}
	return m, nil
}
