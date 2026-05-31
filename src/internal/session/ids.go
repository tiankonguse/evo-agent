package session

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// isoLayout is the human-readable timestamp format used inside record
// fields (Record.Timestamp). Renders local wall-clock time with an explicit
// numeric timezone offset, e.g. `2026-05-31T19:39:16.191+08:00`.
//
// We intentionally use local time (not UTC) so the timestamps in the file
// match what the user sees on their clock, and we always emit the numeric
// offset (rather than `Z`) so the absolute moment is still recoverable —
// the result is fully ISO-8601 compliant and round-trips through any
// standard parser.
//
// Note: this is the *content* format. Session ids and subagent filenames
// use unix-milliseconds instead — see NewSessionID — because numeric
// prefixes are easier to parse and avoid the ':' character entirely
// (which is reserved on some filesystems).
const isoLayout = "2006-01-02T15:04:05.000-07:00"

// idSep separates the numeric millisecond timestamp prefix from the UUID
// suffix in a session id (and from the agent slug in a subagent filename).
const idSep = "_"

// NowISO returns the current local wall-clock time formatted with the
// numeric offset, e.g. `2026-05-31T19:39:16.191+08:00`. Used for
// Record.Timestamp.
func NowISO() string {
	return time.Now().Format(isoLayout)
}

// NewSessionID returns a session id of the form "<unix_ms>_<8 hex>", e.g.
//
//	1780225167937_a3f9b2c4
//
// The numeric prefix preserves chronological ordering under lexical sort so
// `os.ReadDir` on the sessions directory returns entries newest-last without
// an explicit sort.
func NewSessionID() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 10) + idSep + randHex(4)
}

// NewPromptID returns a short prompt id used to group records that belong to
// the same user turn.
func NewPromptID() string {
	return "p_" + randHex(4)
}

// NewSubagentFilename returns the filename for a subagent transcript:
// "<unix_ms>_<slugified_name>_<8 hex>.jsonl".
func NewSubagentFilename(name string) string {
	slug := slugify(name)
	if slug == "" {
		slug = "task"
	}
	return strconv.FormatInt(time.Now().UnixMilli(), 10) + idSep + slug + idSep + randHex(4) + ".jsonl"
}

// randHex returns 2*nBytes hex characters from crypto/rand. Falls back to
// time-based bytes on the (extremely unlikely) error.
func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		// fallback: nanosecond time bytes
		ns := time.Now().UnixNano()
		for i := 0; i < nBytes; i++ {
			b[i] = byte(ns >> (i * 8))
		}
	}
	return hex.EncodeToString(b)
}

// slugify turns a free-form agent name into a filesystem-safe slug.
// Keeps [a-z0-9-_], lowercases letters, collapses runs to single dashes,
// caps length at 24.
func slugify(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '_':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == ' ' || r == '/' || r == '.':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
		if b.Len() >= 24 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}
