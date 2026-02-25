// internal/dce/command_menu.go

package dce

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/soyuz43/prbuddy-go/internal/contextpkg"
)

// outputWriter is used for all command output to enable testability
var outputWriter io.Writer = os.Stdout

// SetOutput allows setting a custom output writer for testing
func SetOutput(w io.Writer) {
	outputWriter = w
}

type commandOptions struct {
	Verbose bool
}

// HandleDCECommandMenu checks if the user input is a recognized command
// and executes the appropriate function.
//
// Returns 'true' if the input matched a slash-command and was handled internally
// (including unknown commands, which show help).
// Returns 'false' only if the input is not a slash-command so it can be passed to the LLM.
func HandleDCECommandMenu(input string, littleguy *LittleGuy) bool {
	trimmed := strings.TrimSpace(input)
	if !isSlashCommand(trimmed) {
		return false
	}

	cmd, args, opts := parseSlashCommand(trimmed)

	switch cmd {
	case "tasks":
		displayTaskList(littleguy, opts.Verbose)
		return true

	case "add":
		handleAddCommand(args, littleguy)
		return true

	case "dce":
		handleDCEControlCommand(args, littleguy)
		return true

	case "help":
		displayCommandMenu()
		return true

	case "priority":
		handlePriorityCommand(args, littleguy)
		return true

	case "complete":
		handleCompleteCommand(args, littleguy)
		return true

	case "refresh":
		refreshTaskList(littleguy)
		return true

	case "status":
		displayDCEStatus(littleguy)
		return true

	default:
		// Any unknown slash command should be handled internally with help output.
		color.New(color.FgYellow).Fprintf(outputWriter, "[!] Unrecognized command: %q\n", trimmed)
		displayCommandMenu()
		return true
	}
}

func isSlashCommand(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), "/")
}

// parseSlashCommand canonicalizes abbreviations/aliases/typos and parses common flags.
// It returns:
// - canonical command name (e.g., "tasks", "add", "help")
// - args string (everything after the command token, preserved for downstream handlers)
// - options (e.g., verbose)
func parseSlashCommand(input string) (canonical string, args string, opts commandOptions) {
	// Keep original spacing for args reconstruction but use Fields for token parsing.
	trimmed := strings.TrimSpace(input)
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "help", "", commandOptions{}
	}

	rawCmd := strings.TrimPrefix(strings.ToLower(fields[0]), "/")

	// Canonicalize command via alias map.
	canonical = canonicalizeCommand(rawCmd)

	// Parse flags (shared patterns).
	// Example accepted forms:
	//   /t -v
	//   /t v
	//   /t verbose
	//   /tasks -v
	//   /tasks verbose
	//
	// For other commands, flags are ignored unless you want them later.
	for _, tok := range fields[1:] {
		switch strings.ToLower(tok) {
		case "-v", "v", "verbose":
			opts.Verbose = true
		}
	}

	// Rebuild args as the original substring after the first token.
	// This ensures descriptions like "/a fix foo bar" keep spaces.
	if len(fields) > 1 {
		// Find first space after the command token in the original trimmed string.
		// Safer than joining fields because it preserves the user's spacing fairly well.
		first := fields[0]
		idx := strings.Index(trimmed, first)
		if idx >= 0 {
			rest := strings.TrimSpace(trimmed[idx+len(first):])
			args = rest
		} else {
			args = strings.Join(fields[1:], " ")
		}
	}

	return canonical, args, opts
}

