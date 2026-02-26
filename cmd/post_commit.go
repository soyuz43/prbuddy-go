// cmd/post_commit.go
//
// Post-commit hook: Idempotent PR draft generator
// This hook ONLY generates artifacts - it never prompts or creates PRs
// It's safe to run repeatedly and won't block git operations

package cmd

import (
	"context" // Added missing import for context
	"fmt"
	"net/http"
	"os"
	"os/exec" // Added missing import for exec
	"path/filepath"
	"strings"
	"time"

	"github.com/soyuz43/prbuddy-go/internal/contextpkg"
	"github.com/soyuz43/prbuddy-go/internal/llm"
	"github.com/soyuz43/prbuddy-go/internal/utils"
	"github.com/spf13/cobra"
)

var (
	extensionActive bool
	nonInteractive  bool
)

// ConversationLog represents the structure for logging conversations
type ConversationLog struct {
	BranchName string               `json:"branch_name"`
	CommitHash string               `json:"commit_hash"`
	Messages   []contextpkg.Message `json:"messages"`
}

var postCommitCmd = &cobra.Command{
	Use:   "post-commit",
	Short: "Generate PR draft artifacts (idempotent)",
	Long: `Generates PR draft artifacts and stores them in .git/pr_buddy_db.
This hook is safe to run repeatedly and will exit immediately if artifacts already exist.
Does NOT create PRs or prompt for user input.`,
	Run: runPostCommit,
}

func init() {
	postCommitCmd.Flags().BoolVar(&extensionActive, "extension-active", false, "Indicates extension connectivity")
	postCommitCmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Disable interactive prompts")
	rootCmd.AddCommand(postCommitCmd)
}

func runPostCommit(cmd *cobra.Command, args []string) {
	// 1. Get HEAD identity
	branchName, err := utils.ExecGit("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branchName == "HEAD" || branchName == "" {
		if !nonInteractive {
			fmt.Println("[PRBuddy-Go] Skipping: detached HEAD or unknown branch")
		}
		return
	}
	branchName = strings.TrimSpace(branchName)

	commitHash, err := utils.ExecGit("rev-parse", "HEAD")
	if err != nil || commitHash == "" {
		if !nonInteractive {
			fmt.Println("[PRBuddy-Go] Skipping: could not determine commit hash")
		}
		return
	}
	commitHash = strings.TrimSpace(commitHash)

	// 2. PRIMARY GATE: Check if draft already exists
	if draftAlreadyExists(branchName, commitHash) {
		if !nonInteractive {
			fmt.Printf("[PRBuddy-Go] Skipping: draft already exists for commit %s\n", commitHash[:7])
		}
		return
	}

	// 3. Generate draft (only if needed)
	draftPR, err := generateDraftPR()
	if err != nil {
		handleGenerationError(err)
		return
	}

	// 4. Save artifacts (core responsibility of hook)
	logDir, err := saveArtifacts(branchName, commitHash, draftPR)
	if err != nil {
		fmt.Printf("[PRBuddy-Go] Warning: could not save artifacts: %v\n", err)
		return
	}

	// 5. Extension communication (best-effort)
	if extensionActive {
		if commErr := communicateWithExtension(branchName, commitHash, draftPR); commErr != nil {
			if !nonInteractive {
				fmt.Printf("[PRBuddy-Go] Extension communication failed: %v\n", commErr)
			}
		}
	} else if !nonInteractive {
		fmt.Printf("[PRBuddy-Go] Draft saved to: %s\n",
			filepath.Join(logDir, "draft.md"))
	}
}

// PRIMARY GATE: Check if we've already processed this commit
func draftAlreadyExists(branch, headHash string) bool {
	repoPath, err := utils.GetRepoPath()
	if err != nil || len(headHash) < 7 {
		return false
	}

	logDir := filepath.Join(
		repoPath,
		".git", "pr_buddy_db",
		utils.SanitizeBranchName(branch),
		fmt.Sprintf("commit-%s", headHash[:7]),
	)

	// Check for ANY artifact - indicates we've processed this commit
	return utils.FileExists(filepath.Join(logDir, "draft.md")) ||
		utils.FileExists(filepath.Join(logDir, "conversation.json"))
}

// Generate draft PR (properly without unused parameters)
func generateDraftPR() (string, error) {
	// Get commit message and diffs
	commitMessage, diffs, err := llm.GeneratePreDraftPR()
	if err != nil {
		return "", fmt.Errorf("pre-draft generation failed: %w", err)
	}

	// Skip if no changes
	if strings.TrimSpace(diffs) == "" {
		return "", fmt.Errorf("no detectable changes")
	}

	// Generate draft with ONLY the parameters the function actually accepts
	draftPR, err := llm.GenerateDraftPR(commitMessage, diffs)
	if err != nil {
		return "", fmt.Errorf("draft generation failed: %w", err)
	}

	return strings.TrimSpace(utils.StripOuterMarkdownCodeFence(draftPR)), nil
}

// Save all artifacts in one place
func saveArtifacts(branch, hash, draft string) (string, error) {
	repoPath, err := utils.GetRepoPath()
	if err != nil {
		return "", fmt.Errorf("repo path detection: %w", err)
	}

	logDir := filepath.Join(
		repoPath,
		".git", "pr_buddy_db",
		utils.SanitizeBranchName(branch),
		fmt.Sprintf("commit-%s", hash[:7]),
	)

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", fmt.Errorf("log directory creation: %w", err)
	}

	// Save draft
	if err := utils.WriteFile(
		filepath.Join(logDir, "draft.md"),
		[]byte(draft+"\n"),
	); err != nil {
		return logDir, err
	}

	// Save conversation log
	conversation := ConversationLog{
		BranchName: branch,
		CommitHash: hash,
		Messages: []contextpkg.Message{
			{Role: "system", Content: "Initiated draft generation"},
			{Role: "assistant", Content: draft},
		},
	}

	conversationJSON, err := utils.MarshalJSON(conversation)
	if err != nil {
		return logDir, err
	}
	if err := utils.WriteFile(
		filepath.Join(logDir, "conversation.json"),
		[]byte(conversationJSON),
	); err != nil {
		return logDir, err
	}

	return logDir, nil
}

func communicateWithExtension(branch, hash, draft string) error {
	if err := activateExtension(); err != nil {
		return fmt.Errorf("extension activation: %w", err)
	}

	port, err := utils.ReadPortFile()
	if err != nil {
		return fmt.Errorf("port retrieval: %w", err)
	}

	return retryCommunication(port, branch, hash, draft)
}

func activateExtension() error {
	// Best-effort. Never block hooks.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "code", "--activate-extension", "prbuddy.extension")
	_ = cmd.Run()
	return nil
}

func retryCommunication(port int, branch, hash, draft string) error {
	client := http.Client{Timeout: 2 * time.Second}

	payload := map[string]interface{}{
		"branch":    branch,
		"commit":    hash,
		"draft_pr":  draft,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	jsonPayload, err := utils.MarshalJSON(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	for i := 0; i < 3; i++ {
		resp, err := client.Post(
			fmt.Sprintf("http://localhost:%d/extension", port),
			"application/json",
			strings.NewReader(string(jsonPayload)),
		)

		if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
			return nil
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("failed after 3 attempts")
}

func handleGenerationError(err error) {
	fmt.Printf("[PRBuddy-Go] Critical error: %v\n", err)
	fmt.Println("Failed to generate draft PR. Check git status and try again.")
}
