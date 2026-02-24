// test/dce/task_helper/build_task_list_test.go
package task_helper

import (
	"strings"
	"testing"

	"github.com/soyuz43/prbuddy-go/internal/dce"
	"github.com/soyuz43/prbuddy-go/test"
)

func TestBuildTaskListWithMatchingFiles(t *testing.T) {
	// Setup test repository
	repoPath := test.SetupTestRepository(t)
	defer test.CleanupTestRepository(t, repoPath)

	// Build task list for a description that should match files
	tasks, _, logs, err := dce.BuildTaskList("context package")
	if err != nil {
		t.Fatalf("BuildTaskList failed: %v", err)
	}

	// Verify logs - update to expect 2 files
	foundFilesLog := false
	for _, log := range logs {
		if strings.Contains(log, "Matched 2 files: [cmd/context.go internal/contextpkg/context.go]") {
			foundFilesLog = true
			break
		}
	}
	if !foundFilesLog {
		t.Error("Expected log about matched files not found")
	}

	// Verify tasks
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(tasks))
	}

	task := tasks[0]
	if task.Description != "context package" {
		t.Errorf("Expected task description 'context package', got '%s'", task.Description)
	}

	// Check files - verify both expected files are present
	expectedFiles := []string{"cmd/context.go", "internal/contextpkg/context.go"}
	if len(task.Files) != len(expectedFiles) {
		t.Errorf("Expected %d files, got %d", len(expectedFiles), len(task.Files))
	}

	for _, expectedFile := range expectedFiles {
		found := false
		for _, file := range task.Files {
			if file == expectedFile {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected file '%s' not found in task.Files", expectedFile)
		}
	}

	if len(task.Functions) == 0 {
		t.Error("Expected at least one function to be extracted")
	}
}

func TestBuildTaskListWithNoMatchingFiles(t *testing.T) {
	// Setup test repository
	repoPath := test.SetupTestRepository(t)
	defer test.CleanupTestRepository(t, repoPath)

	// Build task list for a description that shouldn't match files
	tasks, _, logs, err := dce.BuildTaskList("nonexistent feature")
	if err != nil {
		t.Fatalf("BuildTaskList failed: %v", err)
	}

	// Verify logs
	foundCatchAllLog := false
	for _, log := range logs {
		if strings.Contains(log, "No file matches found - created catch-all task") {
			foundCatchAllLog = true
			break
		}
	}
	if !foundCatchAllLog {
		t.Error("Expected log about catch-all task not found")
	}

	// Verify tasks
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(tasks))
	}

	task := tasks[0]
	if task.Description != "nonexistent feature" {
		t.Errorf("Expected task description 'nonexistent feature', got '%s'", task.Description)
	}

	if len(task.Files) != 0 {
		t.Errorf("Expected no files, got %v", task.Files)
	}

	if len(task.Notes) == 0 || !strings.Contains(task.Notes[0], "No direct file matches found") {
		t.Error("Expected 'no file matches' note not found in task")
	}
}

func TestBuildTaskListWithFunctionExtraction(t *testing.T) {
	// Setup test repository
	repoPath := test.SetupTestRepository(t)
	defer test.CleanupTestRepository(t, repoPath)

	// Build task list for a description that should match a file with functions
	tasks, _, _, err := dce.BuildTaskList("cmd context")
	if err != nil {
		t.Fatalf("BuildTaskList failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(tasks))
	}

	task := tasks[0]
	if len(task.Functions) == 0 {
		t.Error("Expected at least one function to be extracted")
	} else {
		// Verify init function was extracted
		foundInit := false
		for _, fn := range task.Functions {
			if fn == "init" {
				foundInit = true
				break
			}
		}
		if !foundInit {
			t.Error("Expected 'init' function to be extracted, but it wasn't found")
		}

		// Verify ExampleFunction was extracted
		foundExample := false
		for _, fn := range task.Functions {
			if fn == "ExampleFunction" {
				foundExample = true
				break
			}
		}
		if !foundExample {
			t.Error("Expected 'ExampleFunction' to be extracted, but it wasn't found")
		}
	}
}
