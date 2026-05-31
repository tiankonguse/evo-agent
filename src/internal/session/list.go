package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// SessionListEntry is the shape returned to the resume picker.
type SessionListEntry struct {
	ID           string
	Created      int64 // unix milliseconds
	Updated      int64
	TotalInput   int64
	TotalOutput  int64
	MessageCount int
	FirstPrompt  string
	GitBranch    string
}

// TotalTokens returns the sum of input + output tokens recorded so far.
func (e SessionListEntry) TotalTokens() int64 {
	return e.TotalInput + e.TotalOutput
}

// ListSessions walks .evo-agent/sessions/ and returns one entry per session
// directory, newest first. Sessions without a meta.json are still surfaced
// (using the directory id), so a crash mid-write still shows up in /resume.
func ListSessions(projectDir string) []SessionListEntry {
	root := filepath.Join(projectDir, SessionsDirName)
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	entries := make([]SessionListEntry, 0, len(dirs))
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		id := d.Name()
		entry := SessionListEntry{ID: id}

		metaPath := filepath.Join(root, id, MetaFile)
		if data, err := os.ReadFile(metaPath); err == nil {
			var meta MetaData
			if err := json.Unmarshal(data, &meta); err == nil {
				entry.Created = meta.Created
				entry.Updated = meta.Updated
				entry.TotalInput = meta.TotalInput
				entry.TotalOutput = meta.TotalOutput
				entry.MessageCount = meta.MessageCount
				entry.FirstPrompt = meta.FirstPrompt
				entry.GitBranch = meta.GitBranch
			}
		}

		// Fall back to the timestamp embedded in the id when meta is missing.
		if entry.Created == 0 {
			if ms := ParseLeadingTimestampMs(id); ms > 0 {
				entry.Created = ms
				entry.Updated = ms
			}
		}
		entries = append(entries, entry)
	}

	// Sort newest first.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Updated > entries[j].Updated
	})
	return entries
}

// ParseLeadingTimestampMs extracts the unix-millisecond value from the
// numeric prefix of a session id (or subagent filename) of the form
// "<unix_ms>_<rest>". Returns 0 if the prefix is not numeric.
//
// Exposed for the TUI session-picker, which needs the same parser without
// duplicating the format.
func ParseLeadingTimestampMs(id string) int64 {
	var n int64
	for _, r := range id {
		if r >= '0' && r <= '9' {
			n = n*10 + int64(r-'0')
			continue
		}
		break
	}
	return n
}
