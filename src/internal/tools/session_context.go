package tools

import "sync"

// session_context.go — process-wide setter for the active session's
// directory + id, so tools (like bg_run) that need to write under
// .evo-agent/sessions/<id>/ can find them at runtime.
//
// The agent loop has the session bound to its `*Agent` via AttachSession,
// but tool handlers are dispatched from the registry without any context
// argument. This mirrors the same problem solved by SetConversationMessages
// in memory.go — a tiny mutex-protected setter, called once at session
// startup (and once per --resume swap) by main.go.

var (
	sessionMu  sync.RWMutex
	sessionDir string
	sessionID  string
)

// SetSessionContext records the active session's absolute directory and id.
// Pass empty strings to clear (e.g. when persistence is disabled).
func SetSessionContext(dir, id string) {
	sessionMu.Lock()
	sessionDir, sessionID = dir, id
	sessionMu.Unlock()
}

// CurrentSessionDir returns the active session directory, or "" if none.
func CurrentSessionDir() string {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	return sessionDir
}

// CurrentSessionID returns the active session id, or "" if none.
func CurrentSessionID() string {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	return sessionID
}
