// cmd/pr_create.go
//
// Command to create a GitHub PR from saved draft artifacts.
// This command:
// 1. Ensures the branch is pushed to remote
// 2. Uses saved draft artifacts to create the PR
// 3. Handles all GitHub-specific logic

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/soyuz43/prbuddy-go/internal/utils"
	"github.com/spf13/cobra"
)

var (
	baseBranch string
	assignees  string
	reviewers  string
	labels     string
)

var prCreateCmd = &cobra.Command{
	Use:   "pr create",
	Short: "Create a GitHub PR from saved draft artifacts",
	Long: `Creates a GitHub PR using the most recently saved draft artifacts.
This command ensures your branch is pushed to the remote before creating the PR,
making sure GitHub can properly autofill the PR details.`,
	Run: runPRCreate,
}

func init() {
	prCreateCmd.Flags().StringVar(&baseBranch, "base", "", "The base branch to create the PR against (default: detected)")
	prCreateCmd.Flags().StringVar(&assignees, "assignees", "", "Comma-separated list of GitHub users to assign to the PR")
	prCreateCmd.Flags().StringVar(&reviewers, "reviewers", "", "Comma-separated list of GitHub users to request reviews from")
	prCreateCmd.Flags().StringVar(&labels, "labels", "", "Comma-separated list of labels to add to the PR")
	rootCmd.AddCommand(prCreateCmd)
}

func runPRCreate(cmd *cobra.Command, args []string) {
	fmt.Println("[PRBuddy-Go] Starting PR creation workflow...")

	// 1. Get current branch and commit
	branchName, err := utils.ExecGit("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branchName == "HEAD" || branchName == "" {
		fmt.Printf("[PRBuddy-Go] Error: %v\n", err)
		fmt.Println("[PRBuddy-Go] Failed to determine current branch. Are you in detached HEAD state?")
		return
	}
	branchName = strings.TrimSpace(branchName)

	commitHash, err := utils.ExecGit("rev-parse", "HEAD")
	if err != nil {
		fmt.Printf("[PRBuddy-Go] Error: %v\n", err)
		fmt.Println("[PRBuddy-Go] Failed to determine current commit hash.")
		return
	}
	commitHash = strings.TrimSpace(commitHash)

	// 2. Ensure branch is pushed to remote
	if err := pushBranch(branchName); err != nil {
		fmt.Printf("[PRBuddy-Go] Error pushing branch: %v\n", err)
		fmt.Println("[PRBuddy-Go] PR creation requires your branch to be pushed to the remote.")
		return
	}

	// 3. Find saved draft
	draftPath, err := findDraftArtifacts(branchName, commitHash)
	if err != nil {
		fmt.Printf("[PRBuddy-Go] Error: %v\n", err)
		fmt.Println("[PRBuddy-Go] No draft found for current commit. Run 'prbuddy-go post-commit' first.")
		return
	}

	// 4. Create PR using saved draft
	if err := createPRFromDraft(branchName, draftPath); err != nil {
		fmt.Printf("[PRBuddy-Go] PR creation failed: %v\n", err)
		fmt.Println("[PRBuddy-Go] Tip: check `gh auth status` and ensure your repo remote points to GitHub.")
		return
	}

	fmt.Println("[PRBuddy-Go] PR creation workflow completed successfully!")
}

func pushBranch(branchName string) error {
	fmt.Printf("[PRBuddy-Go] Ensuring branch '%s' is pushed to remote...\n", branchName)

	// Check if branch has an upstream
	upstream, err := utils.ExecGit("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err == nil {
		// Branch has upstream - check if ahead
		upstream = strings.TrimSpace(upstream)
		aheadStr, err := utils.ExecGit("rev-list", "--count", upstream+"..HEAD")
		if err == nil && strings.TrimSpace(aheadStr) != "0" {
			fmt.Printf("[PRBuddy-Go] Pushing %s commits to %s...\n", aheadStr, upstream)
			if _, err := utils.ExecGit("push", "origin", branchName); err != nil {
				return fmt.Errorf("failed to push: %w", err)
			}
		}
		return nil
	}

	// Branch has no upstream - set up tracking and push
	fmt.Printf("[PRBuddy-Go] Setting up tracking for new branch '%s'...\n", branchName)
	if _, err := utils.ExecGit("push", "-u", "origin", branchName); err != nil {
		return fmt.Errorf("failed to push with tracking: %w", err)
	}

	return nil
}

func findDraftArtifacts(branch, commit string) (string, error) {
	repoPath, err := utils.GetRepoPath()
	if err != nil {
		return "", fmt.Errorf("repo path detection: %w", err)
	}

	logDir := filepath.Join(
		repoPath,
		".git", "pr_buddy_db",
		utils.SanitizeBranchName(branch),
		fmt.Sprintf("commit-%s", commit[:7]),
	)

	draftPath := filepath.Join(logDir, "draft.md")
	if _, err := os.Stat(draftPath); os.IsNotExist(err) {
		return "", fmt.Errorf("draft not found at %s", draftPath)
	}

	return draftPath, nil
}

func createPRFromDraft(branch, draftPath string) error {
	// Extract title from draft
	title, err := extractPRTitle(draftPath)
	if err != nil {
		return fmt.Errorf("title extraction: %w", err)
	}

	// Detect base branch if not specified
	targetBase := baseBranch
	if targetBase == "" {
		base, err := detectBaseBranch()
		if err != nil {
			fmt.Printf("[PRBuddy-Go] Warning: %v\n", err)
			fmt.Println("[PRBuddy-Go] Using 'main' as default base branch")
			targetBase = "main"
		} else {
			targetBase = base
		}
	}

	fmt.Printf("[PRBuddy-Go] Creating PR from %s to %s...\n", branch, targetBase)

	// Build gh pr create command
	args := []string{"pr", "create", "--title", title, "--body-file", draftPath, "--head", branch, "--base", targetBase}

	if assignees != "" {
		args = append(args, "--assignees", assignees)
	}
	if reviewers != "" {
		args = append(args, "--reviewers", reviewers)
	}
	if labels != "" {
		args = append(args, "--labels", labels)
	}

	// Execute with timeout and sanitized environment
	out, err := runGH(30*time.Second, args...)
	if err != nil {
		return fmt.Errorf("gh command failed: %w", err)
	}

	// Extract and print PR URL
	url := extractPRURL(out)
	if url != "" {
		fmt.Printf("[PRBuddy-Go] PR created: %s\n", url)
	} else {
		fmt.Println("[PRBuddy-Go] PR created (no URL returned)")
	}

	return nil
}

func extractPRTitle(draftPath string) (string, error) {
	content, err := os.ReadFile(draftPath)
	if err != nil {
		return "", err
	}

	// Simple title extraction - first H1 or H2 line
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
			title := strings.TrimPrefix(line, "# ")
			title = strings.TrimPrefix(title, "## ")
			return strings.TrimSpace(title), nil
		}
	}

	// Fallback to commit subject
	commitMsg, err := utils.ExecGit("log", "-1", "--pretty=%s", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commitMsg), nil
}

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

	return "", fmt.Errorf("could not detect base branch")
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

func extractPRURL(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "https://") {
			return line
		}
	}
	return ""
}

// runGH executes gh with a timeout and with a sanitized environment
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
		// These can override gh's stored auth and cause mysterious 401s
		if strings.HasPrefix(kv, "GITHUB_TOKEN=") || strings.HasPrefix(kv, "GH_TOKEN=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
