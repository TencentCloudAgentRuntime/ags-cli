package repl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/c-bata/go-prompt"
	fileCompleter "github.com/c-bata/go-prompt/completer"
)

var (
	// Command history
	history     []string
	historyFile string

	commands = []prompt.Suggest{
		// Tool commands
		{Text: "tool", Description: "Manage sandbox tools"},
		{Text: "tool list", Description: "List available tools"},
		{Text: "tool get", Description: "Get tool details"},
		{Text: "tool create", Description: "Create a new tool"},
		{Text: "tool update", Description: "Update a tool"},
		{Text: "tool delete", Description: "Delete a tool"},
		{Text: "t", Description: "Alias for tool"},
		{Text: "t list", Description: "List available tools"},
		{Text: "t ls", Description: "List available tools"},
		{Text: "t create", Description: "Create a new tool"},
		{Text: "t update", Description: "Update a tool"},
		{Text: "t delete", Description: "Delete a tool"},
		{Text: "t rm", Description: "Delete a tool"},
		{Text: "t del", Description: "Delete a tool"},

		// Instance commands
		{Text: "instance", Description: "Manage sandbox instances"},
		{Text: "instance create", Description: "Create a new instance"},
		{Text: "instance start", Description: "Start a new instance"},
		{Text: "instance list", Description: "List instances"},
		{Text: "instance get", Description: "Get instance details"},
		{Text: "instance login", Description: "Login to instance via webshell"},
		{Text: "instance delete", Description: "Delete an instance"},
		{Text: "instance stop", Description: "Stop an instance"},
		{Text: "i", Description: "Alias for instance"},
		{Text: "i create", Description: "Create a new instance"},
		{Text: "i start", Description: "Start a new instance"},
		{Text: "i c", Description: "Create a new instance"},
		{Text: "i list", Description: "List instances"},
		{Text: "i ls", Description: "List instances"},
		{Text: "i get", Description: "Get instance details"},
		{Text: "i login", Description: "Login to instance via webshell"},
		{Text: "i delete", Description: "Delete an instance"},
		{Text: "i stop", Description: "Stop an instance"},
		{Text: "i rm", Description: "Delete an instance"},

		// Run command
		{Text: "run", Description: "Execute code in a sandbox"},
		{Text: "run -c", Description: "Execute code string"},
		{Text: "run -f", Description: "Execute code from file(s)"},
		{Text: "run -l", Description: "Specify language"},
		{Text: "run -s", Description: "Stream output in real-time"},
		{Text: "run -n", Description: "Run same code N times concurrently"},
		{Text: "run -p", Description: "Execute multiple tasks in parallel"},
		{Text: "r", Description: "Alias for run"},
		{Text: "r -c", Description: "Execute code string"},
		{Text: "r -f", Description: "Execute code from file(s)"},
		{Text: "r -l", Description: "Specify language"},
		{Text: "r -s", Description: "Stream output in real-time"},
		{Text: "r -n", Description: "Run same code N times concurrently"},
		{Text: "r -p", Description: "Execute multiple tasks in parallel"},

		// Exec command
		{Text: "exec", Description: "Execute shell command in sandbox"},
		{Text: "exec ps", Description: "List running processes"},
		{Text: "x", Description: "Alias for exec"},
		{Text: "x ps", Description: "List running processes"},

		// File commands
		{Text: "file", Description: "File operations in sandbox"},
		{Text: "file list", Description: "List files in directory"},
		{Text: "file ls", Description: "List files in directory"},
		{Text: "file upload", Description: "Upload file to sandbox"},
		{Text: "file download", Description: "Download file from sandbox"},
		{Text: "file cat", Description: "Print file contents"},
		{Text: "file stat", Description: "Get file info"},
		{Text: "file mkdir", Description: "Create directory"},
		{Text: "file remove", Description: "Remove file or directory"},
		{Text: "file rm", Description: "Remove file or directory"},
		{Text: "f", Description: "Alias for file"},
		{Text: "fs", Description: "Alias for file"},

		// API Key commands
		{Text: "apikey", Description: "Manage API keys"},
		{Text: "apikey create", Description: "Create a new API key"},
		{Text: "apikey list", Description: "List API keys"},
		{Text: "apikey delete", Description: "Delete an API key"},
		{Text: "ak", Description: "Alias for apikey"},
		{Text: "ak create", Description: "Create a new API key"},
		{Text: "ak list", Description: "List API keys"},
		{Text: "ak ls", Description: "List API keys"},
		{Text: "ak delete", Description: "Delete an API key"},
		{Text: "ak rm", Description: "Delete an API key"},
		{Text: "ak del", Description: "Delete an API key"},
		{Text: "key", Description: "Alias for apikey"},

		// Browser commands
		{Text: "browser", Description: "Manage browser sandbox"},
		{Text: "browser vnc", Description: "Show VNC URL for browser sandbox"},
		{Text: "b", Description: "Alias for browser"},
		{Text: "b vnc", Description: "Show VNC URL for browser sandbox"},

		// Other commands
		{Text: "help", Description: "Show help"},
		{Text: "history", Description: "Show command history"},
		{Text: "clear", Description: "Clear screen"},
		{Text: "exit", Description: "Exit REPL"},
		{Text: "quit", Description: "Exit REPL"},
	}

	toolSubcommands = []prompt.Suggest{
		{Text: "list", Description: "List available tools"},
		{Text: "ls", Description: "List available tools"},
		{Text: "get", Description: "Get tool details"},
		{Text: "create", Description: "Create a new tool"},
		{Text: "update", Description: "Update a tool"},
		{Text: "delete", Description: "Delete a tool"},
		{Text: "rm", Description: "Delete a tool"},
		{Text: "del", Description: "Delete a tool"},
	}

	instanceSubcommands = []prompt.Suggest{
		{Text: "create", Description: "Create a new instance"},
		{Text: "start", Description: "Start a new instance"},
		{Text: "c", Description: "Create a new instance"},
		{Text: "list", Description: "List instances"},
		{Text: "ls", Description: "List instances"},
		{Text: "get", Description: "Get instance details"},
		{Text: "login", Description: "Login to instance via webshell"},
		{Text: "delete", Description: "Delete an instance"},
		{Text: "stop", Description: "Stop an instance"},
		{Text: "rm", Description: "Delete an instance"},
		{Text: "del", Description: "Delete an instance"},
	}

	runFlags = []prompt.Suggest{
		{Text: "-c", Description: "Code to execute"},
		{Text: "--code", Description: "Code to execute"},
		{Text: "-f", Description: "File(s) containing code (can repeat)"},
		{Text: "--file", Description: "File(s) containing code (can repeat)"},
		{Text: "-l", Description: "Programming language"},
		{Text: "--language", Description: "Programming language"},
		{Text: "-s", Description: "Stream output in real-time"},
		{Text: "--stream", Description: "Stream output in real-time"},
		{Text: "-t", Description: "Tool to use"},
		{Text: "--tool", Description: "Tool to use"},
		{Text: "-i", Description: "Existing instance ID (short form)"},
		{Text: "--instance", Description: "Existing instance ID"},
		{Text: "--keep-alive", Description: "Keep instance alive"},
		{Text: "--time", Description: "Print elapsed time to stderr"},
		{Text: "-n", Description: "Concurrent instances count"},
		{Text: "--repeat", Description: "Run the same code N times"},
		{Text: "-p", Description: "Execute tasks in parallel"},
		{Text: "--parallel", Description: "Execute tasks in parallel"},
		{Text: "--max-parallel", Description: "Max parallel executions (0=unlimited)"},
	}

	languages = []prompt.Suggest{
		{Text: "python", Description: "Python (default)"},
		{Text: "javascript", Description: "JavaScript"},
		{Text: "typescript", Description: "TypeScript"},
		{Text: "bash", Description: "Bash shell"},
		{Text: "r", Description: "R language"},
		{Text: "java", Description: "Java"},
	}

	instanceCreateFlags = []prompt.Suggest{
		{Text: "-t", Description: "Tool name (e2b/cloud backend)"},
		{Text: "-t", Description: "Tool name (short form)"},
		{Text: "--tool-name", Description: "Tool name (e2b/cloud backend)"},
		{Text: "--tool", Description: "Tool name (alias for --tool-name)"},
		{Text: "--tool-id", Description: "Tool ID (cloud backend only)"},
		{Text: "--timeout", Description: "Instance timeout in seconds"},
		{Text: "--mount-option", Description: "Mount option to override tool storage"},
		{Text: "--time", Description: "Print elapsed time to stderr"},
	}

	instanceListFlags = []prompt.Suggest{
		{Text: "--tool-id", Description: "Filter by tool ID"},
		{Text: "-s", Description: "Filter by status"},
		{Text: "--status", Description: "Filter by status"},
		{Text: "--short", Description: "Only show instance IDs"},
		{Text: "--no-header", Description: "Hide table header"},
		{Text: "--offset", Description: "Pagination offset"},
		{Text: "--limit", Description: "Pagination limit"},
		{Text: "--time", Description: "Print elapsed time to stderr"},
	}

	instanceLoginFlags = []prompt.Suggest{
		{Text: "--no-browser", Description: "Don't open browser automatically"},
		{Text: "--ttyd-binary", Description: "Path to custom ttyd binary file to upload"},
		{Text: "--time", Description: "Print elapsed time to stderr"},
	}

	toolListFlags = []prompt.Suggest{
		{Text: "--id", Description: "Specific tool IDs to query"},
		{Text: "--status", Description: "Filter by status"},
		{Text: "--type", Description: "Filter by type"},
		{Text: "--tag", Description: "Filter by tag"},
		{Text: "--created-since", Description: "Filter by relative time"},
		{Text: "--created-since-time", Description: "Filter by absolute time (RFC3339)"},
		{Text: "--short", Description: "Show only ID and NAME"},
		{Text: "--no-header", Description: "Hide table header and footer"},
		{Text: "--offset", Description: "Pagination offset"},
		{Text: "--limit", Description: "Pagination limit"},
		{Text: "--time", Description: "Print elapsed time"},
	}

	toolCreateFlags = []prompt.Suggest{
		{Text: "-n", Description: "Tool name"},
		{Text: "--name", Description: "Tool name"},
		{Text: "-t", Description: "Tool type"},
		{Text: "--type", Description: "Tool type"},
		{Text: "-d", Description: "Tool description"},
		{Text: "--description", Description: "Tool description"},
		{Text: "--timeout", Description: "Default timeout"},
		{Text: "--network", Description: "Network mode"},
		{Text: "--vpc-subnet", Description: "VPC subnet ID (required when --network=VPC)"},
		{Text: "--vpc-sg", Description: "Security group ID (required when --network=VPC)"},
		{Text: "--tag", Description: "Tags in key=value format"},
		{Text: "--role-arn", Description: "CAM Role ARN for COS access"},
		{Text: "--mount", Description: "Storage mount config"},
		{Text: "--time", Description: "Print elapsed time"},
	}

	toolUpdateFlags = []prompt.Suggest{
		{Text: "-d", Description: "Tool description"},
		{Text: "--description", Description: "Tool description"},
		{Text: "--network", Description: "Network mode: PUBLIC, SANDBOX, INTERNAL_SERVICE"},
		{Text: "--tag", Description: "Tags in key=value format"},
		{Text: "--clear-tags", Description: "Clear all tags"},
		{Text: "--time", Description: "Print elapsed time"},
	}

	toolGetFlags = []prompt.Suggest{
		{Text: "--time", Description: "Print elapsed time"},
	}

	toolDeleteFlags = []prompt.Suggest{
		{Text: "--time", Description: "Print elapsed time"},
	}

	apikeySubcommands = []prompt.Suggest{
		{Text: "create", Description: "Create a new API key"},
		{Text: "list", Description: "List API keys"},
		{Text: "ls", Description: "List API keys"},
		{Text: "delete", Description: "Delete an API key"},
		{Text: "rm", Description: "Delete an API key"},
		{Text: "del", Description: "Delete an API key"},
	}

	apikeyCreateFlags = []prompt.Suggest{
		{Text: "-n", Description: "API key name"},
		{Text: "--name", Description: "API key name"},
	}

	// Browser command
	browserSubcommands = []prompt.Suggest{
		{Text: "vnc", Description: "Show VNC URL for browser sandbox"},
	}

	browserVNCFlags = []prompt.Suggest{
		{Text: "-i", Description: "Instance ID to connect to (short form)"},
		{Text: "--instance", Description: "Instance ID to connect to"},
		{Text: "-t", Description: "Tool name for creating new instance"},
		{Text: "-t", Description: "Tool name for creating new instance (short form)"},
		{Text: "--tool-name", Description: "Tool name for creating new instance"},
		{Text: "--tool", Description: "Tool name (alias for --tool-name)"},
		{Text: "--tool-id", Description: "Tool ID (cloud backend only)"},
		{Text: "--timeout", Description: "Instance timeout in seconds"},
		{Text: "-p", Description: "VNC service port"},
		{Text: "--port", Description: "VNC service port"},
		{Text: "--time", Description: "Print elapsed time"},
	}

	// Exec command
	execFlags = []prompt.Suggest{
		{Text: "-s", Description: "Stream output in real-time"},
		{Text: "--stream", Description: "Stream output in real-time"},
		{Text: "-i", Description: "Instance ID to use (short form)"},
		{Text: "-i", Description: "Instance ID to use (short form)"},
		{Text: "--instance", Description: "Instance ID to use"},
		{Text: "-t", Description: "Tool for temporary instance"},
		{Text: "--tool", Description: "Tool for temporary instance"},
		{Text: "--keep-alive", Description: "Keep temporary instance alive"},
		{Text: "--time", Description: "Print elapsed time"},
		{Text: "--cwd", Description: "Working directory"},
		{Text: "--env", Description: "Environment variables (KEY=VALUE)"},
	}

	execSubcommands = []prompt.Suggest{
		{Text: "ps", Description: "List running processes"},
	}

	// File command
	fileSubcommands = []prompt.Suggest{
		{Text: "list", Description: "List files in directory"},
		{Text: "ls", Description: "List files in directory"},
		{Text: "upload", Description: "Upload file to sandbox"},
		{Text: "up", Description: "Upload file to sandbox"},
		{Text: "put", Description: "Upload file to sandbox"},
		{Text: "download", Description: "Download file from sandbox"},
		{Text: "down", Description: "Download file from sandbox"},
		{Text: "get", Description: "Download file from sandbox"},
		{Text: "cat", Description: "Print file contents"},
		{Text: "stat", Description: "Get file info"},
		{Text: "mkdir", Description: "Create directory"},
		{Text: "remove", Description: "Remove file or directory"},
		{Text: "rm", Description: "Remove file or directory"},
		{Text: "del", Description: "Remove file or directory"},
	}

	fileFlags = []prompt.Suggest{
		{Text: "-i", Description: "Instance ID to use (short form)"},
		{Text: "-i", Description: "Instance ID to use (short form)"},
		{Text: "--instance", Description: "Instance ID to use"},
		{Text: "-t", Description: "Tool for temporary instance"},
		{Text: "--tool", Description: "Tool for temporary instance"},
		{Text: "--keep-alive", Description: "Keep temporary instance alive"},
		{Text: "--time", Description: "Print elapsed time"},
		{Text: "--depth", Description: "Directory depth for list"},
	}

	globalFlags = []prompt.Suggest{
		{Text: "--backend", Description: "API backend (e2b or cloud)"},
		{Text: "-o", Description: "Output format (text or json)"},
		{Text: "--output", Description: "Output format (text or json)"},
		{Text: "--cloud-internal", Description: "Use internal endpoints"},
	}
)

