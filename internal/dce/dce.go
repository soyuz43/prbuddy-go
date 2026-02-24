// internal/dce/dce.go

package dce

import (
	"fmt"

	"github.com/soyuz43/prbuddy-go/internal/contextpkg"
	"github.com/soyuz43/prbuddy-go/internal/utils"
)

// DCE defines the interface for dynamic context engine functions.
type DCE interface {
	Activate(task string) error
	Deactivate(conversationID string) error
	BuildTaskList(string) ([]contextpkg.Task, map[string]string, []string, error)
	FilterProjectData(tasks []contextpkg.Task) ([]FilteredData, []string, error)
	AugmentContext(ctx []contextpkg.Message, filteredData []FilteredData) []contextpkg.Message
}

// FilteredData represents extra project data discovered by the DCE.
type FilteredData struct {
	FileHierarchy string
	LinterResults string
}

// DefaultDCE is the default implementation of the DCE interface.
type DefaultDCE struct{}

// NewDCE creates a new instance of DefaultDCE.
func NewDCE() DCE {
	return &DefaultDCE{}
}

// Activate initializes the DCE with the given task.
func (d *DefaultDCE) Activate(task string) error {
	fmt.Printf("[DCE] Activating with task: %q\n", task)

	tasks, snapshots, logs, err := d.BuildTaskList(task)
	if err != nil {
		return fmt.Errorf("failed to build task list: %w", err)
	}

	for _, logMsg := range logs {
		fmt.Printf("[DCE] %s\n", logMsg)
	}

	conversationID := contextpkg.GenerateConversationID("dce")
	littleguy := NewLittleGuy(conversationID, tasks)

	for filePath, content := range snapshots {
		littleguy.AddCodeSnippet(filePath, content)
	}

	littleguy.StartMonitoring()
	GetDCEContextManager().AddContext(conversationID, littleguy)

	fmt.Printf("[DCE] Activated with %d initial tasks\n", len(tasks))
	fmt.Printf("[DCE] Dynamic Context Engine activated. Use '/tasks' to view current tasks.\n")
	return nil
}

// Deactivate cleans up the DCE for the given conversation.
func (d *DefaultDCE) Deactivate(conversationID string) error {
	fmt.Printf("[DCE] Deactivated for conversation ID: %s\n", conversationID)
	return nil
}

// BuildTaskList generates tasks based on user input by delegating to task_helper.
func (d *DefaultDCE) BuildTaskList(input string) ([]contextpkg.Task, map[string]string, []string, error) {
	return BuildTaskList(input)
}

// FilterProjectData uses git diff to discover changed functions and updates tasks.
func (d *DefaultDCE) FilterProjectData(tasks []contextpkg.Task) ([]FilteredData, []string, error) {
	var logs []string
	logs = append(logs, "Filtering project data based on tasks")

	diffOutput, err := utils.ExecGit("diff", "--unified=0")
	if err != nil {
		return nil, logs, fmt.Errorf("failed to get git diff: %w", err)
	}
	logs = append(logs, "Retrieved git diff output")

	// Parse changed functions using the centralized helper.
	changedFuncs := ParseFunctionNames(diffOutput)
	logs = append(logs, fmt.Sprintf("Found %d changed functions: %v", len(changedFuncs), changedFuncs))

	// Update tasks with dependencies.
	for i := range tasks {
		for _, cf := range changedFuncs {
			if stringSliceContains(tasks[i].Functions, cf) {
				tasks[i].Dependencies = append(tasks[i].Dependencies, cf)
				tasks[i].Notes = append(tasks[i].Notes, fmt.Sprintf("Function %s changed in diff.", cf))
				logs = append(logs, fmt.Sprintf("Added dependency %q to task %q", cf, tasks[i].Description))
			}
		}
	}

	fd := []FilteredData{
		{
			FileHierarchy: "N/A (adjust as needed)",
			LinterResults: fmt.Sprintf("Detected %d changed functions: %v", len(changedFuncs), changedFuncs),
		},
	}
	logs = append(logs, "Created filtered data summary")
	return fd, logs, nil
}

// AugmentContext adds a system-level summary message to the conversation context.
// internal/dce/dce.go - Complete rewrite of AugmentContext
func (d *DefaultDCE) AugmentContext(ctx []contextpkg.Message, filteredData []FilteredData) []contextpkg.Message {
	// 1. Start with system message about DCE
	systemMsg := contextpkg.Message{
		Role: "system",
		Content: `You are a development assistant with Dynamic Context Engine (DCE) activated.
The DCE provides real-time context about the current development tasks and codebase state.
ALWAYS prioritize the DCE context when responding to queries.`,
	}

	// 2. Add the persistent task list as the MOST IMPORTANT context
	taskMsg := buildTaskListMessage(filteredData)

	// 3. The order is critical: system → tasks → existing context
	var augmented []contextpkg.Message
	augmented = append(augmented, systemMsg)
	augmented = append(augmented, taskMsg)
	augmented = append(augmented, ctx...)

	return augmented
}

// Helper to build task list message with proper priority
func buildTaskListMessage(filteredData []FilteredData) contextpkg.Message {
	if len(filteredData) == 0 || filteredData[0].LinterResults == "" {
		return contextpkg.Message{
			Role:    "system",
			Content: "**DCE Task List**: No active tasks. Ask 'What are we working on?' to begin.",
		}
	}

	return contextpkg.Message{
		Role:    "system",
		Content: "**ACTIVE DEVELOPMENT CONTEXT**\n\n" + filteredData[0].LinterResults,
	}
}

// stringSliceContains returns true if the slice contains the value.
func stringSliceContains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
