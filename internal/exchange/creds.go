package exchange

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/itaywol/adeptability/pkg/adept"
)

// Creds is the on-disk record for one billboard server: the URL, the local
// handle, and the bearer token. Stored at <libRoot>/exchange/<host>.json with
// mode 0600 — a machine-local identity token, like gh/docker keep.
type Creds struct {
	Server string `json:"server"`
	Handle string `json:"handle"`
	Token  string `json:"token"`
}

const (
	defaultPointerFile = "default"
	dismissedFile      = "recommendation-dismissed"
)

// CredStore reads and writes billboard credentials under libRoot.
type CredStore struct {
	dir string
}

// NewCredStore roots a credential store at <libRoot>/exchange.
func NewCredStore(libRoot string) *CredStore {
	return &CredStore{dir: filepath.Join(libRoot, adept.ExchangeDirName)}
}

// hostKey turns a server URL into a filesystem-safe filename stem.
func hostKey(server string) (string, error) {
	u, err := url.Parse(server)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("exchange: invalid server URL %q", server)
	}
	key := strings.NewReplacer(":", "_", "/", "_").Replace(u.Host)
	return key, nil
}

func (s *CredStore) path(server string) (string, error) {
	key, err := hostKey(server)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.dir, key+".json"), nil
}

// Save writes the creds for c.Server (0600) and records it as the default.
func (s *CredStore) Save(c Creds) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("exchange: create creds dir: %w", err)
	}
	path, err := s.path(c.Server)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("exchange: encode creds: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("exchange: write creds: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, defaultPointerFile), []byte(c.Server), 0o600); err != nil {
		return fmt.Errorf("exchange: write default pointer: %w", err)
	}
	return nil
}

// Load returns the creds for server, or fs.ErrNotExist if none are stored.
func (s *CredStore) Load(server string) (Creds, error) {
	path, err := s.path(server)
	if err != nil {
		return Creds{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Creds{}, err
	}
	var c Creds
	if err := json.Unmarshal(b, &c); err != nil {
		return Creds{}, fmt.Errorf("exchange: parse creds %s: %w", path, err)
	}
	return c, nil
}

// DefaultServer returns the most recently saved server URL, or "" if none.
func (s *CredStore) DefaultServer() string {
	b, err := os.ReadFile(filepath.Join(s.dir, defaultPointerFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ResolveServer picks the server URL: flag, then $ADEPT_EXCHANGE_SERVER, then
// the stored default.
func (s *CredStore) ResolveServer(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if env := os.Getenv(adept.ExchangeServerEnvVar); env != "" {
		return env, nil
	}
	if def := s.DefaultServer(); def != "" {
		return def, nil
	}
	return "", errors.New("no exchange server: pass --server, set $ADEPT_EXCHANGE_SERVER, or run `adept exchange register` first")
}

// DismissRecommendation records that the user does not want to be prompted to
// set up the exchange. It is a user-level preference (next to the creds), not
// per-project, so the prompt stays quiet across every repo on this machine.
func (s *CredStore) DismissRecommendation() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("exchange: create creds dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, dismissedFile), []byte("dismissed\n"), 0o600); err != nil {
		return fmt.Errorf("exchange: write dismissal: %w", err)
	}
	return nil
}

// UndismissRecommendation re-enables the setup prompt.
func (s *CredStore) UndismissRecommendation() error {
	err := os.Remove(filepath.Join(s.dir, dismissedFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("exchange: clear dismissal: %w", err)
	}
	return nil
}

// RecommendationDismissed reports whether the user dismissed the setup prompt.
func (s *CredStore) RecommendationDismissed() bool {
	_, err := os.Stat(filepath.Join(s.dir, dismissedFile))
	return err == nil
}

// Registered reports whether a usable token exists for server (env or stored).
func (s *CredStore) Registered(server string) bool {
	if server == "" {
		return false
	}
	tok, err := s.ResolveToken(server)
	return err == nil && tok != ""
}

// ResolveToken picks the bearer token for server: $ADEPT_EXCHANGE_TOKEN, then
// the stored creds.
func (s *CredStore) ResolveToken(server string) (string, error) {
	if env := os.Getenv(adept.ExchangeTokenEnvVar); env != "" {
		return env, nil
	}
	c, err := s.Load(server)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("not registered with %s: run `adept exchange register` or set $%s", server, adept.ExchangeTokenEnvVar)
		}
		return "", err
	}
	return c.Token, nil
}
