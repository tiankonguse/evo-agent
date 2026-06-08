package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDisabledRoundTrip exercises the persistence path: SetDisabled writes
// disabled_tools.json under .evo-agent/, ResetDisabled clears it, and a
// fresh LoadDisabled rehydrates the in-memory set. This is the contract
// the /tools picker relies on — toggling Space must survive a restart.
func TestDisabledRoundTrip(t *testing.T) {
	defer resetDisabledForTest(t)
	dir := t.TempDir()
	LoadDisabled(dir)

	if IsDisabled("bash") {
		t.Fatal("clean slate: nothing should be disabled yet")
	}
	if err := SetDisabled("bash", true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	if !IsDisabled("bash") {
		t.Error("after SetDisabled(bash, true), IsDisabled(bash) should be true")
	}

	// Wipe in-memory state and re-load to prove the file persisted it.
	resetDisabledForTest(t)
	LoadDisabled(dir)
	if !IsDisabled("bash") {
		t.Error("after reload, bash should still be disabled")
	}

	// Reset clears both memory and disk.
	if err := ResetDisabled(); err != nil {
		t.Fatalf("ResetDisabled: %v", err)
	}
	if IsDisabled("bash") {
		t.Error("after ResetDisabled, nothing should be disabled")
	}

	// File should now be an empty array (so subsequent SetDisabled
	// doesn't have to recreate the parent dir).
	data, err := os.ReadFile(filepath.Join(dir, ".evo-agent", disabledToolsFilename))
	if err != nil {
		t.Fatalf("read after reset: %v", err)
	}
	if string(data) != "[]\n" {
		t.Errorf("file after reset = %q; want \"[]\\n\"", string(data))
	}
}

// TestToolsFilteringHidesDisabled is the core behavior guard: Tools()
// must omit any tool whose name appears in the disable set, including
// from the slice handed to ToolsExcept (used by subagents and teammates).
// If this regresses, the model would still see disabled tools in its
// schema and the whole feature is moot.
func TestToolsFilteringHidesDisabled(t *testing.T) {
	defer resetDisabledForTest(t)
	dir := t.TempDir()
	LoadDisabled(dir)

	// Pick any registered tool — bash is guaranteed to exist.
	const target = "bash"
	if !hasToolNamed(target) {
		t.Skip("no built-in tools registered; can't exercise filter")
	}

	if !hasInTools(target) {
		t.Fatalf("Tools() should include %s before disabling", target)
	}
	if err := SetDisabled(target, true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	if hasInTools(target) {
		t.Errorf("Tools() still returned %s after disable", target)
	}
	if hasInExcept(target) {
		t.Errorf("ToolsExcept() still returned %s after disable", target)
	}

	// Re-enable and confirm it comes back.
	if err := SetDisabled(target, false); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if !hasInTools(target) {
		t.Errorf("Tools() should include %s again after enable", target)
	}
}

// TestAllToolEntriesShape sanity-checks the picker feed: every entry must
// have a name and a source ("builtin" or "mcp:..."). The picker can't
// render a row with empty fields, and the source label is what the user
// reads to decide what's safe to disable.
func TestAllToolEntriesShape(t *testing.T) {
	defer resetDisabledForTest(t)
	entries := AllToolEntries()
	if len(entries) == 0 {
		t.Skip("no tools registered")
	}
	for _, e := range entries {
		if e.Name == "" {
			t.Errorf("entry has empty name: %+v", e)
		}
		if e.Source != "builtin" && len(e.Source) < 4 {
			t.Errorf("entry source unexpected: %+v", e)
		}
	}
}

// resetDisabledForTest wipes package state so tests don't leak into each
// other. Holding the lock is enough — no goroutines are touching it
// during the test except via the public API.
func resetDisabledForTest(t *testing.T) {
	t.Helper()
	disabledMu.Lock()
	disabled = map[string]bool{}
	disabledFile = ""
	disabledMu.Unlock()
}

func hasToolNamed(name string) bool {
	disabledMu.RLock()
	defer disabledMu.RUnlock()
	_, ok := registry[name]
	return ok
}

func hasInTools(name string) bool {
	for _, t := range Tools() {
		if t.OfTool != nil && t.OfTool.Name == name {
			return true
		}
	}
	return false
}

func hasInExcept(name string) bool {
	for _, t := range ToolsExcept("__never_match__") {
		if t.OfTool != nil && t.OfTool.Name == name {
			return true
		}
	}
	return false
}
