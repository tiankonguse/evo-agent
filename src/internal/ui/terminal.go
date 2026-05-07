package ui

import "fmt"

// ANSI color codes
const (
	ColorReset   = "\033[0m"
	ColorGreen   = "\033[32m"
	ColorCyan    = "\033[36m"
	ColorBlue    = "\033[34m"
	ColorYellow  = "\033[33m"
	ColorMagenta = "\033[35m"
	ColorRed     = "\033[31m"
)

func PrintThinking(text string) {
	fmt.Printf("%sTHINKING: %s%s\n", ColorGreen, text, ColorReset)
}

func PrintText(text string) {
	fmt.Printf("%sTEXT: %s%s\n", ColorCyan, text, ColorReset)
}

func PrintToolCall(name string) {
	fmt.Printf("%sDEBUG: Tool called: %s%s\n", ColorBlue, name, ColorReset)
}

func PrintCommand(cmd string) {
	fmt.Printf("%s$ %s%s\n", ColorYellow, cmd, ColorReset)
}

func PrintError(msg string) {
	fmt.Printf("%s%s%s\n", ColorRed, msg, ColorReset)
}
