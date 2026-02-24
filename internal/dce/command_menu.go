// internal/dce/command_menu.go

package dce

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/fatih/color"
)

// outputWriter is used for all command output to enable testability
var outputWriter io.Writer = os.Stdout

// SetOutput allows setting a custom output writer for testing
func SetOutput(w io.Writer) {
	outputWriter = w
}

// HandleDCECommandMenu checks if the user input is a recognized command
// and executes the appropriate function.
//
// Returns 'true' if the input matched a command and was handled internally.
// Returns 'false' if the input did not match a command, so it can be passed to the LLM.
func HandleDCECommandMenu(input string, littleguy *LittleGuy) bool {
	trimmedInput := strings.TrimSpace(input)
	lowerInput := strings.ToLower(trimmedInput)

	switch {
	// Handle both singular and plural forms of /task(s)
	case lowerInput == "/task", lowerInput == "/tasks":
		displayTaskList(littleguy, false)
		return true

	case lowerInput == "/task -v", lowerInput == "/tasks -v",
		lowerInput == "/task verbose", lowerInput == "/tasks verbose":
		displayTaskList(littleguy, true)
		return true

	// Handle /add command to add new tasks (with or without space after /add)
	case strings.HasPrefix(lowerInput, "/add") && len(trimmedInput) > 4:
		handleAddCommand(trimmedInput, littleguy)
		return true
	case lowerInput == "/add":
		handleAddCommand(trimmedInput, littleguy)
		return true

	case strings.HasPrefix(lowerInput, "/dce "):
		handleDCEControlCommand(trimmedInput[5:], littleguy)
		return true

	case lowerInput == "/commands", lowerInput == "/cmds", lowerInput == "/help":
		displayCommandMenu()
		return true

	case strings.HasPrefix(lowerInput, "/priority"):
		handlePriorityCommand(trimmedInput, littleguy)
		return true

	case lowerInput == "/complete", strings.HasPrefix(lowerInput, "/complete "):
		handleCompleteCommand(trimmedInput, littleguy)
		return true

	case lowerInput == "/refresh":
		refreshTaskList(littleguy)
		return true

	case lowerInput == "/status":
		displayDCEStatus(littleguy)
		return true

	default:
		return false
	}
}

