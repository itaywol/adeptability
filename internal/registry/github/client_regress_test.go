package github

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// clientRegressEntry is a single tar entry spec for the in-memory tarball
// builder below. Prefixed to avoid colliding with other tests in-package.
type clientRegressEntry struct {
	name     string
	typeflag byte
	size     int64 // declared header size; -1 means "use len(body)"
	body     []byte
}

// clientRegressTarGz builds a gzipped tar whose entries mimic a GitHub
// tarball (every path is under a top-level wrapper dir). When size is set
// explicitly it overrides the real body length, letting us forge a
// malicious Size header without actually allocating the bytes.
func clientRegressTarGz(t *testing.T, entries []clientRegressEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		size := e.size
		if size < 0 {
			size = int64(len(e.body))
		}
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Size:     size,
			Mode:     0o644,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if len(e.body) > 0 {
			_, err := tw.Write(e.body)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// Path-traversal entries must be rejected, not written to the files map.
func TestExtractSkillDir_RejectsPathTraversal(t *testing.T) {
	tgz := clientRegressTarGz(t, []clientRegressEntry{
		{name: "repo-sha/find-skills/SKILL.md", typeflag: tar.TypeReg, size: -1, body: []byte("# ok")},
		{name: "repo-sha/find-skills/../../../../tmp/pwned", typeflag: tar.TypeReg, size: -1, body: []byte("evil")},
	})
	_, _, err := ExtractSkillDir(bytes.NewReader(tgz), "find-skills", []string{"find-skills"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "escapes skill dir")
}

// An absolute inner path must also be rejected.
func TestExtractSkillDir_RejectsAbsolutePath(t *testing.T) {
	tgz := clientRegressTarGz(t, []clientRegressEntry{
		{name: "repo-sha/find-skills/SKILL.md", typeflag: tar.TypeReg, size: -1, body: []byte("# ok")},
		{name: "repo-sha/find-skills//etc/cron.d/evil", typeflag: tar.TypeReg, size: -1, body: []byte("evil")},
	})
	_, _, err := ExtractSkillDir(bytes.NewReader(tgz), "find-skills", []string{"find-skills"})
	require.Error(t, err)
}

// A forged, gigantic Size header must NOT trigger a huge pre-allocation /
// panic; it must be rejected via the per-file cap. archive/tar.Writer
// refuses to emit a header whose Size doesn't match the body, so we forge
// the raw 512-byte ustar header by hand with an enormous octal size field
// and a tiny body. A naive `make([]byte, hdr.Size)` would blow up here.
func TestExtractSkillDir_RejectsOversizedSizeHeader(t *testing.T) {
	var raw bytes.Buffer
	hdr := make([]byte, 512)
	name := "repo-sha/find-skills/SKILL.md"
	copy(hdr[0:100], name)
	copy(hdr[100:108], []byte("0000644\x00"))     // mode
	copy(hdr[108:116], []byte("0000000\x00"))     // uid
	copy(hdr[116:124], []byte("0000000\x00"))     // gid
	copy(hdr[124:136], []byte("77777777777\x00")) // size: ~8 GiB in octal
	copy(hdr[136:148], []byte("00000000000\x00")) // mtime
	hdr[156] = tar.TypeReg                        // typeflag
	copy(hdr[257:263], []byte("ustar\x00"))       // magic
	copy(hdr[263:265], []byte("00"))              // version
	// checksum: spaces while summing, then octal of the byte sum.
	for i := 148; i < 156; i++ {
		hdr[i] = ' '
	}
	sum := 0
	for _, b := range hdr {
		sum += int(b)
	}
	copy(hdr[148:156], []byte(fmt.Sprintf("%06o\x00 ", sum)))
	raw.Write(hdr)
	raw.WriteString("x")           // tiny real body
	raw.Write(make([]byte, 512-1)) // pad to block
	raw.Write(make([]byte, 1024))  // two zero blocks = EOF

	var gzbuf bytes.Buffer
	gz := gzip.NewWriter(&gzbuf)
	_, err := gz.Write(raw.Bytes())
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	require.NotPanics(t, func() {
		_, _, err := ExtractSkillDir(bytes.NewReader(gzbuf.Bytes()), "find-skills", []string{"find-skills"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "limit")
	})
}

// Non-regular entries (symlinks, dirs, etc.) under the skill dir are
// skipped, not extracted as files.
func TestExtractSkillDir_SkipsNonRegularEntries(t *testing.T) {
	tgz := clientRegressTarGz(t, []clientRegressEntry{
		{name: "repo-sha/find-skills/SKILL.md", typeflag: tar.TypeReg, size: -1, body: []byte("# ok")},
		{name: "repo-sha/find-skills/evil-link", typeflag: tar.TypeSymlink, size: 0, body: nil},
	})
	files, matched, err := ExtractSkillDir(bytes.NewReader(tgz), "find-skills", []string{"find-skills"})
	require.NoError(t, err)
	require.Equal(t, "find-skills", matched)
	require.Len(t, files, 1)
	require.Contains(t, files, "SKILL.md")
	require.NotContains(t, files, "evil-link")
}

// Happy path: nested legitimate files extract with clean relative keys.
func TestExtractSkillDir_HappyPath(t *testing.T) {
	tgz := clientRegressTarGz(t, []clientRegressEntry{
		{name: "repo-sha/find-skills/SKILL.md", typeflag: tar.TypeReg, size: -1, body: []byte("# ok")},
		{name: "repo-sha/find-skills/references/api.md", typeflag: tar.TypeReg, size: -1, body: []byte("api")},
	})
	files, matched, err := ExtractSkillDir(bytes.NewReader(tgz), "find-skills", []string{"find-skills"})
	require.NoError(t, err)
	require.Equal(t, "find-skills", matched)
	require.Equal(t, []byte("# ok"), files["SKILL.md"])
	require.Equal(t, []byte("api"), files["references/api.md"])
}

func TestValidateOwnerRepo(t *testing.T) {
	require.NoError(t, validateOwnerRepo("vercel-labs", "skills"))
	require.NoError(t, validateOwnerRepo("a.b_c", "repo.git"))
	require.Error(t, validateOwnerRepo("", "skills"))
	require.Error(t, validateOwnerRepo("owner", ""))
	require.Error(t, validateOwnerRepo("ow/ner", "skills"))
	require.Error(t, validateOwnerRepo("owner", "re po"))
	require.Error(t, validateOwnerRepo("..", "skills"))
}

func TestValidateRef(t *testing.T) {
	require.NoError(t, validateRef("main"))
	require.NoError(t, validateRef("v1.4.0"))
	require.NoError(t, validateRef("release/2024"))
	require.NoError(t, validateRef("abc123def456"))
	require.Error(t, validateRef(".."))
	require.Error(t, validateRef("../../x"))
	require.Error(t, validateRef("foo bar"))
	require.Error(t, validateRef("-leading-dash"))
	require.Error(t, validateRef(""))
}