func canonicalizeCommand(raw string) string {
	// Explicit alias map is safer than “fuzzy matching” for a CLI.
	// Add more aliases/typos here as you observe them in real usage.
	aliases := map[string]string{
		// tasks
		"t":      "tasks",
		"task":   "tasks",
		"tasks":  "tasks",
		"taks":   "tasks", // common typo
		"taks.":  "tasks", // just in case weird punctuation slips in
		"taks,":  "tasks",
		"taks;":  "tasks",
		"taks:":  "tasks",
		"taks!":  "tasks",
		"taks?":  "tasks",
		"taks-":  "tasks",
		"taks_":  "tasks",
		"taks/":  "tasks",
		"taks\\": "tasks",
		"taks)":  "tasks",
		"taks(":  "tasks",
		"taks]":  "tasks",
		"taks[":  "tasks",
		"taks}":  "tasks",
		"taks{":  "tasks",
		"taks'":  "tasks",
		"taks\"": "tasks",
		"taks*":  "tasks",
		"taks&":  "tasks",
		"taks%":  "tasks",
		"taks$":  "tasks",
		"taks#":  "tasks",
		"taks@":  "tasks",
		"taks^":  "tasks",
		"taks~":  "tasks",
		"taks`":  "tasks",
		"taks|":  "tasks",
		"taks+":  "tasks",
		"taks=":  "tasks",
		"taks<":  "tasks",
		"taks>":  "tasks",

		// add
		"a":   "add",
		"add": "add",

		// help / commands
		"c":        "help",
		"cmd":      "help",
		"cmds":     "help",
		"command":  "help",
		"commands": "help",
		"help":     "help",
		"h":        "help",

		// dce
		"d":   "dce",
		"dce": "dce",

		// priority
		"p":        "priority",
		"prio":     "priority",
		"priority": "priority",

		// complete
		"comp":     "complete",
		"complete": "complete",

		// refresh
		"r":       "refresh",
		"refresh": "refresh",

		// status
		"s":      "status",
		"status": "status",
	}

	// Strip trailing punctuation sometimes produced by copy/paste or fat-finger.
	raw = strings.Trim(raw, " \t\r\n")
	raw = strings.Trim(raw, ".,;:!?")

	if canon, ok := aliases[raw]; ok {
		return canon
	}
	return "unknown"
}

// handleAddCommand processes /add and /a commands to add new tasks to the task list.
//
// IMPORTANT SEMANTICS:
// - /add should ADD tasks, not replace the whole task list.
// - Task creation should happen only on initial prompt and /add (not on /task, /status, etc.).
func handleAddCommand(args string, littleguy *LittleGuy) {
	taskDescription := strings.TrimSpace(args)

	if taskDescription == "" {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Please provide a task description after /add\n")
		return
	}

	color.New(color.FgCyan).Fprintf(outputWriter, "\n[Add] Building task from description: %q\n", taskDescription)

	// Build task list from the description
	tasks, snapshots, logs, err := BuildTaskList(taskDescription)
	if err != nil {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Failed to build task list: %v\n", err)
		return
	}

	// Log the build process
	for _, logMsg := range logs {
		fmt.Fprintf(outputWriter, "[DCE] %s\n", logMsg)
	}

	// Add (append) the new tasks to the current task list (do NOT replace).
	appendTasks(littleguy, tasks)

	// Feed snapshots into LittleGuy
	for filePath, content := range snapshots {
		littleguy.AddCodeSnippet(filePath, content)
	}

	// Provide feedback
	color.New(color.FgGreen).Fprintf(outputWriter, "\n[Add] Successfully added %d task(s) to the task list\n", len(tasks))

	// Display the added tasks
	for i, task := range tasks {
		fmt.Fprintf(outputWriter, "  %d) %s\n", i+1, task.Description)
		if len(task.Files) > 0 {
			fmt.Fprintf(outputWriter, "     Files: %s\n", strings.Join(task.Files, ", "))
		}
		if len(task.Functions) > 0 {
			fmt.Fprintf(outputWriter, "     Functions: %s\n", strings.Join(task.Functions, ", "))
		}
		if len(task.Notes) > 0 {
			fmt.Fprintf(outputWriter, "     Notes: %s\n", strings.Join(task.Notes, "; "))
		}
	}
}