// handleAddCommand processes /add commands to add new tasks to the task list
func handleAddCommand(input string, littleguy *LittleGuy) {
	// Extract the task description after "/add"
	var taskDescription string
	if len(input) > 4 {
		taskDescription = strings.TrimSpace(input[4:])
	}

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

	// Add the new tasks to the current task list
	littleguy.UpdateTaskList(tasks)

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

// displayTaskList prints the current task list.
// If verbose=true, it includes additional details like files, functions, notes, etc.
func displayTaskList(littleguy *LittleGuy, verbose bool) {
	color.New(color.FgCyan).Fprintf(outputWriter, "\n[Task List] Current Tasks:\n")

	littleguy.mutex.RLock()
	tasks := littleguy.tasks
	littleguy.mutex.RUnlock()

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

// handleDCEControlCommand processes DCE control commands like "on" and "off"
func handleDCEControlCommand(command string, littleguy *LittleGuy) {
	lowerCmd := strings.ToLower(strings.TrimSpace(command))

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
	littleguy.mutex.RUnlock()

	fmt.Fprintf(outputWriter, "  Status: %s\n", status)
	fmt.Fprintf(outputWriter, "  Active Tasks: %d\n", taskCount)
	fmt.Fprintf(outputWriter, "  Monitoring Interval: %v\n", littleguy.pollInterval)
	fmt.Fprintf(outputWriter, "  Features: Dynamic task tracking, Git change monitoring\n")
}

// handlePriorityCommand allows users to set task priorities
// FIX: Corrected logic to properly distinguish between viewing and setting priorities
func handlePriorityCommand(input string, littleguy *LittleGuy) {
	parts := strings.Fields(input)

	// parts[0] = "/priority"
	// For viewing: len(parts) == 1 (just "/priority")
	// For setting: len(parts) == 3 ("/priority" + taskNum + level)

	if len(parts) == 1 {
		// Display current priorities with formatted labels
		color.New(color.FgCyan).Fprintf(outputWriter, "\n[Priority] Current task priorities:\n")
		littleguy.mutex.RLock()
		defer littleguy.mutex.RUnlock()

		for i, task := range littleguy.tasks {
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

	// Set priority for a specific task
	taskNumStr := parts[1]
	priorityLevel := strings.ToLower(parts[2])

	// Convert task number
	taskNum, err := strconv.Atoi(taskNumStr)
	if err != nil || taskNum < 1 {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Invalid task number\n")
		return
	}

	// Update task priority
	littleguy.mutex.Lock()
	defer littleguy.mutex.Unlock()

	if taskNum > len(littleguy.tasks) {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Task number out of range\n")
		return
	}

	// Remove existing priority notes
	task := &littleguy.tasks[taskNum-1]
	newNotes := []string{}
	for _, note := range task.Notes {
		lowerNote := strings.ToLower(note)
		if !strings.Contains(lowerNote, "priority") {
			newNotes = append(newNotes, note)
		}
	}

	// Add new priority note
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

// handleCompleteCommand marks tasks as completed and shows remaining tasks
func handleCompleteCommand(input string, littleguy *LittleGuy) {
	parts := strings.Fields(input)

	if len(parts) < 2 {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Usage: /complete <task-number>\n")
		return
	}

	// Convert task number
	taskNum, err := strconv.Atoi(parts[1])
	if err != nil || taskNum < 1 {
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Invalid task number\n")
		return
	}

	// Mark task as completed
	littleguy.mutex.Lock()

	if taskNum > len(littleguy.tasks) {
		littleguy.mutex.Unlock()
		color.New(color.FgRed).Fprintf(outputWriter, "[X] Task number out of range\n")
		return
	}

	task := littleguy.tasks[taskNum-1]

	// Remove the task from tasks and add to completed
	littleguy.tasks = append(littleguy.tasks[:taskNum-1], littleguy.tasks[taskNum:]...)
	littleguy.completed = append(littleguy.completed, task)

	taskCount := len(littleguy.tasks)
	littleguy.mutex.Unlock()

	color.New(color.FgGreen).Fprintf(outputWriter, "[Complete] Task %d marked as completed: %s\n", taskNum, task.Description)

	// Show remaining tasks
	if taskCount > 0 {
		fmt.Fprintf(outputWriter, "\nRemaining tasks:\n")
		littleguy.mutex.RLock()
		for i, remainingTask := range littleguy.tasks {
			fmt.Fprintf(outputWriter, "  %d) %s\n", i+1, remainingTask.Description)
		}
		littleguy.mutex.RUnlock()
	} else {
		fmt.Fprintf(outputWriter, "\nNo remaining tasks.\n")
	}
}

// refreshTaskList manually triggers a task list refresh
func refreshTaskList(littleguy *LittleGuy) {
	color.New(color.FgCyan).Fprintf(outputWriter, "\n[Refresh] Refreshing task list from git changes...\n")

	// Directly call RefreshTaskListFromGitChanges with the conversation ID
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
	fmt.Fprint(outputWriter, "  /task or /tasks        - Show the current task list (concise)\n")
	fmt.Fprint(outputWriter, "  /task verbose          - Show the task list with additional details\n")
	fmt.Fprint(outputWriter, "  /add <description>     - Add a new task to the task list\n")
	fmt.Fprint(outputWriter, "  /dce on                - Activate the Dynamic Context Engine\n")
	fmt.Fprint(outputWriter, "  /dce off               - Deactivate the Dynamic Context Engine\n")
	fmt.Fprint(outputWriter, "  /dce status            - Show DCE status and statistics\n")
	fmt.Fprint(outputWriter, "  /priority              - Show current task priorities\n")
	fmt.Fprint(outputWriter, "  /priority <num> <level>- Set task priority (low/medium/high)\n")
	fmt.Fprint(outputWriter, "  /complete <num>        - Mark a task as completed\n")
	fmt.Fprint(outputWriter, "  /refresh               - Manually refresh task list from git\n")
	fmt.Fprint(outputWriter, "  /status                - Show detailed DCE status\n")
	fmt.Fprint(outputWriter, "  /commands, /cmds, /help- Show this command menu\n")
}
