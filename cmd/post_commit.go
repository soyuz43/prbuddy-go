// cmd/post_commit.go

package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/soyuz43/prbuddy-go/internal/contextpkg"
	"github.com/soyuz43/prbuddy-go/internal/llm"
	"github.com/soyuz43/prbuddy-go/internal/utils"
	"github.com/spf13/cobra"
)

var (
	extensionActive   bool
	nonInteractive    bool
	extensionAttempts = 3
	extensionDelay    = 500 * time.Millisecond
)

// ConversationLog represents the structure for logging conversations
type ConversationLog struct {
	BranchName string               `json:"branch_name"`
	CommitHash string               `json:"commit_hash"`
	Messages   []contextpkg.Message `json:"messages"`
}

var postCommitCmd = &cobra.Command{
	Use:   "post-commit",
	Short: "Handle post-commit automation",
	Long:  `Generates PR drafts and coordinates with VS Code extension when available`,
	Run:   runPostCommit,
}

func init() {
	postCommitCmd.Flags().BoolVar(&extensionActive, "extension-active", false,
		"Indicates extension connectivity check")
	postCommitCmd.Flags().BoolVar(&nonInteractive, "non-interactive", false,
		"Disable interactive prompts")
	rootCmd.AddCommand(postCommitCmd)
}

func runPostCommit(cmd *cobra.Command, args []string) {
	if !nonInteractive {
		fmt.Println("[PRBuddy-Go] Starting post-commit workflow...")
	}

	branchName, commitHash, draftPR, err := generateDraftPR()
	if err != nil {
		handleGenerationError(err)
		return
	}

	// Strip outer ``` fences so files/logs/gh body are clean.
	cleanDraft := utils.StripOuterMarkdownCodeFence(draftPR)

	// Show output in terminal (choose raw or clean; clean is usually nicer)
	presentTerminalOutput(cleanDraft)

	// Save logs + draft.md
	logDir, logErr := saveConversationLogs(branchName, commitHash, cleanDraft)
	if logErr != nil {
		fmt.Printf("[PRBuddy-Go] Logging error: %v\n", logErr)
	}

	// Also try sending to extension if enabled (unchanged behavior, but use clean draft)
	if extensionActive {
		if commErr := communicateWithExtension(branchName, commitHash, cleanDraft); commErr != nil {
			handleExtensionFailure(cleanDraft, commErr)
		}
	}

	// Create PR via gh and print URL
	if logDir != "" {
		draftPath := filepath.Join(logDir, "draft.md")
		if err := os.WriteFile(draftPath, []byte(cleanDraft), 0644); err != nil {
			fmt.Printf("[PRBuddy-Go] Failed to write draft.md: %v\n", err)
		} else {
			title := utils.ExtractPRTitleFromMarkdown(cleanDraft)
			if title == "" {
				title = fmt.Sprintf("PRBuddy Draft (%s)", commitHash[:7])
			}

			url, err := createPRWithGH(title, draftPath, branchName)
			if err != nil {
				fmt.Printf("[PRBuddy-Go] gh pr create failed: %v\n", err)
				fmt.Println("[PRBuddy-Go] Tip: ensure you ran `gh auth login` and have permission to open PRs.")
			} else if url != "" {
				fmt.Printf("[PRBuddy-Go] PR created: %s\n", url)
			} else {
				fmt.Println("[PRBuddy-Go] PR created (no URL returned).")
			}
		}
	}

	if !nonInteractive {
		fmt.Println("[PRBuddy-Go] Post-commit workflow completed")
	}
}

func generateDraftPR() (string, string, string, error) {
	branchName, err := utils.ExecGit("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", "", "", fmt.Errorf("branch detection failed: %w", err)
	}

	commitHash, err := utils.ExecGit("rev-parse", "HEAD")
	if err != nil {
		return "", "", "", fmt.Errorf("commit hash retrieval failed: %w", err)
	}

	commitMessage, diffs, err := llm.GeneratePreDraftPR()
	if err != nil {
		return "", "", "", fmt.Errorf("pre-draft generation failed: %w", err)
	}

	if diffs == "" {
		return "", "", "", fmt.Errorf("no detectable changes")
	}

	draftPR, err := llm.GenerateDraftPR(commitMessage, diffs)
	if err != nil {
		return "", "", "", fmt.Errorf("draft generation failed: %w", err)
	}

	return strings.TrimSpace(branchName), strings.TrimSpace(commitHash), draftPR, nil
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
	cmd := exec.Command("code", "--activate-extension", "prbuddy.extension")
	return cmd.Run()
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

	for i := 0; i < extensionAttempts; i++ {
		resp, err := client.Post(
			fmt.Sprintf("http://localhost:%d/extension", port),
			"application/json",
			strings.NewReader(string(jsonPayload)),
		)

		if err == nil && resp.StatusCode == http.StatusOK {
			return nil
		}

		time.Sleep(extensionDelay)
	}

	return fmt.Errorf("failed after %d attempts", extensionAttempts)
}

func handleExtensionFailure(draft string, err error) {
	fmt.Printf("\n[PRBuddy-Go] Extension communication failed: %v\n", err)
	presentTerminalOutput(draft)
}

func presentTerminalOutput(draft string) {
	const line = "═══════════════════════════════════════════════════"
	fmt.Printf("\n%s\n🚀 Draft PR Generated\n%s\n%s\n%s\n\n",
		line, line, draft, line)
}

// saveConversationLogs now returns the logDir so we can also write draft.md there.
func saveConversationLogs(branch, hash, draft string) (string, error) {
	repoPath, err := utils.GetRepoPath()
	if err != nil {
		return "", fmt.Errorf("repo path detection: %w", err)
	}

	logDir := filepath.Join(repoPath, ".git", "pr_buddy_db",
		utils.SanitizeBranchName(branch), fmt.Sprintf("commit-%s", hash[:7]))

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", fmt.Errorf("log directory creation: %w", err)
	}

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

	if err := saveFile(logDir, "conversation.json", string(conversationJSON)); err != nil {
		return logDir, err
	}

	draftContext := []contextpkg.Message{
		{Role: "system", Content: "Initial draft context"},
		{Role: "assistant", Content: draft},
	}

	draftContextJSON, err := utils.MarshalJSON(draftContext)
	if err != nil {
		return logDir, err
	}

	if err := saveFile(logDir, "draft_context.json", string(draftContextJSON)); err != nil {
		return logDir, err
	}

	return logDir, nil
}

func saveFile(dir, filename, content string) error {
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("file write failed: %w", err)
	}
	return nil
}

// createPRWithGH runs: gh pr create --title <title> --body-file <draftPath> --head <branch>
func createPRWithGH(title, draftPath, branch string) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("GitHub CLI (gh) not found in PATH: %w", err)
	}

	// IMPORTANT: we capture stdout because gh prints the PR URL there.
	cmd := exec.Command(
		"gh", "pr", "create",
		"--title", title,
		"--body-file", draftPath,
		"--head", branch,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("%s", errMsg)
	}

	// Usually gh outputs the PR URL on a line by itself.
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", nil
	}
	// Take the last non-empty line as the URL (works even if gh prints extra text).
	lines := utils.SplitLines(out)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line, nil
		}
	}
	return "", nil
}

func handleGenerationError(err error) {
	fmt.Printf("[PRBuddy-Go] Critical error: %v\n", err)
	fmt.Println("Failed to generate draft PR. Check git status and try again.")
}