// appendTasks appends tasks to the current task list with proper locking.
// This avoids accidental full replacement via UpdateTaskList and keeps /add semantics correct.
func appendTasks(littleguy *LittleGuy, newTasks []contextpkg.Task) {
	if littleguy == nil || len(newTasks) == 0 {
		return
	}

	littleguy.mutex.Lock()
	littleguy.tasks = append(littleguy.tasks, newTasks...)
	littleguy.mutex.Unlock()
}

// displayTaskList prints the current task list.
// If verbose=true, it includes additional details like files, functions, notes, etc.
//
// IMPORTANT:
//   - Copy tasks under lock, then render WITHOUT holding the lock.
//     This prevents hangs/deadlocks if any other goroutine needs the same lock
//     (e.g., monitoring loop, refresh, etc.) while we are printing.
func displayTaskList(littleguy *LittleGuy, verbose bool) {
	color.New(color.FgCyan).Fprintf(outputWriter, "\n[Task List] Current Tasks:\n")

	// Copy-under-lock to avoid holding locks while doing I/O.
	tasks := snapshotTasks(littleguy)

	if len(tasks) == 0 {
		color.New(color.FgYellow).Fprintf(outputWriter, "  [!] No active tasks\n")
		return
	}

	for i, task := range tasks {
		fmt.Fprintf(outputWriter, "  %d) %s\n", i+1, task.Description)

		if verbose {
			if len(task.Files) > 0 {
				fmt.Fprintf(outputWriter, "     Files: %s\n", strings.Join(task.Files, ", "))
			}
			if len(task.Functions) > 0 {
				fmt.Fprintf(outputWriter, "     Functions: %s\n", strings.Join(task.Functions, ", "))
			}
			if len(task.Notes) > 0 {
				fmt.Fprintf(outputWriter, "     Notes: %s\n", strings.Join(task.Notes, "; "))
			}
		}
	}
}

// snapshotTasks returns a defensive copy of the current tasks so callers can
// iterate/print without holding locks and without racing concurrent updates.
func snapshotTasks(littleguy *LittleGuy) []contextpkg.Task {
	if littleguy == nil {
		return nil
	}

	littleguy.mutex.RLock()
	defer littleguy.mutex.RUnlock()

	if len(littleguy.tasks) == 0 {
		return nil
	}

	out := make([]contextpkg.Task, len(littleguy.tasks))
	for i := range littleguy.tasks {
		out[i] = littleguy.tasks[i]

		// Deep copy slices to avoid sharing underlying arrays.
		if len(littleguy.tasks[i].Files) > 0 {
			out[i].Files = append([]string(nil), littleguy.tasks[i].Files...)
		}
		if len(littleguy.tasks[i].Functions) > 0 {
			out[i].Functions = append([]string(nil), littleguy.tasks[i].Functions...)
		}
		if len(littleguy.tasks[i].Notes) > 0 {
			out[i].Notes = append([]string(nil), littleguy.tasks[i].Notes...)
		}
	}
	return out
}

// handleDCEControlCommand processes DCE control commands like "on" and "off"
func handleDCEControlCommand(args string, littleguy *LittleGuy) {
	lowerCmd := strings.ToLower(strings.TrimSpace(args))

	switch lowerCmd {
	case "on", "activate", "start":
		littleguy.mutex.Lock()
		wasActive := littleguy.monitorStarted
		littleguy.mutex.Unlock()

		if !wasActive {
			littleguy.StartMonitoring()
			color.New(color.FgGreen).Fprintf(outputWriter, "[DCE] Dynamic Context Engine activated\n")
			color.New(color.FgGreen).Fprintf(outputWriter, "[DCE] Use '/tasks' to view current development tasks\n")
		} else {
			color.New(color.FgYellow).Fprintf(outputWriter, "[DCE] DCE is already active\n")
		}

	case "off", "deactivate", "stop":
		littleguy.mutex.Lock()
		wasActive := littleguy.monitorStarted
		littleguy.mutex.Unlock()

		if wasActive {
			littleguy.mutex.Lock()
			littleguy.monitorStarted = false
			littleguy.mutex.Unlock()
			color.New(color.FgGreen).Fprintf(outputWriter, "[DCE] Dynamic Context Engine deactivated\n")
		} else {
			color.New(color.FgYellow).Fprintf(outputWriter, "[DCE] DCE is already inactive\n")
		}

	case "status", "info":
		displayDCEStatus(littleguy)

	case "":
		color.New(color.FgYellow).Fprintf(outputWriter, "[!] Usage: /dce on|off|status\n")

	default:
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Unknown DCE command. Use '/dce on', '/dce off', or '/dce status'\n")
	}
}