func init() {
	// Set up history file path
	home, err := os.UserHomeDir()
	if err == nil {
		historyFile = filepath.Join(home, ".ags_history")
		loadHistory()
	}
}

func loadHistory() {
	if historyFile == "" {
		return
	}
	data, err := os.ReadFile(historyFile)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			history = append(history, line)
		}
	}
	// Keep only last 1000 entries
	if len(history) > 1000 {
		history = history[len(history)-1000:]
	}
}

func saveHistory() {
	if historyFile == "" {
		return
	}
	// Keep only last 1000 entries
	if len(history) > 1000 {
		history = history[len(history)-1000:]
	}
	data := strings.Join(history, "\n")
	_ = os.WriteFile(historyFile, []byte(data), 0600)
}

func addToHistory(cmd string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return
	}
	// Don't add duplicates of the last command
	if len(history) > 0 && history[len(history)-1] == cmd {
		return
	}
	history = append(history, cmd)
	saveHistory()
}

// getPreviousFlag returns the flag before current position
func getPreviousFlag(words []string) string {
	for i := len(words) - 1; i >= 0; i-- {
		if strings.HasPrefix(words[i], "-") {
			return words[i]
		}
	}
	return ""
}

func completer(d prompt.Document) []prompt.Suggest {
	text := d.TextBeforeCursor()
	words := strings.Fields(text)

	if len(words) == 0 {
		return prompt.FilterHasPrefix(commands, "", true)
	}

	// Get the first word (command)
	cmd := words[0]

	// Handle subcommand completion
	switch cmd {
	case "tool", "t":
		if len(words) == 1 {
			if strings.HasSuffix(text, " ") {
				return toolSubcommands
			}
			return prompt.FilterHasPrefix(commands, cmd, true)
		}
		if len(words) == 2 && !strings.HasSuffix(text, " ") {
			return prompt.FilterHasPrefix(toolSubcommands, words[1], true)
		}
		// Handle flags for list subcommand
		if len(words) >= 2 && (words[1] == "list" || words[1] == "ls") {
			lastWord := words[len(words)-1]
			if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
				return prompt.FilterHasPrefix(toolListFlags, lastWord, true)
			}
			if strings.HasSuffix(text, " ") {
				return toolListFlags
			}
		}
		// Handle flags for create subcommand
		if len(words) >= 2 && words[1] == "create" {
			lastWord := words[len(words)-1]
			if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
				return prompt.FilterHasPrefix(toolCreateFlags, lastWord, true)
			}
			if strings.HasSuffix(text, " ") {
				return toolCreateFlags
			}
		}
		// Handle flags for update subcommand
		if len(words) >= 2 && words[1] == "update" {
			lastWord := words[len(words)-1]
			if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
				return prompt.FilterHasPrefix(toolUpdateFlags, lastWord, true)
			}
			if strings.HasSuffix(text, " ") {
				return toolUpdateFlags
			}
		}
		// Handle flags for get subcommand
		if len(words) >= 2 && words[1] == "get" {
			lastWord := words[len(words)-1]
			if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
				return prompt.FilterHasPrefix(toolGetFlags, lastWord, true)
			}
			if strings.HasSuffix(text, " ") {
				return toolGetFlags
			}
		}
		// Handle flags for delete subcommand
		if len(words) >= 2 && (words[1] == "delete" || words[1] == "rm" || words[1] == "del") {
			lastWord := words[len(words)-1]
			if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
				return prompt.FilterHasPrefix(toolDeleteFlags, lastWord, true)
			}
			if strings.HasSuffix(text, " ") {
				return toolDeleteFlags
			}
		}

	case "instance", "i":
		if len(words) == 1 {
			if strings.HasSuffix(text, " ") {
				return instanceSubcommands
			}
			return prompt.FilterHasPrefix(commands, cmd, true)
		}
		if len(words) == 2 && !strings.HasSuffix(text, " ") {
			return prompt.FilterHasPrefix(instanceSubcommands, words[1], true)
		}
		// Handle flags for create subcommand
		if len(words) >= 2 && (words[1] == "create" || words[1] == "c" || words[1] == "start") {
			lastWord := words[len(words)-1]
			if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
				return prompt.FilterHasPrefix(instanceCreateFlags, lastWord, true)
			}
			if strings.HasSuffix(text, " ") {
				return instanceCreateFlags
			}
		}
		// Handle flags for list subcommand
		if len(words) >= 2 && (words[1] == "list" || words[1] == "ls") {
			lastWord := words[len(words)-1]
			if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
				return prompt.FilterHasPrefix(instanceListFlags, lastWord, true)
			}
			if strings.HasSuffix(text, " ") {
				return instanceListFlags
			}
		}
		// Handle flags for login subcommand
		if len(words) >= 2 && words[1] == "login" {
			lastWord := words[len(words)-1]
			if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
				return prompt.FilterHasPrefix(instanceLoginFlags, lastWord, true)
			}
			if strings.HasSuffix(text, " ") {
				return instanceLoginFlags
			}
		}

	case "run", "r":
		if len(words) == 1 && strings.HasSuffix(text, " ") {
			return runFlags
		}

		lastWord := words[len(words)-1]

		// Check if we should suggest languages (after -l or --language)
		if strings.HasSuffix(text, " ") {
			prevFlag := getPreviousFlag(words)
			if prevFlag == "-l" || prevFlag == "--language" {
				return languages
			}
			return runFlags
		}

		// Check if currently typing after -l or --language
		if len(words) >= 2 {
			prevWord := words[len(words)-2]
			if prevWord == "-l" || prevWord == "--language" {
				return prompt.FilterHasPrefix(languages, lastWord, true)
			}
		}

		if strings.HasPrefix(lastWord, "-") {
			return prompt.FilterHasPrefix(runFlags, lastWord, true)
		}

	case "apikey", "ak", "key":
		if len(words) == 1 {
			if strings.HasSuffix(text, " ") {
				return apikeySubcommands
			}
			return prompt.FilterHasPrefix(commands, cmd, true)
		}
		if len(words) == 2 && !strings.HasSuffix(text, " ") {
			return prompt.FilterHasPrefix(apikeySubcommands, words[1], true)
		}
		// Handle flags for create subcommand
		if len(words) >= 2 && words[1] == "create" {
			lastWord := words[len(words)-1]
			if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
				return prompt.FilterHasPrefix(apikeyCreateFlags, lastWord, true)
			}
			if strings.HasSuffix(text, " ") {
				return apikeyCreateFlags
			}
		}

	case "exec", "x":
		if len(words) == 1 {
			if strings.HasSuffix(text, " ") {
				suggestions := append([]prompt.Suggest{}, execSubcommands...)
				suggestions = append(suggestions, execFlags...)
				return suggestions
			}
			return prompt.FilterHasPrefix(commands, cmd, true)
		}
		lastWord := words[len(words)-1]
		// Check for ps subcommand
		if len(words) == 2 && !strings.HasSuffix(text, " ") {
			return prompt.FilterHasPrefix(execSubcommands, words[1], true)
		}
		if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
			return prompt.FilterHasPrefix(execFlags, lastWord, true)
		}
		if strings.HasSuffix(text, " ") {
			return execFlags
		}

	case "file", "f", "fs":
		if len(words) == 1 {
			if strings.HasSuffix(text, " ") {
				return fileSubcommands
			}
			return prompt.FilterHasPrefix(commands, cmd, true)
		}
		if len(words) == 2 && !strings.HasSuffix(text, " ") {
			return prompt.FilterHasPrefix(fileSubcommands, words[1], true)
		}
		// After subcommand, suggest flags
		lastWord := words[len(words)-1]
		if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
			return prompt.FilterHasPrefix(fileFlags, lastWord, true)
		}
		if strings.HasSuffix(text, " ") {
			return fileFlags
		}

	case "browser", "b":
		if len(words) == 1 {
			if strings.HasSuffix(text, " ") {
				return browserSubcommands
			}
			return prompt.FilterHasPrefix(commands, cmd, true)
		}
		if len(words) == 2 && !strings.HasSuffix(text, " ") {
			return prompt.FilterHasPrefix(browserSubcommands, words[1], true)
		}
		// Handle flags for vnc subcommand
		if len(words) >= 2 && words[1] == "vnc" {
			lastWord := words[len(words)-1]
			if strings.HasPrefix(lastWord, "-") && !strings.HasSuffix(text, " ") {
				return prompt.FilterHasPrefix(browserVNCFlags, lastWord, true)
			}
			if strings.HasSuffix(text, " ") {
				return browserVNCFlags
			}
		}
	}

	// Default: filter from all commands
	if !strings.HasSuffix(text, " ") {
		return prompt.FilterHasPrefix(commands, words[len(words)-1], true)
	}

	return globalFlags
}

