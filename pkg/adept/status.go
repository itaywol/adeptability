package adept

// Status is the per-skill sync state.
type Status string

const (
	StatusSynced      Status = "synced"
	StatusAhead       Status = "ahead"
	StatusBehind      Status = "behind"
	StatusDiverged    Status = "diverged"
	StatusLocalOnly   Status = "local-only"
	StatusLibraryOnly Status = "library-only"
)