// displayDCEStatus shows detailed DCE status information
func displayDCEStatus(littleguy *LittleGuy) {
	color.New(color.FgCyan).Fprintf(outputWriter, "\n[DCE Status] Engine Status:\n")

	littleguy.mutex.RLock()
	status := "ACTIVE"
	if !littleguy.monitorStarted {
		status = "INACTIVE"
	}
	taskCount := len(littleguy.tasks)
	pollInterval := littleguy.pollInterval
	littleguy.mutex.RUnlock()

	fmt.Fprintf(outputWriter, "  Status: %s\n", status)
	fmt.Fprintf(outputWriter, "  Active Tasks: %d\n", taskCount)
	fmt.Fprintf(outputWriter, "  Monitoring Interval: %v\n", pollInterval)
	fmt.Fprintf(outputWriter, "  Features: Dynamic task tracking, Git change monitoring\n")
}

// handlePriorityCommand allows users to set task priorities.
// Input is the args portion after the command token.
func handlePriorityCommand(args string, littleguy *LittleGuy) {
	// We reconstruct a synthetic input to preserve old parsing expectations.
	parts := strings.Fields("/priority " + strings.TrimSpace(args))

	if len(parts) == 1 {
		// Display current priorities with formatted labels
		color.New(color.FgCyan).Fprintf(outputWriter, "\n[Priority] Current task priorities:\n")

		// Copy tasks under lock and render without holding lock.
		tasks := snapshotTasks(littleguy)

		for i, task := range tasks {
			priorityLabel := "[Low]"
			for _, note := range task.Notes {
				if strings.Contains(strings.ToLower(note), "high priority") {
					priorityLabel = "[High]"
					break
				} else if strings.Contains(strings.ToLower(note), "medium priority") {
					priorityLabel = "[Medium]"
				}
			}
			fmt.Fprintf(outputWriter, "  %d) %s %s\n", i+1, priorityLabel, task.Description)
		}
		return
	}

	// Setting priority requires exactly 3 parts: /priority <num> <level>
	if len(parts) != 3 {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Usage: /priority <task-number> <low|medium|high>\n")
		return
	}

	taskNumStr := parts[1]
	priorityLevel := strings.ToLower(parts[2])

	taskNum, err := strconv.Atoi(taskNumStr)
	if err != nil || taskNum < 1 {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Invalid task number\n")
		return
	}

	littleguy.mutex.Lock()
	defer littleguy.mutex.Unlock()

	if taskNum > len(littleguy.tasks) {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Task number out of range\n")
		return
	}

	task := &littleguy.tasks[taskNum-1]
	newNotes := []string{}
	for _, note := range task.Notes {
		lowerNote := strings.ToLower(note)
		if !strings.Contains(lowerNote, "priority") {
			newNotes = append(newNotes, note)
		}
	}

	switch priorityLevel {
	case "high", "urgent", "critical":
		newNotes = append(newNotes, "High Priority: Critical task requiring immediate attention")
		color.New(color.FgGreen).Fprintf(outputWriter, "[Priority] Task %d set to HIGH priority\n", taskNum)
	case "medium", "normal":
		newNotes = append(newNotes, "Medium Priority: Important but not time-critical")
		color.New(color.FgGreen).Fprintf(outputWriter, "[Priority] Task %d set to MEDIUM priority\n", taskNum)
	case "low", "optional":
		newNotes = append(newNotes, "Low Priority: Can be addressed later")
		color.New(color.FgGreen).Fprintf(outputWriter, "[Priority] Task %d set to LOW priority\n", taskNum)
	default:
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Invalid priority level. Use: low, medium, or high\n")
		return
	}

	task.Notes = newNotes
}

