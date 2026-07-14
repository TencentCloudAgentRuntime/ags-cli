package cli

import (
	"fmt"
	"strings"
)

func exampleBlocks(examples ...string) string {
	var blocks []string
	for i, example := range examples {
		example = strings.TrimSpace(example)
		if example == "" {
			continue
		}
		if strings.HasPrefix(example, "Example - ") {
			blocks = append(blocks, example)
			continue
		}
		blocks = append(blocks, fmt.Sprintf("Example - %s:\n%s", builtinExampleTitle(i), indentBuiltinExample(example)))
	}
	return strings.Join(blocks, "\n\n")
}

func exampleCommands(blocks string) []string {
	var examples []string
	var lines []string
	inExample := false
	flush := func() {
		example := strings.TrimSpace(strings.Join(lines, "\n"))
		if example != "" {
			examples = append(examples, example)
		}
		lines = nil
	}

	for _, line := range strings.Split(blocks, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Example - ") {
			if inExample {
				flush()
			}
			inExample = true
			continue
		}
		if inExample {
			lines = append(lines, strings.TrimPrefix(line, "  "))
		}
	}
	if inExample {
		flush()
	}
	return examples
}

func builtinExampleTitle(index int) string {
	switch index {
	case 0:
		return "Basic usage"
	case 1:
		return "Common option"
	case 2:
		return "JSON output"
	default:
		return "Advanced usage"
	}
}

func indentBuiltinExample(example string) string {
	var lines []string
	for _, line := range strings.Split(example, "\n") {
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, "  "+strings.TrimRight(line, " "))
	}
	return strings.Join(lines, "\n")
}
