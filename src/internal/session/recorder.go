package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// Recorder writes append-only records to the main session transcript and
// keeps the meta.json sidecar in sync.
//
// Concurrency: the agent loop is single-threaded per process today, but the
// task tool spawns subagents on the same goroutine that may write start/end
// markers around child execution. A small mutex keeps the writes atomic so
// a future move to true concurrency does not silently corrupt a line.
type Recorder struct {
	sess *Session
	mu   sync.Mutex
	meta MetaData
}

// MetaData is what we surface in /resume's session list. It is written to
// meta.json after every append so the picker can read it cheaply.
type MetaData struct {
	ID           string `json:"id"`
	Created      int64  `json:"created_ms"`
	Updated      int64  `json:"updated_ms"`
	AgentVersion string `json:"agent_version"`
	Cwd          string `json:"cwd"`
	GitBranch    string `json:"git_branch,omitempty"`
	FirstPrompt  string `json:"first_prompt,omitempty"`
	TotalInput   int64  `json:"total_input_tokens"`
	TotalOutput  int64  `json:"total_output_tokens"`
	MessageCount int    `json:"message_count"`
}

func newRecorder(s *Session) *Recorder {
	r := &Recorder{
		sess: s,
		meta: MetaData{
			ID:           s.ID,
			Created:      time.Now().UnixMilli(),
			AgentVersion: s.AgentVersion,
			Cwd:          s.ProjectDir,
			GitBranch:    s.GitBranch,
		},
	}
	r.meta.Updated = r.meta.Created
	r.flushMeta()
	return r
}

// AppendUser appends a user-role MessageParam to the transcript.
func (r *Recorder) AppendUser(promptID string, msg anthropic.MessageParam) {
	r.appendMessage(TypeUser, promptID, msg, 0, 0)
}

// AppendAssistant appends an assistant-role MessageParam plus token usage.
func (r *Recorder) AppendAssistant(promptID string, msg anthropic.MessageParam, in, out int64) {
	r.appendMessage(TypeAssistant, promptID, msg, in, out)
}

func (r *Recorder) appendMessage(t, promptID string, msg anthropic.MessageParam, in, out int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec := r.baseRecord(t, promptID)
	m := msg
	rec.Message = &m
	rec.InputTokens = in
	rec.OutputTokens = out
	if err := r.writeRecord(r.sess.MessagesPath, rec); err != nil {
		// Persistence is best-effort: we surface the error on stderr but do
		// not fail the agent loop. A broken disk shouldn't kill the chat.
		fmt.Fprintf(os.Stderr, "[session] append %s failed: %v\n", t, err)
		return
	}

	// Update meta sidecar (kept numeric for cheap sort + arithmetic).
	r.meta.Updated = time.Now().UnixMilli()
	r.meta.TotalInput += in
	r.meta.TotalOutput += out
	if t == TypeUser || t == TypeAssistant {
		r.meta.MessageCount++
	}
	if r.meta.FirstPrompt == "" && t == TypeUser {
		r.meta.FirstPrompt = firstUserText(msg)
	}
	r.flushMeta()
}

// AppendCompactBoundary marks the spot in the transcript where context was
// compacted. Resume reads up to (and uses) the most recent boundary's summary.
func (r *Recorder) AppendCompactBoundary(promptID, summary string, count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.baseRecord(TypeCompactBoundary, promptID)
	rec.Summary = summary
	rec.CompactCount = count
	if err := r.writeRecord(r.sess.MessagesPath, rec); err != nil {
		fmt.Fprintf(os.Stderr, "[session] append compact_boundary failed: %v\n", err)
	}
}

// AppendResumeMarker records that this session was started by restoring an
// older session.
func (r *Recorder) AppendResumeMarker(fromSessionID string, restored int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.baseRecord(TypeResumeMarker, "")
	rec.FromSessionID = fromSessionID
	rec.RestoredCount = restored
	if err := r.writeRecord(r.sess.MessagesPath, rec); err != nil {
		fmt.Fprintf(os.Stderr, "[session] append resume_marker failed: %v\n", err)
	}
}

// AppendSubagentStart writes a placeholder in the parent transcript pointing
// at the subagent's sidechain file. The actual messages live in that file.
func (r *Recorder) AppendSubagentStart(promptID, agentName, subagentPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.baseRecord(TypeSubagentStart, promptID)
	rec.AgentName = agentName
	rec.SubagentPath = subagentPath
	if err := r.writeRecord(r.sess.MessagesPath, rec); err != nil {
		fmt.Fprintf(os.Stderr, "[session] append subagent_start failed: %v\n", err)
	}
}

// AppendSubagentEnd closes a subagent block in the parent transcript.
func (r *Recorder) AppendSubagentEnd(promptID, agentName, subagentPath, result string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.baseRecord(TypeSubagentEnd, promptID)
	rec.AgentName = agentName
	rec.SubagentPath = subagentPath
	rec.Result = result
	if err := r.writeRecord(r.sess.MessagesPath, rec); err != nil {
		fmt.Fprintf(os.Stderr, "[session] append subagent_end failed: %v\n", err)
	}
}

// baseRecord populates the common envelope fields.
func (r *Recorder) baseRecord(t, promptID string) Record {
	return Record{
		Type:         t,
		Timestamp:    NowISO(),
		AgentVersion: r.sess.AgentVersion,
		SessionID:    r.sess.ID,
		Cwd:          r.sess.ProjectDir,
		PromptID:     promptID,
		GitBranch:    r.sess.GitBranch,
	}
}

// writeRecord opens the target file in append mode and writes one JSON line.
// Each call performs its own open+write+close to keep crash recovery simple
// and avoid holding a long-lived FD for a low-frequency writer.
func (r *Recorder) writeRecord(path string, rec Record) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if _, err := f.WriteString("\n"); err != nil {
		return err
	}
	return nil
}

// flushMeta writes meta.json. Best-effort.
func (r *Recorder) flushMeta() {
	data, err := json.MarshalIndent(r.meta, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(r.sess.MetaPath, data, 0o600)
}

// firstUserText extracts the first text block from a user message, truncated
// to 200 characters. Used to populate meta.FirstPrompt for the resume picker.
func firstUserText(msg anthropic.MessageParam) string {
	for _, blk := range msg.Content {
		if blk.OfText != nil && blk.OfText.Text != "" {
			text := blk.OfText.Text
			if len(text) > 200 {
				text = text[:200]
			}
			return text
		}
	}
	return ""
}