// handleCompleteCommand marks tasks as completed.
// Input is the args portion after the command token.
func handleCompleteCommand(args string, littleguy *LittleGuy) {
	parts := strings.Fields("/complete " + strings.TrimSpace(args))

	if len(parts) < 2 {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Usage: /complete <task-number>\n")
		return
	}

	taskNum, err := strconv.Atoi(parts[1])
	if err != nil || taskNum < 1 {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Invalid task number\n")
		return
	}

	littleguy.mutex.Lock()

	if taskNum > len(littleguy.tasks) {
		littleguy.mutex.Unlock()
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Task number out of range\n")
		return
	}

	task := littleguy.tasks[taskNum-1]

	littleguy.tasks = append(littleguy.tasks[:taskNum-1], littleguy.tasks[taskNum:]...)
	littleguy.completed = append(littleguy.completed, task)

	remainingTasks := make([]contextpkg.Task, len(littleguy.tasks))
	copy(remainingTasks, littleguy.tasks)

	taskCount := len(remainingTasks)
	littleguy.mutex.Unlock()

	color.New(color.FgGreen).Fprintf(outputWriter, "[Complete] Task %d marked as completed: %s\n", taskNum, task.Description)

	if taskCount > 0 {
		fmt.Fprintf(outputWriter, "\nRemaining tasks:\n")
		for i, remainingTask := range remainingTasks {
			fmt.Fprintf(outputWriter, "  %d) %s\n", i+1, remainingTask.Description)
		}
	} else {
		fmt.Fprintf(outputWriter, "\nNo remaining tasks.\n")
	}
}

// refreshTaskList manually triggers a task list refresh
func refreshTaskList(littleguy *LittleGuy) {
	color.New(color.FgCyan).Fprintf(outputWriter, "\n[Refresh] Refreshing task list from git changes...\n")

	err := RefreshTaskListFromGitChanges(littleguy.conversationID)
	if err != nil {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Failed to refresh task list: %v\n", err)
		return
	}

	color.New(color.FgGreen).Fprintf(outputWriter, "[Refresh] Task list updated with latest changes\n")
}

// displayCommandMenu shows available special commands for DCE
func displayCommandMenu() {
	color.New(color.FgGreen).Fprintf(outputWriter, "\n[Commands] Available DCE Commands:\n")
	fmt.Fprint(outputWriter, "  /t, /task, /tasks              - Show the current task list (concise)\n")
	fmt.Fprint(outputWriter, "  /t -v | /t v | /t verbose      - Show the task list with additional details\n")
	fmt.Fprint(outputWriter, "  /a <description>, /add <desc>  - Add a new task to the task list\n")
	fmt.Fprint(outputWriter, "  /dce on|off|status             - Control DCE monitoring\n")
	fmt.Fprint(outputWriter, "  /priority                      - Show current task priorities\n")
	fmt.Fprint(outputWriter, "  /priority <num> <level>        - Set task priority (low/medium/high)\n")
	fmt.Fprint(outputWriter, "  /complete <num>                - Mark a task as completed\n")
	fmt.Fprint(outputWriter, "  /refresh                        - Manually refresh task list from git\n")
	fmt.Fprint(outputWriter, "  /status                         - Show detailed DCE status\n")
	fmt.Fprint(outputWriter, "  /c, /cmds, /commands, /help     - Show this command menu\n")
}
