package agent

import "testing"

func TestParseAgentsCmd_BareInvocation(t *testing.T) {
	if act, _ := ParseAgentsCmd("/agents"); act != AgentsCmdList {
		t.Errorf("/agents: want List, got %d", act)
	}
}

func TestParseAgentsCmd_List(t *testing.T) {
	if act, _ := ParseAgentsCmd("/agents list"); act != AgentsCmdList {
		t.Errorf("/agents list: want List, got %d", act)
	}
}

func TestParseAgentsCmd_Reload(t *testing.T) {
	if act, _ := ParseAgentsCmd("/agents reload"); act != AgentsCmdReload {
		t.Errorf("/agents reload: want Reload, got %d", act)
	}
}

func TestParseAgentsCmd_Show(t *testing.T) {
	act, arg := ParseAgentsCmd("/agents show code-reviewer")
	if act != AgentsCmdShow {
		t.Errorf("/agents show <name>: want Show, got %d", act)
	}
	if arg != "code-reviewer" {
		t.Errorf("arg: want %q, got %q", "code-reviewer", arg)
	}
}

func TestParseAgentsCmd_ShowMissingName(t *testing.T) {
	if act, _ := ParseAgentsCmd("/agents show"); act != AgentsCmdNotMatched {
		t.Errorf("/agents show (no name): want NotMatched, got %d", act)
	}
	if act, _ := ParseAgentsCmd("/agents show "); act != AgentsCmdNotMatched {
		t.Errorf("/agents show \"\": want NotMatched, got %d", act)
	}
}

func TestParseAgentsCmd_LeadingTrailingWhitespace(t *testing.T) {
	if act, _ := ParseAgentsCmd("  /agents  "); act != AgentsCmdList {
		t.Errorf("whitespace-padded /agents: want List, got %d", act)
	}
}

func TestParseAgentsCmd_NotMatched(t *testing.T) {
	for _, q := range []string{
		"/agent",          // singular — different command
		"/agents-foo",     // not a delimiter match
		"/agentsxyz",      // glued suffix
		"hello /agents",   // not at start
		"/tools",          // unrelated
		"",                // empty
		"agents",          // missing slash
		"/agents unknown", // unknown subcommand
	} {
		if act, _ := ParseAgentsCmd(q); act != AgentsCmdNotMatched {
			t.Errorf("ParseAgentsCmd(%q): want NotMatched, got %d", q, act)
		}
	}
}
