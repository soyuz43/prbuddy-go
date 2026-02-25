// internal/dce/task_helper.go
package dce

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/soyuz43/prbuddy-go/internal/contextpkg"
	"github.com/soyuz43/prbuddy-go/internal/treesitter"
	"github.com/soyuz43/prbuddy-go/internal/utils"
)

// BuildTaskList creates tasks based on user input, file matching, and function extraction.
// Uses Tree-sitter for accurate Go function extraction instead of regex.
func BuildTaskList(input string) ([]contextpkg.Task, map[string]string, []string, error) {
	var logs []string
	logs = append(logs, fmt.Sprintf("Building task list from input: %q", input))

	// 1. Retrieve all tracked files.
	out, err := utils.ExecGit("ls-files")
	if err != nil {
		return nil, nil, logs, fmt.Errorf("failed to execute git ls-files: %w", err)
	}
	trackedFiles := utils.SplitLines(out)
	logs = append(logs, fmt.Sprintf("Found %d tracked files", len(trackedFiles)))

	// 2. Match files based on keywords.
	matchedFiles := matchFilesByKeywords(trackedFiles, input)
	logs = append(logs, fmt.Sprintf("Matched %d files: %v", len(matchedFiles), matchedFiles))

	// 3. If no files matched, create a catch-all task.
	if len(matchedFiles) == 0 {
		task := contextpkg.Task{
			Description: input,
			Notes:       []string{"No direct file matches found. Add manually."},
		}
		logs = append(logs, "No file matches found - created catch-all task")
		return []contextpkg.Task{task}, nil, logs, nil
	}

	// 4. Extract functions from each matched file using Tree-sitter.
	var allFunctions []string

	// Get repository root for Tree-sitter parsing
	repoRoot, err := utils.GetRepoPath()
	if err != nil {
		logs = append(logs, fmt.Sprintf("Warning: Could not get repo root: %v", err))
		repoRoot = "."
	}

	// Initialize Tree-sitter parser once (reuse across files for efficiency)
	parser := treesitter.NewGoParser()

	// Build project map for the entire repo (more efficient than per-file parsing)
	projectMap, err := parser.BuildProjectMap(repoRoot)
	if err != nil {
		logs = append(logs, fmt.Sprintf("Warning: Tree-sitter parse error: %v", err))
		logs = append(logs, "Falling back to empty function list")
	}

	// Extract functions for matched files from the project map
	for _, f := range matchedFiles {
		funcs := extractFunctionsFromProjectMap(f, projectMap)
		if len(funcs) > 0 {
			logs = append(logs, fmt.Sprintf("Extracted %d functions from %s: %v", len(funcs), f, funcs))
			allFunctions = append(allFunctions, funcs...)
		} else {
			logs = append(logs, fmt.Sprintf("No functions found in %s", f))
		}
	}

	// 4b. Read file contents for snapshots
	snapshots := make(map[string]string)
	for _, f := range matchedFiles {
		content, err := os.ReadFile(f)
		if err == nil {
			snapshots[f] = string(content)
		}
	}

	// 5. Create a consolidated task.
	task := contextpkg.Task{
		Description:  input,
		Files:        matchedFiles,
		Functions:    allFunctions,
		Dependencies: nil,
		Notes:        []string{"Matched via input and file heuristics. Functions extracted via Tree-sitter."},
	}
	logs = append(logs, fmt.Sprintf("Created task with %d files and %d functions", len(matchedFiles), len(allFunctions)))

	return []contextpkg.Task{task}, snapshots, logs, nil
}

// extractFunctionsFromProjectMap extracts function names for a specific file from the Tree-sitter project map.
// Handles path normalization between git ls-files output and Tree-sitter paths.
func extractFunctionsFromProjectMap(filePath string, projectMap *treesitter.ProjectMap) []string {
	if projectMap == nil || len(projectMap.Functions) == 0 {
		return nil
	}

	var funcs []string

	// Normalize the file path for comparison
	// Git returns: internal/dce/task_helper.go
	// Tree-sitter may return: /prbuddy-go/internal/dce/task_helper.go or internal/dce/task_helper.go
	normalizedTarget := normalizeFilePath(filePath)

	for _, fn := range projectMap.Functions {
		normalizedFnFile := normalizeFilePath(fn.File)

		// Match if paths are equivalent after normalization
		if normalizedFnFile == normalizedTarget || strings.HasSuffix(normalizedFnFile, normalizedTarget) {
			funcs = append(funcs, fn.Name)
		}
	}

	return funcs
}

