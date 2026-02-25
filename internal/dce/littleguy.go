// internal/dce/littleguy.go

package dce

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/soyuz43/prbuddy-go/internal/contextpkg"
	"github.com/soyuz43/prbuddy-go/internal/utils"
)

// LittleGuy tracks an ephemeral code snapshot and tasks for a single DCE session.
type LittleGuy struct {
	mutex          sync.RWMutex
	conversationID string
	tasks          []contextpkg.Task // Ongoing tasks
	completed      []contextpkg.Task // Completed tasks
	codeSnapshots  map[string]string // filePath -> file content
	pollInterval   time.Duration     // How often to check for diffs
	monitorStarted bool              // Tracks background monitoring status
	pendingQueries []string
	queryCallback  func(string)
}

// NewLittleGuy initializes a new LittleGuy instance.
func NewLittleGuy(conversationID string, initialTasks []contextpkg.Task) *LittleGuy {
	lg := &LittleGuy{
		conversationID: conversationID,
		tasks:          initialTasks,
		completed:      []contextpkg.Task{},
		codeSnapshots:  make(map[string]string),
		pollInterval:   10 * time.Second,
	}

	// Add to context manager
	GetDCEContextManager().AddContext(conversationID, lg)
	return lg
}

// IsActive returns whether the DCE monitoring is active
func (lg *LittleGuy) IsActive() bool {
	lg.mutex.RLock()
	defer lg.mutex.RUnlock()
	return lg.monitorStarted
}

// StopMonitoring stops the background monitoring
func (lg *LittleGuy) StopMonitoring() {
	lg.mutex.Lock()
	defer lg.mutex.Unlock()
	lg.monitorStarted = false
}

// GetPollInterval returns the current polling interval
func (lg *LittleGuy) GetPollInterval() time.Duration {
	lg.mutex.RLock()
	defer lg.mutex.RUnlock()
	return lg.pollInterval
}

// GetConversationID returns the associated conversation ID
func (lg *LittleGuy) GetConversationID() string {
	return lg.conversationID
}

// StartMonitoring launches a background goroutine that periodically checks Git diffs.
func (lg *LittleGuy) StartMonitoring() {
	lg.mutex.Lock()
	if lg.monitorStarted {
		lg.mutex.Unlock()
		return
	}
	lg.monitorStarted = true
	lg.mutex.Unlock()

	go func() {
		for {
			lg.mutex.RLock()
			monitoring := lg.monitorStarted
			interval := lg.pollInterval
			lg.mutex.RUnlock()

			if !monitoring {
				return
			}

			time.Sleep(interval)

			diffOutput, err := utils.ExecGit("diff", "--unified=0")
			if err != nil {
				color.Red("[LittleGuy] Failed to run git diff: %v\n", err)
				continue
			}
			if diffOutput != "" {
				lg.UpdateFromDiff(diffOutput)
			}
		}
	}()
}

// MonitorInput analyzes user input for function names or file references and updates tasks.
func (lg *LittleGuy) MonitorInput(input string) {
	// Parse input outside lock (cheap, but keeps lock time small).
	type pendingTask struct {
		desc  string
		files []string
		fns   []string
		notes []string
	}

	var toAdd []pendingTask

	lines := strings.Split(input, "\n")
	for _, line := range lines {
		if matches := FuncPattern.FindStringSubmatch(line); len(matches) >= 3 {
			funcName := matches[2]
			toAdd = append(toAdd, pendingTask{
				desc:  fmt.Sprintf("Detected function: %s", funcName),
				fns:   []string{funcName},
				notes: []string{"Consider testing and documenting this function."},
			})
		}

		if strings.Contains(line, ".go") || strings.Contains(line, ".js") ||
			strings.Contains(line, ".py") || strings.Contains(line, ".ts") {
			words := strings.Fields(line)
			for _, word := range words {
				if strings.Contains(word, ".go") || strings.Contains(word, ".js") ||
					strings.Contains(word, ".py") || strings.Contains(word, ".ts") {
					toAdd = append(toAdd, pendingTask{
						desc:  fmt.Sprintf("Detected file reference: %s", word),
						files: []string{word},
						notes: []string{"Consider adding to code snapshots or tasks."},
					})
				}
			}
		}
	}

	// Apply under lock (dedupe using existing state).
	lg.mutex.Lock()
	for _, p := range toAdd {
		// If it's a function task, dedupe by function name
		if len(p.fns) == 1 && p.fns[0] != "" {
			if lg.hasTaskForFunction(p.fns[0]) {
				continue
			}
		}
		// If it's a file task, dedupe by file path
		if len(p.files) == 1 && p.files[0] != "" {
			if lg.hasTaskForFile(p.files[0]) {
				continue
			}
		}

		lg.tasks = append(lg.tasks, contextpkg.Task{
			Description: p.desc,
			Files:       p.files,
			Functions:   p.fns,
			Notes:       p.notes,
		})
	}
	lg.mutex.Unlock()

	// Log context AFTER unlocking to avoid deadlock and lock contention.
	messages := lg.BuildEphemeralContext("")
	lg.logLLMContext(messages)
}

