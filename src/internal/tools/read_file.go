// Package tools — read_file tool.
//
// This tool is a Go re-implementation inspired by Claude Code's official
// FileReadTool (refs/FileReadTool/*.ts). The intent is to bring the same
// production-tested behaviors into evo-agent:
//
//   - cat -n style line-numbered output (line numbers start at 1)
//   - offset/limit range reads (default 2000 lines, 256 KB byte cap)
//   - empty / out-of-range warnings as <system-reminder> blocks
//   - binary-extension and device-file pre-checks (no I/O on /dev/zero etc.)
//   - dedup: same file + same range + unchanged mtime => "file unchanged"
//     stub instead of re-sending the full content (saves cache_creation tokens)
//   - friendly file-not-found error with a similar-filename suggestion
//
// Image / PDF / notebook handling from the original tool is intentionally
// out of scope here: evo-agent's loop is text-only today and these formats
// belong to a separate follow-up. The tool refuses binary extensions with a
// clear message instead. The upstream macOS-screenshot thin-space fallback
// is also dropped — .png is on the binary blocklist and rejected before any
// stat call, so the fallback could never fire.
package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go"
)

// ─── Public input ────────────────────────────────────────────────────────────

// ReadFileInput mirrors the shape of the official FileReadTool input.
//
//	file_path : path to the file (absolute preferred, relative resolved against cwd)
//	offset    : 1-indexed line number to start reading from (0/1 = start)
//	limit     : maximum number of lines to read (0 = use defaultMaxLines)
type ReadFileInput struct {
	FilePath string `json:"file_path" jsonschema_description:"Absolute path to the file to read. Relative paths are resolved against the current working directory."`
	Offset   int    `json:"offset,omitempty" jsonschema_description:"The line number to start reading from (1-indexed). Only provide if the file is too large to read at once."`
	Limit    int    `json:"limit,omitempty" jsonschema_description:"The number of lines to read. Only provide if the file is too large to read at once."`
}

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	// defaultMaxLines mirrors MAX_LINES_TO_READ in the upstream tool.
	defaultMaxLines = 2000
	// maxFileSizeBytes is the hard byte cap when no explicit limit is given.
	// Matches MAX_OUTPUT_SIZE / DEFAULT in claude-code's limits.ts (256 KB).
	maxFileSizeBytes = 256 * 1024
	// readFileMaxOutputBytes caps the final string the model sees, regardless
	// of line count. Keeps the tool result inside one prompt-cache slice.
	readFileMaxOutputBytes = 50000
	// truncatedLineLen mirrors how cat -n handles very long lines: clip at
	// 2000 chars and mark with an ellipsis.
	truncatedLineLen = 2000

	fileUnchangedStub = "File unchanged since last read. The content from the earlier read_file tool_result in this conversation is still current — refer to that instead of re-reading."
	fileNotFoundCwd   = "File does not exist."
)

// blockedDevicePaths are device files whose read would never EOF or would
// block waiting on input. We reject by path with no I/O — safe specials like
// /dev/null are intentionally not in this list.
var blockedDevicePaths = map[string]struct{}{
	"/dev/zero": {}, "/dev/random": {}, "/dev/urandom": {}, "/dev/full": {},
	"/dev/stdin": {}, "/dev/tty": {}, "/dev/console": {},
	"/dev/stdout": {}, "/dev/stderr": {},
	"/dev/fd/0": {}, "/dev/fd/1": {}, "/dev/fd/2": {},
}

// binaryExtensions: refuse to dump bytes the model can't read meaningfully.
// Mirrors hasBinaryExtension() in the upstream constants/files.ts.
var binaryExtensions = map[string]struct{}{
	".exe": {}, ".dll": {}, ".so": {}, ".dylib": {}, ".a": {}, ".o": {},
	".class": {}, ".jar": {}, ".war": {}, ".ear": {},
	".zip": {}, ".tar": {}, ".gz": {}, ".bz2": {}, ".xz": {}, ".7z": {}, ".rar": {},
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}, ".bmp": {}, ".ico": {},
	".pdf": {},
	".mp3": {}, ".mp4": {}, ".wav": {}, ".flac": {}, ".ogg": {}, ".avi": {}, ".mov": {}, ".mkv": {},
	".pyc": {}, ".pyo": {},
	".db": {}, ".sqlite": {}, ".sqlite3": {},
	".bin": {}, ".dat": {},
	".woff": {}, ".woff2": {}, ".ttf": {}, ".otf": {}, ".eot": {},
}

// ─── Read-state dedup ────────────────────────────────────────────────────────
//
// The model often re-asks for a file it already has in context. The first
// tool_result is still on the stack — sending the bytes again only burns
// cache_creation tokens. We track {abs path, mtime, offset, limit} and short-
// circuit identical replays with a tiny stub. mtime change invalidates the
// entry so post-edit reads always see fresh content.

type readStateEntry struct {
	mtime  int64 // file mtime in unix nanoseconds
	offset int
	limit  int
}

var (
	readStateMu sync.Mutex
	readState   = map[string]readStateEntry{}
)

