// cmd/common.go
package cmd

import (
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// Color helper functions - defined ONLY HERE
func bold(text string) string {
	return color.New(color.Bold).SprintFunc()(text)
}

func green(text string) string {
	return color.New(color.FgGreen).SprintFunc()(text)
}

func red(text string) string {
	return color.New(color.FgRed).SprintFunc()(text)
}

func cyan(text string) string {
	return color.New(color.FgCyan).SprintFunc()(text)
}

// Global command instance - initialized without Run function
var rootCmd = &cobra.Command{
	Use:   "prbuddy-go",
	Short: "PRBuddy-Go helps automate pull request creation and code review",
	Long: `PRBuddy-Go is a tool that helps developers:
- Generate PR drafts automatically
- Analyze code changes
- Integrate with GitHub
- Streamline the code review process`,
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// isInitialized - defined ONLY HERE
func isInitialized() (bool, error) {
	// Check for the presence of the post-commit hook
	hookPath := ".git/hooks/post-commit"
	if _, err := os.Stat(hookPath); err == nil {
		return true, nil
	}

	// Check for the presence of the database directory
	dbPath := ".git/pr_buddy_db"
	if _, err := os.Stat(dbPath); err == nil {
		return true, nil
	}

	return false, nil
}
