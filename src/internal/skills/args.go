package skills

// ParseArgs splits a raw argument string using shell-style quoting.
// Supports double-quoted strings to preserve spaces within arguments.
// Example: `"hello world" second` → ["hello world", "second"]
func ParseArgs(raw string) []string {
	var args []string
	var current []byte
	inQuote := false

	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		switch {
		case ch == '"' && !inQuote:
			inQuote = true
		case ch == '"' && inQuote:
			inQuote = false
		case ch == '\\' && inQuote && i+1 < len(raw) && raw[i+1] == '"':
			// Escaped quote inside quoted string
			current = append(current, '"')
			i++ // skip the escaped quote
		case ch == ' ' && !inQuote:
			if len(current) > 0 {
				args = append(args, string(current))
				current = current[:0]
			}
		default:
			current = append(current, ch)
		}
	}
	if len(current) > 0 {
		args = append(args, string(current))
	}
	return args
}