// ExecuteCommand executes a command string
var ExecuteCommand func(args []string) error

// Start starts the REPL
func Start() error {
	fmt.Println("AGS CLI - Interactive Mode")
	fmt.Println("Type 'help' for available commands, 'exit' to quit")
	fmt.Println()

	p := prompt.New(
		executor,
		completer,
		prompt.OptionPrefix("ags> "),
		prompt.OptionTitle("AGS CLI"),
		prompt.OptionPrefixTextColor(prompt.Cyan),
		prompt.OptionPreviewSuggestionTextColor(prompt.Blue),
		prompt.OptionSelectedSuggestionBGColor(prompt.LightGray),
		prompt.OptionSuggestionBGColor(prompt.DarkGray),
		prompt.OptionHistory(history),
		prompt.OptionAddKeyBind(prompt.KeyBind{
			Key: prompt.ControlC,
			Fn: func(b *prompt.Buffer) {
				fmt.Println("^C")
			},
		}),
		prompt.OptionCompletionWordSeparator(fileCompleter.FilePathCompletionSeparator),
	)
	p.Run()
	return nil
}

func executor(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	// Add to history
	addToHistory(input)

	// Handle special commands
	switch input {
	case "exit", "quit":
		fmt.Println("Goodbye!")
		os.Exit(0)
	case "help":
		printHelp()
		return
	case "history":
		printHistory()
		return
	case "clear":
		fmt.Print("\033[H\033[2J")
		return
	}

	// Execute command through cobra
	if ExecuteCommand != nil {
		args := parseArgs(input)
		if err := ExecuteCommand(args); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
	} else {
		fmt.Println("Command execution not configured")
	}
}

