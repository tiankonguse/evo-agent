package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: invoke read_file via the registered handler so we exercise the same
// JSON path the agent loop uses.
func callReadFile(t *testing.T, in ReadFileInput) (string, error) {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	def, ok := registry["read_file"]
	if !ok {
		t.Fatal("read_file not registered")
	}
	return def.Handler(raw)
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestReadFile_BasicCatN(t *testing.T) {
	path := writeTempFile(t, "hello.txt", "alpha\nbeta\ngamma\n")
	out, err := callReadFile(t, ReadFileInput{FilePath: path})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// cat -n format: "%6d\t%s\n"
	want := "     1\talpha\n     2\tbeta\n     3\tgamma\n"
	if out != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

func TestReadFile_OffsetLimit(t *testing.T) {
	path := writeTempFile(t, "lines.txt", "a\nb\nc\nd\ne\n")
	out, err := callReadFile(t, ReadFileInput{FilePath: path, Offset: 2, Limit: 2})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "     2\tb\n") || !strings.Contains(out, "     3\tc\n") {
		t.Errorf("offset/limit window wrong:\n%s", out)
	}
	if strings.Contains(out, "     1\t") || strings.Contains(out, "     4\t") {
		t.Errorf("offset/limit leaked outside window:\n%s", out)
	}
}

func TestReadFile_EmptyFileWarning(t *testing.T) {
	path := writeTempFile(t, "empty.txt", "")
	out, err := callReadFile(t, ReadFileInput{FilePath: path})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "the file exists but the contents are empty") {
		t.Errorf("expected empty warning, got: %s", out)
	}
}

func TestReadFile_OffsetOutOfRange(t *testing.T) {
	path := writeTempFile(t, "short.txt", "only-line\n")
	out, err := callReadFile(t, ReadFileInput{FilePath: path, Offset: 50})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "shorter than the provided offset") {
		t.Errorf("expected oor warning, got: %s", out)
	}
	if !strings.Contains(out, "1 lines") {
		t.Errorf("expected total lines in warning, got: %s", out)
	}
}

func TestReadFile_NotFoundSuggestion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	missing := filepath.Join(dir, "config.yml") // typo
	_, err := callReadFile(t, ReadFileInput{FilePath: missing})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("expected similar-name suggestion, got: %v", err)
	}
}

func TestReadFile_Directory(t *testing.T) {
	dir := t.TempDir()
	_, err := callReadFile(t, ReadFileInput{FilePath: dir})
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected directory error, got: %v", err)
	}
}

func TestReadFile_BinaryExtensionRefused(t *testing.T) {
	path := writeTempFile(t, "blob.zip", "any")
	_, err := callReadFile(t, ReadFileInput{FilePath: path})
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Errorf("expected binary refusal, got: %v", err)
	}
}

func TestReadFile_BlockedDevice(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero not available")
	}
	_, err := callReadFile(t, ReadFileInput{FilePath: "/dev/zero"})
	if err == nil || !strings.Contains(err.Error(), "block or produce infinite output") {
		t.Errorf("expected device block, got: %v", err)
	}
}

func TestReadFile_DedupSameRangeUnchangedMTime(t *testing.T) {
	path := writeTempFile(t, "stable.txt", "one\ntwo\n")
	out1, err := callReadFile(t, ReadFileInput{FilePath: path})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if !strings.Contains(out1, "one") {
		t.Fatalf("first read missing content: %s", out1)
	}
	out2, err := callReadFile(t, ReadFileInput{FilePath: path})
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if !strings.Contains(out2, "File unchanged since last read") {
		t.Errorf("expected dedup stub on second read, got: %s", out2)
	}
}

func TestReadFile_DedupInvalidatedAfterEdit(t *testing.T) {
	path := writeTempFile(t, "mut.txt", "before\n")
	if _, err := callReadFile(t, ReadFileInput{FilePath: path}); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if _, err := runEditFile(path, "before", "after"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	out, err := callReadFile(t, ReadFileInput{FilePath: path})
	if err != nil {
		t.Fatalf("post-edit read: %v", err)
	}
	if strings.Contains(out, "File unchanged") {
		t.Errorf("dedup should be invalidated after edit, but got: %s", out)
	}
	if !strings.Contains(out, "after") {
		t.Errorf("expected post-edit content, got: %s", out)
	}
}

func TestReadFile_DedupInvalidatedAfterWrite(t *testing.T) {
	path := writeTempFile(t, "mut2.txt", "v1\n")
	if _, err := callReadFile(t, ReadFileInput{FilePath: path}); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if _, err := runWriteFile(path, "v2\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := callReadFile(t, ReadFileInput{FilePath: path})
	if err != nil {
		t.Fatalf("post-write read: %v", err)
	}
	if strings.Contains(out, "File unchanged") {
		t.Errorf("dedup should be invalidated after write, got: %s", out)
	}
	if !strings.Contains(out, "v2") {
		t.Errorf("expected post-write content, got: %s", out)
	}
}

func TestReadFile_CRLFNormalized(t *testing.T) {
	path := writeTempFile(t, "crlf.txt", "one\r\ntwo\r\n")
	out, err := callReadFile(t, ReadFileInput{FilePath: path})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("CR should be stripped from cat -n output: %q", out)
	}
}

func TestReadFile_DefaultLineCap(t *testing.T) {
	// Build a 2500-line file; default cap is 2000.
	var b strings.Builder
	for i := 0; i < 2500; i++ {
		b.WriteString("line\n")
	}
	path := writeTempFile(t, "many.txt", b.String())
	out, err := callReadFile(t, ReadFileInput{FilePath: path})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Last visible line number must be ≤ 2000.
	if !strings.Contains(out, "  2000\tline\n") {
		t.Errorf("expected line 2000 to appear, missing")
	}
	if strings.Contains(out, "  2001\t") {
		t.Errorf("line 2001 should not appear without explicit limit, got it")
	}
}

func TestReadFile_RelativePathResolved(t *testing.T) {
	// Sanity: relative path is resolved against cwd.
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.WriteFile("rel.txt", []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	out, err := callReadFile(t, ReadFileInput{FilePath: "rel.txt"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("relative path should resolve, got: %s", out)
	}
}