// InvalidateReadState drops any cached read entry for the given absolute
// path. Other tools (edit_file / write_file) should call this after mutating
// a file so the next read_file call reads fresh bytes instead of returning
// the unchanged stub. Safe to call with paths that aren't tracked.
func InvalidateReadState(absPath string) {
	readStateMu.Lock()
	delete(readState, absPath)
	readStateMu.Unlock()
}

// ─── Registration ────────────────────────────────────────────────────────────

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "read_file",
			Description: anthropic.String(
				"Read a file from the local filesystem.\n\n" +
					"Usage:\n" +
					"- file_path should be an absolute path; relative paths resolve against the working directory.\n" +
					fmt.Sprintf("- By default, reads up to %d lines starting at line 1. Files larger than %d KB will be truncated; use offset and limit for larger files.\n", defaultMaxLines, maxFileSizeBytes/1024) +
					"- Provide offset (1-indexed line) and limit when you only need a portion of the file.\n" +
					"- Results are returned in cat -n format, with line numbers starting at 1.\n" +
					"- This tool reads text files only. Binary files, images, PDFs and notebooks are not supported by this tool.\n" +
					"- Reading a file that has not changed since a previous read in this session returns a small 'unchanged' stub instead of the full content.",
			),
			InputSchema: GenerateSchema[ReadFileInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in ReadFileInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return runReadFile(in.FilePath, in.Offset, in.Limit)
		},
	})
}

// ─── Implementation ──────────────────────────────────────────────────────────

func runReadFile(filePath string, offset, limit int) (string, error) {
	if strings.TrimSpace(filePath) == "" {
		return "", errors.New("read_file: file_path is required")
	}

	absPath, err := resolvePath(filePath)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}

	if _, blocked := blockedDevicePaths[absPath]; blocked {
		return "", fmt.Errorf("read_file: cannot read %q: this device file would block or produce infinite output", filePath)
	}
	// /proc/self/fd/{0,1,2} and /proc/<pid>/fd/{0,1,2} are stdio aliases.
	if isProcStdioAlias(absPath) {
		return "", fmt.Errorf("read_file: cannot read %q: this fd alias would block or produce infinite output", filePath)
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	if _, isBin := binaryExtensions[ext]; isBin {
		return "", fmt.Errorf("read_file: refusing to read binary %s file %q — use bash/file-specific tools instead", ext, filePath)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", notFoundError(filePath, absPath)
		}
		return "", fmt.Errorf("read_file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("read_file: %q is a directory — use the bash tool with `ls` instead", filePath)
	}

	return readResolved(filePath, absPath, info, offset, limit)
}

// readResolved performs the actual read once we know the file exists and is
// a regular file. filePath is the original (user-supplied) path used in
// error messages; absPath is the resolved absolute path used for I/O and
// dedup keying.
func readResolved(filePath, absPath string, info os.FileInfo, offset, limit int) (string, error) {
	// Normalize: offset is 1-indexed; treat 0 as "from start".
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}

	// ── Dedup short-circuit ─────────────────────────────────────────────────
	mtime := info.ModTime().UnixNano()
	readStateMu.Lock()
	prev, hadPrev := readState[absPath]
	readStateMu.Unlock()
	if hadPrev && prev.mtime == mtime && prev.offset == offset && prev.limit == limit {
		return fileUnchangedStub, nil
	}

	// ── Empty file ──────────────────────────────────────────────────────────
	if info.Size() == 0 {
		updateReadState(absPath, mtime, offset, limit)
		return "<system-reminder>Warning: the file exists but the contents are empty.</system-reminder>", nil
	}

	// ── Read bytes (capped) ────────────────────────────────────────────────
	// When the caller did not supply a limit, also cap by maxFileSizeBytes
	// to avoid OOM on accidentally-huge files. Explicit limit users opt out
	// of the byte cap so their requested range always lands.
	var data []byte
	var err error
	if limit > 0 || offset > 0 {
		data, err = os.ReadFile(absPath)
	} else {
		data, err = readFileCapped(absPath, maxFileSizeBytes)
	}
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}

	// Best-effort UTF-8 sanity check. Files that aren't valid UTF-8 are
	// likely binary the extension list missed (e.g. .so without a name) —
	// reject rather than spew junk into the model's context.
	if !utf8.Valid(data) && !looksMostlyText(data) {
		return "", fmt.Errorf("read_file: %q does not look like a text file (invalid UTF-8) — refuse to dump binary bytes", filePath)
	}

	// ── Slice by lines ──────────────────────────────────────────────────────
	allLines := splitLinesPreserve(string(data))
	totalLines := len(allLines)

	startIdx := 0
	if offset > 1 {
		startIdx = offset - 1
	}
	startLineNumber := startIdx + 1 // 1-indexed for display

	if startIdx >= totalLines {
		// Out-of-range: this matches the upstream warning verbatim in spirit.
		updateReadState(absPath, mtime, offset, limit)
		return fmt.Sprintf(
			"<system-reminder>Warning: the file exists but is shorter than the provided offset (%d). The file has %d lines.</system-reminder>",
			startLineNumber, totalLines,
		), nil
	}

	effectiveLimit := limit
	if effectiveLimit == 0 {
		effectiveLimit = defaultMaxLines
	}
	endIdx := startIdx + effectiveLimit
	if endIdx > totalLines {
		endIdx = totalLines
	}

	// ── Format with cat -n style line numbers ──────────────────────────────
	var b strings.Builder
	// Pre-size: 7 bytes prefix per line + content + newline.
	b.Grow(len(data) + (endIdx-startIdx)*8)
	for i := startIdx; i < endIdx; i++ {
		line := allLines[i]
		if utf8.RuneCountInString(line) > truncatedLineLen {
			line = clipRunes(line, truncatedLineLen) + "…"
		}
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, line)
	}
	out := b.String()

	// Final byte cap so a single read_file never blows past the tool-result
	// budget. PersistLargeOutput will further offload when persistThreshold
	// is hit downstream.
	if len(out) > readFileMaxOutputBytes {
		out = out[:readFileMaxOutputBytes] + "\n... (output truncated at byte cap)"
	}

	updateReadState(absPath, mtime, offset, limit)
	return out, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// resolvePath expands ~ and resolves relative paths against cwd. Returns