// parseArgs parses input string into arguments, handling quoted strings
func parseArgs(input string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range input {
		switch {
		case r == '"' || r == '\'':
			if inQuote && r == quoteChar {
				inQuote = false
				quoteChar = 0
			} else if !inQuote {
				inQuote = true
				quoteChar = r
			} else {
				current.WriteRune(r)
			}
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

func printHistory() {
	if len(history) == 0 {
		fmt.Println("No command history")
		return
	}
	// Show last 20 commands
	start := 0
	if len(history) > 20 {
		start = len(history) - 20
	}
	for i := start; i < len(history); i++ {
		fmt.Printf("%4d  %s\n", i+1, history[i])
	}
}

func printHelp() {
	fmt.Println(`Available commands:

Tool Management:
  tool list, t list, t ls     List available tools
    --id <id>                   Specific tool IDs to query
    --status <status>           Filter by status
    --type <type>               Filter by type
    --short                     Show only ID and NAME
    --offset <n>                Pagination offset
    --limit <n>                 Pagination limit
    --time                      Print elapsed time to stderr
  tool get <id>, t get <id>   Get tool details
  tool create, t create       Create a new tool
    -n, --name <name>           Tool name (required)
    -t, --type <type>           Tool type (required)
    -d, --description <desc>    Tool description
    --role-arn <arn>            CAM Role ARN for COS access
    --mount <config>            Storage mount config
  tool update <id>, t update  Update a tool
    -d, --description <desc>    Tool description
    --network <mode>            Network mode: PUBLIC, SANDBOX, INTERNAL_SERVICE
    --tag <key=value>           Tags in key=value format
    --clear-tags                Clear all tags
  tool delete <id>, t rm <id> Delete a tool

Instance Management:
  instance create, i create, i c    Create a new instance
    -t, --tool-name <name>          Tool name (e2b/cloud backend)
    --tool <name>                   Tool name (alias for --tool-name)
    --tool-id <id>                  Tool ID (cloud backend only)
    --timeout <seconds>             Instance timeout (default: 300)
    --mount-option <config>         Mount option to override tool storage
    --time                          Print elapsed time to stderr
  
  instance list, i list, i ls       List all instances
    --tool-id <id>              Filter by tool ID
    -s, --status <status>           Filter by status
    --short                         Only show instance IDs
    --offset <n>                    Pagination offset
    --limit <n>                     Pagination limit
    --time                          Print elapsed time to stderr
  instance get <id>, i get <id>     Get instance details
  instance delete <id>, i rm <id>   Delete an instance

Code Execution:
  run -c "<code>"             Execute code string
  run -f <file>               Execute code from file (can repeat for multiple files)
  run -l <language>           Specify language (python, javascript, typescript, bash, r, java)
  run -s, --stream            Stream output in real-time
  run -n <N>, --repeat        Run same code N times
  run -p, --parallel          Execute multiple tasks in parallel
  run --max-parallel <N>      Limit max parallel executions
  run --instance <id>         Use existing instance
  run --keep-alive            Keep temporary instance alive
  run --time                  Print elapsed time to stderr

  Examples:
    run -c "print('Hello')"
    run -s -c "import time; [print(i) or time.sleep(1) for i in range(5)]"
    run -l javascript -c "console.log('Hello')"
    run -l bash -c "echo Hello"
    run -f script.py
    run -f a.py -f b.py -f c.py           # Multiple files sequentially
    run -f a.py -f b.py -p                # Multiple files in parallel
    run -c "print('test')" -n 5           # Same code 5 times concurrently

Shell Command Execution:
  exec "<command>"            Execute shell command in sandbox
  exec -s "<command>"         Stream output in real-time
  exec --cwd <path>           Set working directory
  exec --env KEY=VALUE        Set environment variable
  exec --time                 Print elapsed time to stderr
  exec ps                     List running processes

  Examples:
    exec "ls -la"
    exec -s "ping -c 5 localhost"
    exec --env FOO=bar "echo $FOO"
    exec --cwd /home/user "pwd"

File Operations:
  file list <path>, f ls      List files in directory
  file upload <local> <remote>, f up    Upload file to sandbox
  file download <remote> [local], f down  Download file from sandbox
  file cat <path>             Print file contents
  file stat <path>            Get file info
  file mkdir <path>           Create directory
  file remove <path>, f rm    Remove file or directory

  Common flags:
    --instance <id>           Use existing instance
    --keep-alive              Keep temporary instance alive
    --time                    Print elapsed time

  Examples:
    file ls /home/user
    file upload local.txt /home/user/remote.txt
    file download /home/user/file.txt ./local.txt
    file cat /home/user/.bashrc

API Key Management (Cloud backend only):
  apikey create, ak create    Create a new API key
    -n, --name <name>           API key name (required)
  apikey list, ak list, ak ls List API keys
  apikey delete <id>, ak rm   Delete an API key

Browser Sandbox:
  browser vnc, b vnc          Show VNC URL for browser sandbox
    -i, --instance <id>         Instance ID to connect to
    -t, --tool-name <name>      Tool name for creating new instance
    --tool <name>               Tool name (alias for --tool-name)
    --tool-id <id>              Tool ID (cloud backend only)
    --timeout <seconds>         Instance timeout (default: 300)
    -p, --port <port>           VNC service port (default: 9000)
    --time                      Print elapsed time

  Examples:
    browser vnc --instance <id>           # Show VNC URL for existing instance
    browser vnc -i <id>                   # Show VNC URL (short form)
    browser vnc --tool-name browser-v1    # Create new browser sandbox
    browser vnc -t browser-v1             # Create new browser sandbox (short form)
    browser vnc --tool browser-v1         # Create new browser sandbox (alias)
    browser vnc --tool-id sdt-xxxx        # Create using tool ID
    browser vnc -t browser-v1 --timeout 3600  # Create with 1 hour timeout

Global Flags:
  --backend <e2b|cloud>       API backend to use
  -o, --output <text|json>    Output format
  --cloud-internal            Use internal endpoints (Tencent Cloud internal network)

JSON Output:
  Commands with --time flag include timing info in JSON output.
  List commands include pagination info: {"items": [...], "pagination": {...}}

Other:
  help                        Show this help
  history                     Show command history
  clear                       Clear screen
  exit, quit                  Exit REPL

Keyboard Shortcuts:
  Ctrl+C                      Cancel current input
  Up/Down                     Navigate command history
  Tab                         Auto-complete`)
}