// UpdateFromDiff parses Git diff output and updates tasks accordingly.
func (lg *LittleGuy) UpdateFromDiff(diff string) {
	// CRITICAL:
	// ParseGitDiff can be expensive (large diffs).
	// Do it OUTSIDE the lock so /t (which needs RLock) can't stall for seconds/minutes.
	changes := ParseGitDiff(diff)

	// Apply changes under lock.
	lg.mutex.Lock()
	for _, change := range changes {
		switch change.Type {
		case "new_file":
			lg.handleNewFile(change)
		case "modified":
			lg.handleModifiedFile(change)
		case "deleted":
			lg.handleDeletedFile(change)
		}
	}
	lg.mutex.Unlock()

	// Log the updated context for debugging (no locks held).
	messages := lg.BuildEphemeralContext("")
	lg.logLLMContext(messages)
}

// ParseGitDiff extracts meaningful changes from git diff output
func ParseGitDiff(diff string) []GitChange {
	var changes []GitChange
	currentFile := ""
	lines := strings.Split(diff, "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			// Extract file path
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				currentFile = strings.TrimPrefix(parts[2], "b/")
			}
		} else if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ b/")
		} else if currentFile != "" && (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")) {
			// Skip header lines
			if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
				continue
			}

			// Process change line
			changeType := "modified"
			if strings.HasPrefix(line, "+") {
				changeType = "added"
			} else if strings.HasPrefix(line, "-") {
				changeType = "removed"
			}

			// Extract function name if present
			funcName := ""
			if matches := FuncPattern.FindStringSubmatch(line[1:]); len(matches) >= 3 {
				funcName = matches[2]
			}

			changes = append(changes, GitChange{
				File:     currentFile,
				Type:     changeType,
				Content:  strings.TrimPrefix(line, "+- "),
				FuncName: funcName,
			})
		}
	}

	return changes
}

// GitChange represents a single change in a git diff
type GitChange struct {
	File     string
	Type     string // "added", "removed", "modified"
	Content  string
	FuncName string
}

// handleNewFile creates appropriate tasks for a new file
func (lg *LittleGuy) handleNewFile(change GitChange) {
	lg.tasks = append(lg.tasks, contextpkg.Task{
		Description: fmt.Sprintf("New file: %s", change.File),
		Files:       []string{change.File},
		Notes:       []string{"Consider adding tests and documentation"},
	})
}

// handleModifiedFile creates appropriate tasks for modified content
func (lg *LittleGuy) handleModifiedFile(change GitChange) {
	if change.FuncName != "" {
		if change.Type == "added" {
			// Function was added
			lg.tasks = append(lg.tasks, contextpkg.Task{
				Description: fmt.Sprintf("New function: %s", change.FuncName),
				Files:       []string{change.File},
				Functions:   []string{change.FuncName},
				Notes:       []string{"Write unit tests", "Add documentation"},
			})
		} else if change.Type == "removed" {
			// Function was removed - mark related tasks as completed
			for i := 0; i < len(lg.tasks); i++ {
				task := lg.tasks[i]
				for _, fn := range task.Functions {
					if fn == change.FuncName {
						lg.completed = append(lg.completed, task)
						lg.tasks = append(lg.tasks[:i], lg.tasks[i+1:]...)
						i--
						break
					}
				}
			}
		}
	}
}

// handleDeletedFile handles file deletion
func (lg *LittleGuy) handleDeletedFile(change GitChange) {
	// Mark all tasks related to this file as completed
	for i := 0; i < len(lg.tasks); i++ {
		task := lg.tasks[i]
		for _, file := range task.Files {
			if file == change.File {
				lg.completed = append(lg.completed, task)
				lg.tasks = append(lg.tasks[:i], lg.tasks[i+1:]...)
				i--
				break
			}
		}
	}
}

// markTaskAsCompleted moves tasks referencing a given function to the completed list.
func (lg *LittleGuy) markTaskAsCompleted(funcName string) {
	for i, task := range lg.tasks {
		for _, f := range task.Functions {
			if f == funcName {
				lg.completed = append(lg.completed, task)
				lg.tasks = append(lg.tasks[:i], lg.tasks[i+1:]...)
				return
			}
		}
	}
}

