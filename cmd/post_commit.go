// cmd/post_commit.go

package cmd

import (
	"bytes"
	"context"
	"errors"
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

	// New: allow users to disable gh PR creation explicitly
	createPR bool
)

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
	postCommitCmd.Flags().BoolVar(&extensionActive, "extension-active", false, "Indicates extension connectivity check")
	postCommitCmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Disable interactive prompts")

	// New: default true, but can be turned off (especially useful in CI or on non-GitHub repos)
	postCommitCmd.Flags().BoolVar(&createPR, "create-pr", true, "Attempt to create a PR via gh after generating draft")
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

	// Clean up LLM formatting for storage/gh
	cleanDraft := utils.StripOuterMarkdownCodeFence(draftPR)
	cleanDraft = strings.TrimSpace(cleanDraft)

	// Save logs + draft.md first (even if extension/gh fails)
	logDir, logErr := saveConversationLogs(branchName, commitHash, cleanDraft)
	if logErr != nil {
		fmt.Printf("[PRBuddy-Go] Logging error: %v\n", logErr)
	}

	if logDir != "" {
		if err := writeDraftFile(logDir, cleanDraft); err != nil {
			fmt.Printf("[PRBuddy-Go] Failed to write draft.md: %v\n", err)
		}
	}

	// Output strategy: extension-first if enabled; otherwise terminal.
	// Avoid double-printing.
	if extensionActive {
		if commErr := communicateWithExtension(branchName, commitHash, cleanDraft); commErr != nil {
			handleExtensionFailure(cleanDraft, commErr)
		}
	} else {
		presentTerminalOutput(cleanDraft)
	}

	// Try to create PR (never fail the hook)
	if createPR && logDir != "" {
		draftPath := filepath.Join(logDir, "draft.md")

		title := utils.ExtractPRTitleFromMarkdown(cleanDraft)
		if title == "" {
			// fallback: commit first line
			title = fallbackTitleFromCommit(commitHash)
			if title == "" {
				title = fmt.Sprintf("PRBuddy Draft (%s)", commitHash[:7])
			}
		}

		base, baseErr := detectBaseBranch()
		if baseErr != nil {
			// Not fatal — we can still try gh without base, but it’s safer to provide one.
			// Prefer a safe default.
			base = "main"
		}

		if ok, whyNot := shouldCreatePRWithGH(); !ok {
			fmt.Printf("[PRBuddy-Go] Skipping gh PR create: %s\n", whyNot)
		} else {
			url, err := createPRWithGH(title, draftPath, branchName, base)
			if err != nil {
				fmt.Printf("[PRBuddy-Go] gh pr create failed: %v\n", err)
				fmt.Println("[PRBuddy-Go] Tip: check `gh auth status` and ensure your repo remote points to GitHub.")
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
	branchName = strings.TrimSpace(branchName)
	if branchName == "HEAD" || branchName == "" {
		return "", "", "", fmt.Errorf("detached HEAD: cannot determine branch for PR")
	}

	commitHash, err := utils.ExecGit("rev-parse", "HEAD")
	if err != nil {
		return "", "", "", fmt.Errorf("commit hash retrieval failed: %w", err)
	}
	commitHash = strings.TrimSpace(commitHash)

	commitMessage, diffs, err := llm.GeneratePreDraftPR()
	if err != nil {
		return "", "", "", fmt.Errorf("pre-draft generation failed: %w", err)
	}

	if strings.TrimSpace(diffs) == "" {
		return "", "", "", fmt.Errorf("no detectable changes")
	}

	draftPR, err := llm.GenerateDraftPR(commitMessage, diffs)
	if err != nil {
		return "", "", "", fmt.Errorf("draft generation failed: %w", err)
	}

	return branchName, commitHash, draftPR, nil
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
	// Make this a best-effort call with timeout; don’t block hooks forever.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "code", "--activate-extension", "prbuddy.extension")
	_ = cmd.Run() // ignore errors; extension may not exist
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

	for i := 0; i < extensionAttempts; i++ {
		resp, err := client.Post(
			fmt.Sprintf("http://localhost:%d/extension", port),
			"application/json",
			strings.NewReader(string(jsonPayload)),
		)
		if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
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
	fmt.Printf("\n%s\n🚀 Draft PR Generated\n%s\n%s\n%s\n\n", line, line, draft, line)
}

func writeDraftFile(logDir, draft string) error {
	path := filepath.Join(logDir, "draft.md")
	return os.WriteFile(path, []byte(draft+"\n"), 0644)
}

// saveConversationLogs returns the logDir so we can also write draft.md there.
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

	if err := os.WriteFile(filepath.Join(logDir, "conversation.json"), []byte(conversationJSON), 0644); err != nil {
		return logDir, err
	}

	// store the actual draft content here too (not "Draft generated")
	draftContext := []contextpkg.Message{
		{Role: "system", Content: "Initial draft context"},
		{Role: "assistant", Content: draft},
	}
	draftContextJSON, err := utils.MarshalJSON(draftContext)
	if err != nil {
		return logDir, err
	}

	if err := os.WriteFile(filepath.Join(logDir, "draft_context.json"), []byte(draftContextJSON), 0644); err != nil {
		return logDir, err
	}

	return logDir, nil
}

func shouldCreatePRWithGH() (bool, string) {
	if _, err := exec.LookPath("gh"); err != nil {
		return false, "gh not found in PATH"
	}

	// Check gh auth quickly; do not assume env is clean.
	ok, err := ghAuthOK()
	if err != nil {
		return false, fmt.Sprintf("gh auth check error: %v", err)
	}
	if !ok {
		return false, "gh not authenticated (run `gh auth login`)"
	}

	// Ensure repo looks like GitHub (best effort).
	// If this fails, PR creation usually fails too.
	_, err = runGH(5*time.Second, "repo", "view", "--json", "nameWithOwner")
	if err != nil {
		return false, "gh cannot view repo (is this a GitHub remote?)"
	}

	return true, ""
}

func ghAuthOK() (bool, error) {
	_, err := runGH(5*time.Second, "auth", "status")
	if err != nil {
		// gh returns non-zero if not logged in
		return false, nil
	}
	return true, nil
}

// detectBaseBranch tries multiple strategies in order.
func detectBaseBranch() (string, error) {
	// 1) Try git symbolic ref: refs/remotes/origin/HEAD -> origin/<base>
	out, err := utils.ExecGit("symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		out = strings.TrimSpace(out)
		// refs/remotes/origin/main
		parts := strings.Split(out, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// 2) Ask GitHub via gh
	b, err := ghRepoDefaultBranch()
	if err == nil && b != "" {
		return b, nil
	}

	// 3) fallback heuristics
	if branchExists("main") {
		return "main", nil
	}
	if branchExists("master") {
		return "master", nil
	}

	return "", errors.New("could not detect base branch")
}

func ghRepoDefaultBranch() (string, error) {
	out, err := runGH(5*time.Second, "repo", "view", "--json", "defaultBranchRef", "--jq", ".defaultBranchRef.name")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func branchExists(name string) bool {
	_, err := utils.ExecGit("show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

func fallbackTitleFromCommit(commitHash string) string {
	msg, err := utils.ExecGit("log", "-1", "--pretty=%s", commitHash)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(msg)
}

// createPRWithGH runs: gh pr create --title <title> --body-file <draftPath> --head <branch> --base <base>
func createPRWithGH(title, draftPath, headBranch, baseBranch string) (string, error) {
	// Capture stdout because gh prints the PR URL there.
	out, err := runGH(30*time.Second,
		"pr", "create",
		"--title", title,
		"--body-file", draftPath,
		"--head", headBranch,
		"--base", baseBranch,
	)
	if err != nil {
		return "", err
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return "", nil
	}

	lines := utils.SplitLines(out)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line, nil
		}
	}
	return "", nil
}

// runGH executes gh with a timeout and with a sanitized environment so shell-exported
// GITHUB_TOKEN / GH_TOKEN don’t override stored gh auth.
func runGH(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", args...)

	cmd.Env = sanitizeEnvForGH(os.Environ())

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("gh timed out running: gh %s", strings.Join(args, " "))
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

func sanitizeEnvForGH(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		// These can override gh’s stored auth and cause mysterious 401s.
		if strings.HasPrefix(kv, "GITHUB_TOKEN=") ||
			strings.HasPrefix(kv, "GH_TOKEN=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func handleGenerationError(err error) {
	fmt.Printf("[PRBuddy-Go] Critical error: %v\n", err)
	fmt.Println("Failed to generate draft PR. Check git status and try again.")
}
