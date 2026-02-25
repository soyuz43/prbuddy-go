// test/dce/command_menu/help_test.go
package command_menu_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/soyuz43/prbuddy-go/internal/dce"
	"github.com/soyuz43/prbuddy-go/test"
)

func TestHelpCommandDisplaysCorrectly(t *testing.T) {
	// Setup
	_, littleguy := test.SetupDCEForTesting(t, "Test task")

	// Capture output
	mockOutput := &MockOutputWriter{Buffer: &bytes.Buffer{}}
	SetOutputForTests(mockOutput)

	// Test /help
	dce.HandleDCECommandMenu("/help", littleguy)
	output := mockOutput.String()

	// Verify command menu is displayed
	if !strings.Contains(output, "Available DCE Commands") {
		t.Error("Help output doesn't contain command menu header")
	}

	// Verify add command (semantic, not exact phrasing)
	// We accept either "/add <...>" or the new combined alias line.
	if !strings.Contains(output, "/add") {
		t.Error("Help output doesn't include /add")
	}
	if !strings.Contains(output, "/a") {
		t.Error("Help output doesn't include /a alias")
	}

	// Verify help/commands aliases exist (order-independent)
	for _, tok := range []string{"/help", "/cmds", "/commands"} {
		if !strings.Contains(output, tok) {
			t.Errorf("Help output doesn't include %s", tok)
		}
	}
}

func TestAllHelpCommandAliasesWork(t *testing.T) {
	// Setup
	_, littleguy := test.SetupDCEForTesting(t, "Test task")

	// Test all help command variants
	commands := []string{"/help", "/commands", "/cmds", "/c"}

	for _, cmd := range commands {
		t.Run(fmt.Sprintf("Command_%s", cmd), func(t *testing.T) {
			mockOutput := &MockOutputWriter{Buffer: &bytes.Buffer{}}
			SetOutputForTests(mockOutput)

			handled := dce.HandleDCECommandMenu(cmd, littleguy)
			if !handled {
				t.Fatalf("Expected %q to be handled, but it was not", cmd)
			}

			output := mockOutput.String()

			if !strings.Contains(output, "Available DCE Commands") {
				t.Errorf("Output for '%s' doesn't contain command menu", cmd)
			}

			if !strings.Contains(output, "/add") {
				t.Errorf("Output for '%s' doesn't include /add", cmd)
			}
		})
	}
}