// BuildEphemeralContext aggregates tasks, code snapshots, and user input into the LLM context.
func (lg *LittleGuy) BuildEphemeralContext(userQuery string) []contextpkg.Message {
	lg.mutex.RLock()
	defer lg.mutex.RUnlock()

	var messages []contextpkg.Message
	messages = append(messages, contextpkg.Message{
		Role:    "system",
		Content: "You are a helpful developer assistant. Below is the current task list and code snapshots.",
	})

	if len(lg.tasks) > 0 {
		var builder strings.Builder
		for i, t := range lg.tasks {
			builder.WriteString(fmt.Sprintf("Task %d: %s\n", i+1, t.Description))
			if len(t.Notes) > 0 {
				builder.WriteString(fmt.Sprintf("Notes: %v\n", t.Notes))
			}
			if len(t.Files) > 0 {
				builder.WriteString(fmt.Sprintf("Files: %v\n", t.Files))
			}
			if len(t.Functions) > 0 {
				builder.WriteString(fmt.Sprintf("Functions: %v\n", t.Functions))
			}
			builder.WriteString("\n")
		}
		messages = append(messages, contextpkg.Message{
			Role:    "system",
			Content: builder.String(),
		})
	}

	if len(lg.codeSnapshots) > 0 {
		var builder strings.Builder
		for path, content := range lg.codeSnapshots {
			builder.WriteString(fmt.Sprintf("File: %s\n---\n%s\n---\n\n", path, content))
		}
		messages = append(messages, contextpkg.Message{
			Role:    "system",
			Content: builder.String(),
		})
	}

	messages = append(messages, contextpkg.Message{
		Role:    "user",
		Content: userQuery,
	})
	return messages
}

// AddCodeSnippet stores a snippet of file content.
func (lg *LittleGuy) AddCodeSnippet(filePath, content string) {
	lg.mutex.Lock()
	defer lg.mutex.Unlock()
	lg.codeSnapshots[filePath] = content
}

// UpdateTaskList appends new tasks if they're not already represented.
func (lg *LittleGuy) UpdateTaskList(newTasks []contextpkg.Task) {
	lg.mutex.Lock()
	defer lg.mutex.Unlock()
	for _, t := range newTasks {
		duplicate := false
		for _, existing := range lg.tasks {
			if t.Description == existing.Description {
				duplicate = true
				break
			}
		}
		if !duplicate {
			lg.tasks = append(lg.tasks, t)
		}
	}
}

// logLLMContext writes the raw LLM input to a log file using utils.LogLittleGuyContext.
func (lg *LittleGuy) logLLMContext(messages []contextpkg.Message) {
	var rawContext strings.Builder
	for _, msg := range messages {
		rawContext.WriteString(fmt.Sprintf("%s: %s\n\n", msg.Role, msg.Content))
	}
	if err := utils.LogLittleGuyContext(lg.conversationID, rawContext.String()); err != nil {
		color.Red("[LittleGuy] Failed to log LLM context: %v\n", err)
	}
}

// hasTaskForFile returns true if any task already includes the file.
func (lg *LittleGuy) hasTaskForFile(file string) bool {
	for _, task := range lg.tasks {
		for _, f := range task.Files {
			if f == file {
				return true
			}
		}
	}
	return false
}

// hasTaskForFunction returns true if any task already includes the function.
func (lg *LittleGuy) hasTaskForFunction(fn string) bool {
	for _, task := range lg.tasks {
		for _, f := range task.Functions {
			if f == fn {
				return true
			}
		}
	}
	return false
}

// Method to set query callback
func (lg *LittleGuy) SetQueryCallback(callback func(string)) {
	lg.queryCallback = callback
}

// Method to generate and send queries based on task changes
func (lg *LittleGuy) CheckForQueries() {
	lg.mutex.Lock()
	defer lg.mutex.Unlock()

	// 1. Check for new functions without tests
	for _, task := range lg.tasks {
		for _, fn := range task.Functions {
			if !lg.hasTestForFunction(fn) && !lg.isQueryPending(fn) {
				query := fmt.Sprintf("You added the function '%s'. Would you like me to generate test cases?", fn)
				lg.pendingQueries = append(lg.pendingQueries, fn)
				if lg.queryCallback != nil {
					lg.queryCallback(query)
				}
			}
		}
	}
}

// Helper to check if a function has tests (simplified)
func (lg *LittleGuy) hasTestForFunction(funcName string) bool {
	for _, task := range lg.tasks {
		if strings.Contains(task.Description, "test") &&
			utils.StringSliceContains(task.Functions, funcName) {
			return true
		}
	}
	return false
}

// Helper to check if query is already pending
func (lg *LittleGuy) isQueryPending(identifier string) bool {
	for _, p := range lg.pendingQueries {
		if p == identifier {
			return true
		}
	}
	return false
}

// containsString returns true if the slice contains the value.
// This replaces the missing utils.StringSliceContains function.
func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
