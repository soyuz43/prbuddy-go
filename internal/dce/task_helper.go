// internal/dce/task_helper.go
package dce

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/soyuz43/prbuddy-go/internal/contextpkg"
	"github.com/soyuz43/prbuddy-go/internal/utils"
)

// BuildTaskList creates tasks based on user input, file matching, and function extraction.
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

	// 4. Extract functions from each matched file.
	var allFunctions []string
	fileFuncPattern := `(?m)^\s*(def|func|function|public|private|static|void)\s+(\w+)\s*\(`
	for _, f := range matchedFiles {
		funcs := extractFunctionsFromFile(f, fileFuncPattern)
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
		Notes:        []string{"Matched via input and file heuristics."},
	}
	logs = append(logs, fmt.Sprintf("Created task with %d files and %d functions", len(matchedFiles), len(allFunctions)))

	return []contextpkg.Task{task}, snapshots, logs, nil
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

// extractFunctionsFromFile reads file content and extracts function names using the provided regex pattern.
func extractFunctionsFromFile(filePath, pattern string) []string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	matches := re.FindAllStringSubmatch(string(data), -1)
	var funcs []string
	for _, m := range matches {
		if len(m) >= 3 {
			funcs = append(funcs, m[2])
		}
	}

	return funcs
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
			newTask := contextpkg.Task{
				Description: fmt.Sprintf("New file detected: %s", changedFile),
				Files:       []string{changedFile},
				Functions:   []string{}, // Optionally, extract functions from the file.
				Notes:       []string{"Automatically added due to git changes."},
			}
			littleguy.tasks = append(littleguy.tasks, newTask)
			fmt.Printf("[TaskHelper] Added new task for file: %s\n", changedFile)
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
