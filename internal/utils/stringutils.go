// internal/utils/stringutils.go
package utils

import (
	"strings"
	"unicode"
)

// SplitLines splits a string into lines, removing any trailing newline.
func SplitLines(s string) []string {
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// JoinLines joins a slice of strings into a single string separated by newlines.
func JoinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

// SanitizeBranchName replaces "/" with "_" and spaces with "-" in branch names.
func SanitizeBranchName(branch string) string {
	return strings.ReplaceAll(strings.ReplaceAll(branch, "/", "_"), " ", "-")
}

// StringSliceContains returns true if the slice contains the value.
func StringSliceContains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// StripOuterMarkdownCodeFence removes a single outer triple-backtick code fence if present.
// Example input:
//
// ```markdown
// # Title
// body
// ```
//
// Output:
//
// # Title
// body
func StripOuterMarkdownCodeFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return s
	}

	// Find the first newline after the opening fence line.
	firstNL := strings.Index(t, "\n")
	if firstNL == -1 {
		// It's just ```something with no body; treat as-is.
		return strings.TrimSpace(strings.TrimPrefix(t, "```"))
	}

	// Everything after the opening fence line.
	body := t[firstNL+1:]

	// Remove the last closing fence if it exists at the end.
	// We intentionally allow trailing whitespace after it.
	bodyTrimRight := strings.TrimRightFunc(body, unicode.IsSpace)
	if strings.HasSuffix(bodyTrimRight, "```") {
		bodyTrimRight = strings.TrimSuffix(bodyTrimRight, "```")
		bodyTrimRight = strings.TrimRightFunc(bodyTrimRight, unicode.IsSpace)
		return bodyTrimRight
	}

	// If there's no closing fence, just return whatever is after the first line.
	return strings.TrimSpace(body)
}

// ExtractPRTitleFromMarkdown tries to find a title from the draft markdown.
// It prefers the first markdown heading line: "# ..." or "## ...".
func ExtractPRTitleFromMarkdown(markdown string) string {
	lines := SplitLines(strings.TrimSpace(markdown))
	for _, ln := range lines {
		line := strings.TrimSpace(ln)
		if strings.HasPrefix(line, "#") {
			// Count leading #'s (e.g. #, ##, ###)
			i := 0
			for i < len(line) && line[i] == '#' {
				i++
			}
			title := strings.TrimSpace(line[i:])
			title = strings.TrimPrefix(title, "PR:")
			title = strings.TrimSpace(title)
			if title != "" {
				return title
			}
		}
	}
	return ""
}
