// cmd/root.go

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/soyuz43/prbuddy-go/internal/contextpkg"
	"github.com/soyuz43/prbuddy-go/internal/dce"
	"github.com/soyuz43/prbuddy-go/internal/llm"
	"github.com/soyuz43/prbuddy-go/internal/utils"
	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Initialize the root command's Run function here to break the initialization cycle
func init() {
	rootCmd.Run = runRootCommand
}

func runRootCommand(cmd *cobra.Command, args []string) {
	color.Cyan("[PRBuddy-Go] Starting...\n")

	initialized, err := isInitialized()
	if err != nil {
		color.Red("Error checking initialization status: %v\n", err)
		os.Exit(1)
	}

	if initialized {
		runInteractiveSession()
	} else {
		showInitialMenu()
	}
}

func runInteractiveSession() {
	color.Green("\nPRBuddy-Go is initialized in this repository.\n")

	fmt.Println(bold("Available Commands:"))
	fmt.Printf("   %s    - %s\n", green("generate pr"), "Generate a draft pull request")
	fmt.Printf("   %s    - %s\n", green("what changed"), "Show changes since the last commit")
	fmt.Printf("   %s    - %s\n", green("quickassist"), "Open a persistent chat session with the assistant")
	fmt.Printf("   %s    - %s\n", green("dce"), "Dynamic Context Engine")
	fmt.Printf("   %s    - %s\n", green("context save"), "Save current conversation context")
	fmt.Printf("   %s    - %s\n", green("context load"), "Reload saved context for current branch/commit")
	fmt.Printf("   %s    - %s\n", green("pr create"), "Create PR from saved draft")
	fmt.Printf("   %s    - %s\n", green("serve"), "Start API server for extension integration")
	fmt.Printf("   %s    - %s\n", green("map"), "Generate project scaffolds")
	fmt.Printf("   %s    - %s\n", green("help"), "Show help information")
	fmt.Printf("   %s    - %s\n", red("remove"), "Uninstall PRBuddy-Go and delete all associated files")
	fmt.Printf("   %s    - %s\n", green("exit"), "Exit the tool")

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("\n%s ", cyan(">"))
		input, err := reader.ReadString('\n')
		if err != nil {
			color.Red("Error reading input: %v\n", err)
			continue
		}

		parts := strings.Fields(strings.TrimSpace(input))
		if len(parts) == 0 {
			continue
		}

		// Handle command shortcuts and aliases
		command := strings.ToLower(parts[0])
		args := parts[1:]

		// Map shortcuts to full commands
		switch command {
		case "g", "gen":
			command = "generate"
		case "w", "changes":
			command = "what"
		case "q", "qa":
			command = "quickassist"
		case "s":
			command = "serve"
		case "p":
			command = "pr"
		}

		// Properly find and execute the Cobra command
		cmd, _, err := rootCmd.Find(append([]string{command}, args...))
		if err != nil {
			color.Red("[PRBuddy-Go] Unknown command: '%s'\n", strings.Join(parts, " "))
			continue
		}

		// Set args for the command
		cmd.SetArgs(args)

		// Execute the command through Cobra's proper flow
		if err := cmd.Execute(); err != nil {
			color.Red("[PRBuddy-Go] Error: %v\n", err)
		}
	}
}

func handleGeneratePR() {
	color.Cyan("\n[PRBuddy-Go] Generating draft PR...\n")
	runPostCommit(nil, nil)
}

func handleWhatChanged() {
	color.Cyan("\n[PRBuddy-Go] Checking changes...\n")
	// This is no longer used directly - handled through Cobra execution
}

func handleQuickAssist(args []string, reader *bufio.Reader) {
	if len(args) > 0 {
		singleQueryResponse(strings.Join(args, " "))
		return
	}
	startInteractiveQuickAssist(reader)
}

func singleQueryResponse(query string) {
	if query == "" {
		color.Red("No question provided.\n")
		return
	}

	resp, err := llm.HandleQuickAssist("", query)
	if err != nil {
		color.Red("Error: %v\n", err)
		return
	}

	color.Yellow("\nQuickAssist Response:\n")
	color.Cyan(resp)
	fmt.Println()
}

func startInteractiveQuickAssist(reader *bufio.Reader) {
	color.Cyan("\n[PRBuddy-Go] Quick Assist - Interactive Mode")
	color.Yellow("Type 'exit' or 'q' to end the session.\n")

	conversationID := ""

	for {
		color.Green("\nYou:")
		fmt.Print("> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			color.Red("Error reading input: %v\n", err)
			continue
		}

		query := strings.TrimSpace(input)
		if shouldExit(query) {
			color.Cyan("\nEnding session.\n")
			return
		}

		if query == "" {
			color.Yellow("No question provided.\n")
			continue
		}

		resp, err := llm.HandleQuickAssist(conversationID, query)
		if err != nil {
			color.Red("Error: %v\n", err)
			continue
		}

		color.Blue("\nAssistant:\n")
		color.Cyan(resp)
		fmt.Println()

		if conv, exists := contextpkg.ConversationManagerInstance.GetConversation(conversationID); exists {
			conv.AddMessage("assistant", resp)
		}
	}
}

