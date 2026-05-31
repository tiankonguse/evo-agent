package session

import (
	"fmt"
	"os"
	"path/filepath"
)

// SessionsDirName is the subdirectory under the project's .evo-agent/ where
// per-session transcripts live.
const SessionsDirName = ".evo-agent/sessions"

// MessagesFile is the basename of the main transcript file inside a session
// directory.
const MessagesFile = "messages.jsonl"

// MetaFile is the basename of the lightweight metadata sidecar.
const MetaFile = "meta.json"

// SubagentDirName is the per-session subdirectory holding subagent transcripts.
const SubagentDirName = "subagent"

// Session represents the on-disk presence of a single agent run.
//
// A Session is created at process start (either fresh or as a resume target)
// and lives for the lifetime of the process. The transcript is append-only;
// once the process exits, the file is never modified again.
type Session struct {
	ID           string
	Dir          string // absolute path to .evo-agent/sessions/<id>/
	MessagesPath string
	MetaPath     string
	SubagentDir  string

	ProjectDir   string // cfg.ProjectDir
	AgentVersion string
	GitBranch    string

	Recorder *Recorder
}

// NewSession creates a fresh session directory and writes a session_start
// record. The returned Session is ready for AppendMessage calls.
func NewSession(projectDir, agentVersion string) (*Session, error) {
	id := NewSessionID()
	return openSession(id, projectDir, agentVersion, true /* writeStart */)
}

// AdoptSession creates the on-disk presence for a given session id without
// writing a session_start record. Used when an external orchestrator already
// owns the id.
func AdoptSession(id, projectDir, agentVersion string) (*Session, error) {
	return openSession(id, projectDir, agentVersion, false)
}

func openSession(id, projectDir, agentVersion string, writeStart bool) (*Session, error) {
	dir := filepath.Join(projectDir, SessionsDirName, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("session mkdir: %w", err)
	}
	s := &Session{
		ID:           id,
		Dir:          dir,
		MessagesPath: filepath.Join(dir, MessagesFile),
		MetaPath:     filepath.Join(dir, MetaFile),
		SubagentDir:  filepath.Join(dir, SubagentDirName),
		ProjectDir:   projectDir,
		AgentVersion: agentVersion,
		GitBranch:    currentGitBranch(projectDir),
	}
	s.Recorder = newRecorder(s)

	if writeStart {
		// Write a session_start record so the file is non-empty even if the
		// user quits before sending a message — and so it captures the
		// initial git branch / cwd snapshot.
		rec := s.Recorder.baseRecord(TypeSessionStart, "")
		if err := s.Recorder.writeRecord(s.MessagesPath, rec); err != nil {
			return nil, err
		}
	}
	return s, nil
}
