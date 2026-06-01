package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// Renderable describes what every command output produces. The CLI picks the
// concrete renderer based on --json.
type Renderable interface {
	JSON() any
	Plain(w io.Writer) error
}

// Print writes the renderable to w using the global --json flag.
func (d *Deps) Print(w io.Writer, r Renderable) error {
	if d.Flags != nil && d.Flags.JSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r.JSON())
	}
	return r.Plain(w)
}

// PrintError emits an error in the configured format.
func (d *Deps) PrintError(w io.Writer, err error) {
	if d.Flags != nil && d.Flags.JSON {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	fmt.Fprintln(w, err)
}

// NewTabWriter returns a tabwriter pre-configured for human plain output.
func NewTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
}