func handleDCECommand() {
	color.Cyan("[PRBuddy-Go] Dynamic Context Engine - Interactive Mode")
	color.Yellow("Type 'exit'/'q' or '/exit' to end the session. Use '/bg' or '/suspend' to leave DCE running in background.")

	dceInstance := dce.NewDCE()
	reader := bufio.NewReader(os.Stdin)

	before := snapshotDCEContextIDs()

	color.Green("What are we working on today?")
	fmt.Print("> ")

	var task string
	for {
		firstInput, err := reader.ReadString('\n')
		if err != nil {
			color.Red("Error reading input: %v", err)
			return
		}

		query := strings.TrimSpace(firstInput)
		if query == "" {
			color.Yellow("Please provide a task description (or type 'exit').")
			fmt.Print("> ")
			continue
		}
		if shouldExit(query) || strings.EqualFold(query, "/exit") || strings.EqualFold(query, "/q") || strings.EqualFold(query, "/e") {
			color.Cyan("Exiting DCE.\n")
			return
		}
		if strings.HasPrefix(query, "/") {
			color.Yellow("DCE isn't active yet. Enter a task description to start, or type 'exit'.")
			color.Yellow("Tip: once active, use /t, /a <desc>, /help, /exit, /bg.\n")
			fmt.Print("> ")
			continue
		}

		task = query
		break
	}

	if err := dceInstance.Activate(task); err != nil {
		color.Red("Error activating DCE: %v", err)
		return
	}

	after := snapshotDCEContextIDs()
	conversationID := findNewDCEContextID(before, after)

	if conversationID == "" {
		dce.GetDCEContextManager().ForEachContext(func(cid string, _ *dce.LittleGuy) {
			if conversationID == "" {
				conversationID = cid
			}
		})
	}

	if conversationID == "" {
		color.Red("Failed to get conversation ID")
		return
	}

	color.Green("DCE is active. Type your queries or DCE commands (/t, /a, /status, /help, /exit).")

	for {
		color.Green("You:")
		fmt.Print("> ")

		line, err := reader.ReadString('\n')
		if err != nil {
			color.Red("Error reading input: %v", err)
			break
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		// Plain exits
		if shouldExit(input) {
			break
		}

		// Slash exits / background controls
		lower := strings.ToLower(input)
		if lower == "/exit" || lower == "/q" || lower == "/e" {
			break
		}
		if lower == "/bg" || lower == "/suspend" {
			// Leave interactive DCE mode but keep monitoring running.
			color.Cyan("Leaving DCE session. Monitoring remains ACTIVE in background.\n")
			return
		}

		// Handle any other slash command via DCE command menu.
		if strings.HasPrefix(input, "/") {
			littleguy, _ := dce.GetDCEContextManager().GetContext(conversationID)
			if littleguy != nil && dce.HandleDCECommandMenu(input, littleguy) {
				continue
			}
			color.Yellow("[!] DCE context not found; cannot execute command. Type 'exit' to leave.")
			continue
		}

		// Regular query: talk to assistant (no DCE re-activation).
		response, err := llm.HandleQuickAssist(conversationID, input)
		if err != nil {
			color.Red("Error processing request: %v", err)
			continue
		}

		color.Cyan("Assistant:")
		fmt.Println(response)
	}

	// Best-effort stop monitoring when explicitly exiting.
	if lg, ok := dce.GetDCEContextManager().GetContext(conversationID); ok && lg != nil {
		lg.StopMonitoring()
	}

	_ = dceInstance.Deactivate(conversationID)
	color.Cyan("DCE deactivated. Exiting.")
}

func snapshotDCEContextIDs() map[string]struct{} {
	ids := make(map[string]struct{})
	dce.GetDCEContextManager().ForEachContext(func(cid string, _ *dce.LittleGuy) {
		ids[cid] = struct{}{}
	})
	return ids
}

func findNewDCEContextID(before, after map[string]struct{}) string {
	for cid := range after {
		if _, existed := before[cid]; !existed {
			return cid
		}
	}
	return ""
}

func handleMapCommand() {
	mapCmd.Run(nil, nil)
}

func handleServeCommand() {
	color.Cyan("\n[PRBuddy-Go] Starting API server...\n")
	llm.ServeCmd.Run(nil, nil)
}

func handleRemoveCommand() {
	color.Red("\n⚠ WARNING: This will remove PRBuddy-Go from your repository! ⚠")
	color.Yellow("Are you sure? Type 'yes' to confirm: ")

	reader := bufio.NewReader(os.Stdin)
	confirmation, _ := reader.ReadString('\n')
	confirmation = strings.TrimSpace(strings.ToLower(confirmation))

	if confirmation != "yes" {
		color.Cyan("Operation cancelled.")
		return
	}

	color.Red("\n[PRBuddy-Go] Removing PRBuddy-Go from the repository...\n")
	removeCmd.Run(nil, nil)
	color.Green("\n[PRBuddy-Go] Successfully uninstalled.\n")
}

func handleContextSave() {
	branch, err := utils.GetCurrentBranch()
	if err != nil {
		color.Red("Error getting branch: %v", err)
		return
	}
	commit, err := utils.GetLatestCommit()
	if err != nil {
		color.Red("Error getting commit hash: %v", err)
		return
	}

	conv, exists := contextpkg.ConversationManagerInstance.GetConversation("")
	if !exists {
		color.Yellow("No active conversation to save.\n")
		return
	}

	if err := llm.SaveDraftContext(branch, commit, conv.BuildContext()); err != nil {
		color.Red("Failed to save context: %v", err)
		return
	}
	color.Green("Conversation context saved for %s @ %s\n", branch, commit[:7])
}

func handleContextLoad() {
	branch, err := utils.GetCurrentBranch()
	if err != nil {
		color.Red("Error getting branch: %v", err)
		return
	}
	commit, err := utils.GetLatestCommit()
	if err != nil {
		color.Red("Error getting commit hash: %v", err)
		return
	}

	context, err := llm.LoadDraftContext(branch, commit)
	if err != nil {
		color.Red("Failed to load context: %v", err)
		return
	}

	conv := contextpkg.ConversationManagerInstance.StartConversation("", "", false)
	conv.SetMessages(context)
	color.Green("Context loaded for %s @ %s.\n", branch, commit[:7])
}

func joinMessages(msgs []contextpkg.Message) string {
	var sb strings.Builder
	caser := cases.Title(language.English)
	for _, m := range msgs {
		sb.WriteString(caser.String(m.Role))
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func shouldExit(query string) bool {
	return strings.EqualFold(query, "exit") ||
		strings.EqualFold(query, "q") ||
		strings.EqualFold(query, "quit")
}

func printInitialHelp() {
	fmt.Println(bold("\nInitial Setup Commands:"))
	fmt.Printf("   %s    - %s\n", green("init"), "Initialize PRBuddy-Go in the current repository")
	fmt.Printf("   %s    - %s\n", green("help"), "Show this help information")
	fmt.Printf("   %s    - %s\n", green("exit"), "Exit the tool")
}

func printInteractiveHelp() {
	fmt.Println(bold("\nPull Request Workflow"))
	fmt.Printf("   %s    - %s\n", green("generate pr"), "Draft a new pull request with AI assistance")
	fmt.Printf("   %s    - %s\n", green("what changed"), "Show changes since your last commit")
	fmt.Printf("   %s    - %s\n", green("pr create"), "Create PR from saved draft")

	fmt.Println(bold("\nAssistant Tools"))
	fmt.Printf("   %s    - %s\n", green("quickassist"), "Chat live with the assistant (no memory)")
	fmt.Printf("   %s    - %s\n", green("dce"), "Enable Dynamic Context Engine (monitors task context)")
	fmt.Printf("   %s    - %s\n", green("context save"), "Save current conversation context")
	fmt.Printf("   %s    - %s\n", green("context load"), "Reload saved context for current branch/commit")

	fmt.Println(bold("\nProject Utilities"))
	fmt.Printf("   %s    - %s\n", green("map"), "Generate starter scaffolds for your project")
	fmt.Printf("   %s    - %s\n", green("serve"), "Start API server (for editor integration)")

	fmt.Println(bold("\nSystem"))
	fmt.Printf("   %s    - %s\n", green("help"), "Show this help information")
	fmt.Printf("   %s    - %s\n", red("remove"), "Uninstall PRBuddy-Go from this repository")
	fmt.Printf("   %s    - %s\n", green("exit"), "Exit the CLI")
}

func showInitialMenu() {
	color.Yellow("\nPRBuddy-Go is not initialized in this repository.\n")

	fmt.Println(bold("Available Commands:"))
	fmt.Printf("   %s    - %s\n", green("init"), "Initialize PRBuddy-Go in the current repository")
	fmt.Printf("   %s    - %s\n", green("help"), "Show help information")
	fmt.Printf("   %s    - %s\n", green("exit"), "Exit the tool")

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("\n%s ", cyan(">"))
		input, err := reader.ReadString('\n')
		if err != nil {
			color.Red("Error reading input: %v\n", err)
			continue
		}

		command := strings.TrimSpace(strings.ToLower(input))

		switch command {
		case "init", "i":
			initCmd.Run(nil, nil)
			return
		case "help", "h":
			printInitialHelp()
		case "exit", "e", "quit", "q":
			color.Cyan("Exiting...\n")
			return
		default:
			color.Red("Unknown command. Type 'help' for available commands.\n")
		}
	}
}
