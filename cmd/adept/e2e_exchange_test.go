package main_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestE2EExchange drives the full billboard loop through the real binary:
// serve (fs driver) → register → submit → list --mine → respond → show → close.
func TestE2EExchange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e under -short")
	}

	repoRoot := findRepoRoot(t)
	binPath := adeptBin(t.TempDir())
	buildBinary(t, repoRoot, binPath)

	home := t.TempDir()
	libRoot := filepath.Join(t.TempDir(), "lib")
	dataDir := filepath.Join(t.TempDir(), "board")
	baseEnv := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"ADEPT_LIBRARY=" + libRoot,
	}

	// Start the server on an ephemeral port and capture its bootstrap token
	// and bound address from stdout.
	serve := exec.Command(binPath, "exchange", "serve", "--addr", "127.0.0.1:0", "--db", "fs", "--data", dataDir)
	serve.Env = baseEnv
	stdout, err := serve.StdoutPipe()
	require.NoError(t, err)
	serve.Stderr = serve.Stdout
	require.NoError(t, serve.Start())
	t.Cleanup(func() { _ = serve.Process.Kill(); _ = serve.Wait() })

	boot, addr := scanServeStartup(t, stdout)
	server := "http://" + addr
	env := append([]string{"ADEPT_EXCHANGE_SERVER=" + server}, baseEnv...)

	run := func(t *testing.T, args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(binPath, args...)
		cmd.Env = env
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		err := cmd.Run()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("run adept %v: %v\nstderr: %s", args, err, errBuf.String())
		}
		return out.String() + errBuf.String(), code
	}

	t.Run("register stores a token", func(t *testing.T) {
		out, code := run(t, "exchange", "register", "--bootstrap", boot, "--handle", "alice")
		require.Equal(t, 0, code, out)
		require.FileExists(t, filepath.Join(libRoot, "exchange", "127.0.0.1_"+portOf(addr)+".json"))
	})

	var itemID int
	t.Run("submit returns an item", func(t *testing.T) {
		out, code := run(t, "--json", "exchange", "submit", "--title", "how does sync work", "--body", "asking", "--assignee", "alice")
		require.Equal(t, 0, code, out)
		var item struct {
			ID     int    `json:"id"`
			Status string `json:"status"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &item))
		require.Equal(t, "attention-required", item.Status)
		itemID = item.ID
	})

	t.Run("list --mine shows the request", func(t *testing.T) {
		out, code := run(t, "--json", "exchange", "list", "--mine")
		require.Equal(t, 0, code, out)
		var items []struct {
			ID int `json:"id"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &items))
		require.Len(t, items, 1)
		require.Equal(t, itemID, items[0].ID)
	})

	t.Run("respond auto-flips to in-progress", func(t *testing.T) {
		out, code := run(t, "--json", "exchange", "respond", strconv.Itoa(itemID), "--body", "read the orchestrator")
		require.Equal(t, 0, code, out)
		var item struct {
			Status   string `json:"status"`
			Comments []struct {
				Body string `json:"body"`
			} `json:"comments"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &item))
		require.Equal(t, "in-progress", item.Status)
		require.Len(t, item.Comments, 1)
	})

	t.Run("close moves it to closed", func(t *testing.T) {
		out, code := run(t, "--json", "exchange", "close", strconv.Itoa(itemID))
		require.Equal(t, 0, code, out)
		var item struct {
			Status string `json:"status"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &item))
		require.Equal(t, "closed", item.Status)
	})
}

// scanServeStartup reads serve's stdout until it has both the bootstrap token
// and the bound address, or times out.
func scanServeStartup(t *testing.T, r interface{ Read([]byte) (int, error) }) (boot, addr string) {
	t.Helper()
	type res struct{ boot, addr string }
	done := make(chan res, 1)
	go func() {
		sc := bufio.NewScanner(r)
		var b, a string
		grabNext := false
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			switch {
			case grabNext:
				b = line
				grabNext = false
			case strings.Contains(line, "bootstrap token"):
				grabNext = true
			case strings.Contains(line, "billboard serving on "):
				rest := strings.TrimPrefix(line, "billboard serving on ")
				a, _, _ = strings.Cut(rest, " ")
			}
			if b != "" && a != "" {
				done <- res{b, a}
				return
			}
		}
		if err := sc.Err(); err != nil {
			return // EOF/read error: let the select time out with a clear failure
		}
	}()
	select {
	case r := <-done:
		return r.boot, r.addr
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for exchange serve startup")
		return "", ""
	}
}

func portOf(addr string) string {
	_, port, _ := strings.Cut(addr, ":")
	return port
}