// normalizeFilePath normalizes file paths for consistent comparison.
// Removes leading slashes and repo name prefixes that Tree-sitter may add.
func normalizeFilePath(path string) string {
	// Remove leading slashes
	path = strings.TrimPrefix(path, "/")

	// Remove repo name prefix if present (e.g., "prbuddy-go/")
	// This handles Tree-sitter's tendency to include repo name in paths
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 2 {
		// Check if first part looks like a repo name (no slashes, reasonable length)
		if !strings.Contains(parts[0], "/") && len(parts[0]) > 0 && len(parts[0]) < 50 {
			return parts[1]
		}
	}

	return path
}

// matchFilesByKeywords returns files from allFiles that contain any keyword from userInput.
func matchFilesByKeywords(allFiles []string, userInput string) []string {
	var matched []string
	words := strings.Fields(strings.ToLower(userInput))
	for _, file := range allFiles {
		lowerFile := strings.ToLower(file)
		for _, w := range words {
			if len(w) >= 3 && strings.Contains(lowerFile, w) {
				matched = append(matched, file)
				break
			}
		}
	}

	return matched
}

// RefreshTaskListFromGitChanges checks for unstaged and untracked changes and updates the task list if new files are detected.
// It uses Git commands to detect changes.
func RefreshTaskListFromGitChanges(conversationID string) error {
	// Get the LittleGuy instance for this conversation
	littleguy, exists := GetDCEContextManager().GetContext(conversationID)
	if !exists {
		return fmt.Errorf("no active DCE context found for conversation %s", conversationID)
	}

	// Retrieve unstaged changes.
	diffOutput, err := utils.ExecGit("diff", "--name-only")
	if err != nil {
		return fmt.Errorf("failed to retrieve git diff: %w", err)
	}
	unstagedFiles := utils.SplitLines(diffOutput)

	// Retrieve untracked files.
	untrackedOutput, err := utils.ExecGit("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return fmt.Errorf("failed to retrieve untracked files: %w", err)
	}
	untrackedFiles := utils.SplitLines(untrackedOutput)

	// Combine both lists.
	changedFiles := append(unstagedFiles, untrackedFiles...)

	// Filter out empty entries.
	var validChangedFiles []string
	for _, file := range changedFiles {
		if trimmed := file; trimmed != "" {
			validChangedFiles = append(validChangedFiles, trimmed)
		}
	}

	// For each changed file, if it is not already represented in a task, add a new task.
	littleguy.mutex.Lock()
	defer littleguy.mutex.Unlock()

	for _, changedFile := range validChangedFiles {
		existsInTask := false
		for _, task := range littleguy.tasks {
			for _, file := range task.Files {
				if file == changedFile {
					existsInTask = true
					break
				}
			}
			if existsInTask {
				break
			}
		}
		if !existsInTask {
			// Extract functions from new file using Tree-sitter
			var funcs []string
			repoRoot, err := utils.GetRepoPath()
			if err == nil {
				parser := treesitter.NewGoParser()
				projectMap, parseErr := parser.BuildProjectMap(repoRoot)
				if parseErr == nil {
					funcs = extractFunctionsFromProjectMap(changedFile, projectMap)
				}
			}

			newTask := contextpkg.Task{
				Description: fmt.Sprintf("New file detected: %s", changedFile),
				Files:       []string{changedFile},
				Functions:   funcs,
				Notes:       []string{"Automatically added due to git changes."},
			}
			littleguy.tasks = append(littleguy.tasks, newTask)
			fmt.Printf("[TaskHelper] Added new task for file: %s (functions: %v)\n", changedFile, funcs)
		}
	}
	return nil
}

// PeriodicallyRefreshTaskList runs RefreshTaskListFromGitChanges at the specified interval,
// allowing the task list to be updated periodically based on recent git changes.
func PeriodicallyRefreshTaskList(conversationID string) {
	interval := 100 * time.Second // Set the refresh interval to 100 seconds
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		<-ticker.C
		err := RefreshTaskListFromGitChanges(conversationID)
		if err != nil {
			fmt.Printf("[TaskHelper] Error refreshing task list: %v\n", err)
		} else {
			fmt.Println("[TaskHelper] Task list refreshed based on git changes.")
		}
	}
}
