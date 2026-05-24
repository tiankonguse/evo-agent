package skills

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// indexedArgRe matches $ARGUMENTS[N] placeholders.
var indexedArgRe = regexp.MustCompile(`\$ARGUMENTS\[(\d+)\]`)

// shorthandArgRe matches $N shorthand (e.g. $0, $1) but NOT $ARGUMENTS or $name patterns.
// Must be a single or multi-digit number, not followed by word characters.
var shorthandArgRe = regexp.MustCompile(`\$(\d+)\b`)

// RenderBody substitutes argument placeholders in the skill/command body.
//
// Substitution order:
// 1. $ARGUMENTS[N] → args[N] (or empty if out of bounds)
// 2. $name → named argument from argNames mapped to position
// 3. $N shorthand → args[N]
// 4. $ARGUMENTS → full rawArgs string
// 5. If NO argument placeholder was present in the original body, append "ARGUMENTS: <rawArgs>"
func RenderBody(body string, argNames []string, args []string, rawArgs string) string {
	if rawArgs == "" {
		return body
	}

	// Detect if the body has any kind of argument placeholder
	hasPlaceholder := strings.Contains(body, "$ARGUMENTS") ||
		shorthandArgRe.MatchString(body)
	if !hasPlaceholder {
		for _, name := range argNames {
			if name != "" && strings.Contains(body, "$"+name) {
				hasPlaceholder = true
				break
			}
		}
	}

	// 1. Replace $ARGUMENTS[N]
	body = indexedArgRe.ReplaceAllStringFunc(body, func(match string) string {
		sub := indexedArgRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		idx, err := strconv.Atoi(sub[1])
		if err != nil || idx < 0 || idx >= len(args) {
			return ""
		}
		return args[idx]
	})

	// 2. Replace named arguments ($name where name is in argNames)
	for i, name := range argNames {
		if name == "" {
			continue
		}
		placeholder := "$" + name
		if i < len(args) {
			body = strings.ReplaceAll(body, placeholder, args[i])
		}
	}

	// 3. Replace $N shorthand
	body = shorthandArgRe.ReplaceAllStringFunc(body, func(match string) string {
		sub := shorthandArgRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		idx, err := strconv.Atoi(sub[1])
		if err != nil || idx < 0 || idx >= len(args) {
			return ""
		}
		return args[idx]
	})

	// 4. Replace bare $ARGUMENTS
	body = strings.ReplaceAll(body, "$ARGUMENTS", rawArgs)

	// 5. Fallback: append ARGUMENTS only if NO placeholder was present in original body
	if !hasPlaceholder {
		body = body + fmt.Sprintf("\nARGUMENTS: %s", rawArgs)
	}

	return body
}