// a cleaned absolute path.
func resolvePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if !filepath.IsAbs(p) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p), nil
}

// readFileCapped reads up to maxBytes from path. Returns whatever was read on
// short files. We deliberately don't fail when the file exceeds the cap —
// upstream behavior prefers a truncated read with a warning over an error.
func readFileCapped(path string, maxBytes int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, maxBytes)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, fs.ErrClosed) && err.Error() != "EOF" {
		// short read on small files yields io.EOF on next call, not first
		if n == 0 {
			return nil, err
		}
	}
	return buf[:n], nil
}

// splitLinesPreserve splits on \n while preserving the (line N has no
// trailing newline if the file ended without one) shape. Empty input ->
// empty slice. We strip trailing \r so Windows files don't render \r in cat -n.
func splitLinesPreserve(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	// If the file ended with \n, Split yields an empty trailing element we
	// don't want to count as a real line.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	for i, p := range parts {
		if strings.HasSuffix(p, "\r") {
			parts[i] = strings.TrimSuffix(p, "\r")
		}
	}
	return parts
}

// clipRunes returns the first n runes of s.
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// looksMostlyText accepts files where ≥ 95 % of bytes are printable / common
// whitespace. Used as a fallback when utf8.Valid is false (some legitimate
// text files are latin-1 or shift-jis but should still be readable).
func looksMostlyText(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	good := 0
	for _, c := range b {
		if c == '\n' || c == '\r' || c == '\t' || (c >= 0x20 && c < 0x7f) {
			good++
		}
	}
	return good*100/len(b) >= 95
}

// updateReadState records the most recent successful read for absPath so the
// next identical call can be deduplicated.
func updateReadState(absPath string, mtime int64, offset, limit int) {
	readStateMu.Lock()
	readState[absPath] = readStateEntry{mtime: mtime, offset: offset, limit: limit}
	readStateMu.Unlock()
}

// notFoundError builds a friendly ENOENT message including a similar-name
// suggestion when one exists in the parent directory.
func notFoundError(userPath, absPath string) error {
	cwd, _ := os.Getwd()
	msg := fmt.Sprintf("read_file: %s Current working directory: %s.", fileNotFoundCwd, cwd)
	if alt := findSimilarFile(absPath); alt != "" {
		msg += fmt.Sprintf(" Did you mean %s?", alt)
	}
	_ = userPath
	return errors.New(msg)
}

// findSimilarFile scans the parent directory for files whose name shares a
// stem prefix with the missing one. Returns the first match (cheap, good
// enough to nudge the model toward the right path).
func findSimilarFile(absPath string) string {
	dir := filepath.Dir(absPath)
	want := strings.ToLower(filepath.Base(absPath))
	wantStem := strings.TrimSuffix(want, filepath.Ext(want))
	if wantStem == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		got := strings.ToLower(e.Name())
		gotStem := strings.TrimSuffix(got, filepath.Ext(got))
		// Prefer exact stem match first.
		if gotStem == wantStem {
			return filepath.Join(dir, e.Name())
		}
	}
	// Loose match: same prefix.
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		got := strings.ToLower(e.Name())
		if strings.HasPrefix(got, wantStem) || strings.HasPrefix(wantStem, strings.TrimSuffix(got, filepath.Ext(got))) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}


// isProcStdioAlias matches /proc/self/fd/{0,1,2} and /proc/<pid>/fd/{0,1,2}.
func isProcStdioAlias(p string) bool {
	if !strings.HasPrefix(p, "/proc/") {
		return false
	}
	switch {
	case strings.HasSuffix(p, "/fd/0"), strings.HasSuffix(p, "/fd/1"), strings.HasSuffix(p, "/fd/2"):
		return true
	}
	return false
}
