package session

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthropics/anthropic-sdk-go"
)

// SubagentRecorder writes a subagent's transcript to the per-session
// subagent/ directory.
//
// Subagent records use the same envelope as the main transcript but live in
// their own sidechain file so the parent transcript stays linear (only
// subagent_start / subagent_end markers there). UUIDs are not deduplicated
// against the parent — fork-style continuations remain readable.
type SubagentRecorder struct {
	sess     *Session
	AgentNm  string
	Filename string // basename within sess.SubagentDir
	Path     string // absolute path
}

// NewSubagentRecorder creates the subagent transcript file and returns a
// recorder bound to it. The returned `Filename` is the basename used for
// the subagent_start marker in the parent transcript.
func NewSubagentRecorder(sess *Session, agentName string) (*SubagentRecorder, error) {
	if err := os.MkdirAll(sess.SubagentDir, 0o755); err != nil {
		return nil, fmt.Errorf("subagent mkdir: %w", err)
	}
	filename := NewSubagentFilename(agentName)
	path := filepath.Join(sess.SubagentDir, filename)

	sr := &SubagentRecorder{
		sess:     sess,
		AgentNm:  agentName,
		Filename: filename,
		Path:     path,
	}

	// Write a leading session_start so the file is well-formed even if the
	// subagent crashes before producing output.
	rec := sr.baseRecord(TypeSessionStart, "")
	rec.AgentName = agentName
	rec.SubagentPath = filename
	if err := sr.writeRecord(rec); err != nil {
		return nil, err
	}
	return sr, nil
}

// AppendUser appends a user-role message to the subagent transcript.
func (s *SubagentRecorder) AppendUser(promptID string, msg anthropic.MessageParam) {
	s.appendMessage(TypeUser, promptID, msg, 0, 0)
}

// AppendAssistant appends an assistant-role message + tokens to the subagent
// transcript.
func (s *SubagentRecorder) AppendAssistant(promptID string, msg anthropic.MessageParam, in, out int64) {
	s.appendMessage(TypeAssistant, promptID, msg, in, out)
}

func (s *SubagentRecorder) appendMessage(t, promptID string, msg anthropic.MessageParam, in, out int64) {
	rec := s.baseRecord(t, promptID)
	m := msg
	rec.Message = &m
	rec.InputTokens = in
	rec.OutputTokens = out
	if err := s.writeRecord(rec); err != nil {
		fmt.Fprintf(os.Stderr, "[session] subagent append %s failed: %v\n", t, err)
	}
}

func (s *SubagentRecorder) baseRecord(t, promptID string) Record {
	return Record{
		Type:         t,
		Timestamp:    NowISO(),
		AgentVersion: s.sess.AgentVersion,
		SessionID:    s.sess.ID,
		Cwd:          s.sess.ProjectDir,
		PromptID:     promptID,
		GitBranch:    s.sess.GitBranch,
		AgentName:    s.AgentNm,
		SubagentPath: s.Filename,
	}
}

func (s *SubagentRecorder) writeRecord(rec Record) error {
	// Reuse the parent recorder's writer logic.
	return s.sess.Recorder.writeRecord(s.Path, rec)
}
