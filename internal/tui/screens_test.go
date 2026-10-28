package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	"github.com/ibrahimraimi/envil/internal/service"
)

func TestProjectDetailScreenNavigation(t *testing.T) {
	svc := &fakeService{
		history: []service.VersionInfo{
			{Number: 1, SizeBytes: 150, CreatedAt: time.Now()},
		},
	}
	projScreen := NewProjectDetailScreen(svc, "proj-123", "test-project")
	projScreen.Update(HistoryLoadedMsg{Versions: svc.history})

	// Press 'esc' to return to dashboard
	_, cmd := projScreen.Update(keyMsg("esc"))
	if cmd == nil {
		t.Fatalf("expected navigate cmd on esc, got nil")
	}
	nav := cmd().(NavigateMsg)
	if _, ok := nav.To.(*DashboardScreen); !ok {
		t.Fatalf("expected NavigateMsg to DashboardScreen, got %T", nav.To)
	}

	// Press 's' to restore
	_, cmd = projScreen.Update(keyMsg("s"))
	if cmd == nil {
		t.Fatalf("expected restore cmd on 's', got nil")
	}
	nav = cmd().(NavigateMsg)
	if _, ok := nav.To.(*RestoreScreen); !ok {
		t.Fatalf("expected NavigateMsg to RestoreScreen, got %T", nav.To)
	}
}

func TestBackupScreenConflictAndSuccess(t *testing.T) {
	svc := &fakeService{}
	backupScreen := NewBackupScreen(svc, "proj-123")

	// 1. Conflict message handling
	backupScreen.Update(BackupDoneMsg{Err: errors.New("backup conflict: remote version has changed")})
	conflictView := backupScreen.View(80, 24)
	if !strings.Contains(conflictView, "CONFLICT") {
		t.Fatalf("expected conflict banner, got: %s", conflictView)
	}

	// Press 'd' opens restore screen
	_, cmd := backupScreen.Update(keyMsg("d"))
	if cmd == nil {
		t.Fatalf("expected navigate cmd on 'd', got nil")
	}
	nav := cmd().(NavigateMsg)
	if _, ok := nav.To.(*RestoreScreen); !ok {
		t.Fatalf("expected NavigateMsg to RestoreScreen, got %T", nav.To)
	}

	// 2. Success message handling
	backupScreenSuccess := NewBackupScreen(svc, "proj-123")
	backupScreenSuccess.Update(BackupDoneMsg{Version: 3})
	successView := backupScreenSuccess.View(80, 24)
	if !strings.Contains(successView, "Backup completed successfully") || !strings.Contains(successView, "v3") {
		t.Fatalf("expected success message with version v3, got: %s", successView)
	}
}

func TestRestoreSafetyNoRestoreBeforeY(t *testing.T) {
	svc := &fakeService{
		diffs: []service.DiffEntry{
			{Filename: ".env", Key: "SECRET_KEY", Status: "MODIFIED"},
		},
	}
	restoreScreen := NewRestoreScreen(svc, "proj-123", 1)
	restoreScreen.Update(DiffLoadedMsg{Diffs: svc.diffs})

	// Rule: Assert no call to svc.Restore before 'y' is pressed!
	if len(svc.restoreCalls) != 0 {
		t.Fatalf("expected 0 restore calls before confirmation, got %d", len(svc.restoreCalls))
	}

	// Press 'n' to cancel
	_, cmd := restoreScreen.Update(keyMsg("n"))
	if len(svc.restoreCalls) != 0 {
		t.Fatalf("expected 0 restore calls after 'n' cancel, got %d", len(svc.restoreCalls))
	}
	if cmd == nil {
		t.Fatalf("expected navigation to dashboard on cancel")
	}

	// Now test 'y' confirmation
	restoreScreen2 := NewRestoreScreen(svc, "proj-123", 1)
	restoreScreen2.Update(DiffLoadedMsg{Diffs: svc.diffs})
	restoreScreen2.Update(keyMsg("y"))

	if !restoreScreen2.isRestoring {
		t.Fatalf("expected isRestoring to be true after 'y'")
	}
}

func TestRestoreScreenSecretMaskingAndReveal(t *testing.T) {
	svc := &fakeService{
		diffs: []service.DiffEntry{
			{Filename: ".env", Key: "DATABASE_PASSWORD", Status: "MODIFIED"},
			{Filename: ".env", Key: "ANOTHER_KEY", Status: "ADDED"},
		},
		revealedLocal:  "local_super_secret",
		revealedRemote: "remote_super_secret",
	}
	restoreScreen := NewRestoreScreen(svc, "proj-123", 1)
	restoreScreen.Update(DiffLoadedMsg{Diffs: svc.diffs})

	// 1. Security Check: Model must contain NO plaintext secret values by default!
	if restoreScreen.revealedKey != "" || restoreScreen.revealedLocal != "" || restoreScreen.revealedRemote != "" {
		t.Fatalf("expected no revealed secrets in model by default")
	}

	// 2. View check: default view must render masked "********"
	maskedView := restoreScreen.View(80, 24)
	if !strings.Contains(maskedView, "DATABASE_PASSWORD") {
		t.Fatalf("expected key name in diff view")
	}
	if !strings.Contains(maskedView, "********") {
		t.Fatalf("expected masked '********' in diff view, got: %s", maskedView)
	}
	if strings.Contains(maskedView, "local_super_secret") || strings.Contains(maskedView, "remote_super_secret") {
		t.Fatalf("leak: plaintext secrets found in default diff view!")
	}

	// 3. Reveal toggle: press 'v'
	_, revealCmd := restoreScreen.Update(keyMsg("v"))
	if revealCmd == nil {
		t.Fatalf("expected revealCmd on 'v'")
	}
	revealMsg := revealCmd()
	restoreScreen.Update(revealMsg)

	if restoreScreen.revealedKey != "DATABASE_PASSWORD" {
		t.Fatalf("expected revealedKey to be set to DATABASE_PASSWORD")
	}

	revealedView := restoreScreen.View(80, 24)
	if !strings.Contains(revealedView, "local_super_secret") {
		t.Fatalf("expected revealed local value in view after 'v', got: %s", revealedView)
	}

	// 4. Moving cursor or exiting wipes revealed secrets
	restoreScreen.Update(keyMsg("down"))
	if restoreScreen.revealedKey != "" || restoreScreen.revealedLocal != "" {
		t.Fatalf("expected revealed secrets to be wiped when cursor moved")
	}
}

func TestSetupWizardVerificationAndMemoryWipe(t *testing.T) {
	phrase := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
	svc := &fakeService{phrase: phrase}

	setupScreen := NewSetupScreen(svc)
	if len(setupScreen.words) != 24 {
		t.Fatalf("expected 24 words in setupScreen, got %d", len(setupScreen.words))
	}

	// Press enter to move to verification
	setupScreen.Update(keyMsg("enter"))
	if setupScreen.step != setupStepVerify {
		t.Fatalf("expected step to be setupStepVerify")
	}

	// Enter incorrect words
	for i := range setupScreen.inputs {
		setupScreen.inputs[i].SetValue("wrongword")
	}
	setupScreen.focusIndex = len(setupScreen.inputs) - 1
	setupScreen.Update(keyMsg("enter"))

	if len(svc.completeSetupCalls) != 0 {
		t.Fatalf("expected CompleteSetup NOT to be called when verification fails")
	}
	if setupScreen.err == nil {
		t.Fatalf("expected error on incorrect word verification")
	}

	// Now enter correct words
	for i, idx := range setupScreen.verifyIndices {
		setupScreen.inputs[i].SetValue(setupScreen.words[idx])
	}
	setupScreen.Update(keyMsg("enter"))

	if len(svc.completeSetupCalls) != 1 {
		t.Fatalf("expected CompleteSetup to be called once, got %d", len(svc.completeSetupCalls))
	}

	// CRITICAL SECURITY RULE: Model must no longer hold the phrase!
	if setupScreen.phrase != "" || setupScreen.words != nil {
		t.Fatalf("SECURITY VIOLATION: setupScreen must wipe phrase from memory upon completion! phrase=%q, words=%v",
			setupScreen.phrase, setupScreen.words)
	}
}

func TestRecoveryWizardMaskingAndBIP39Validation(t *testing.T) {
	svc := &fakeService{}
	recScreen := NewRecoverScreen(svc)

	// 1. Password masking: EchoMode must be EchoPassword
	if recScreen.input.EchoMode != textinput.EchoPassword {
		t.Fatalf("expected EchoMode to be EchoPassword, got %v", recScreen.input.EchoMode)
	}

	// 2. Invalid phrase rejection
	recScreen.input.SetValue("word1 word2 word3")
	recScreen.Update(keyMsg("enter"))
	if len(svc.recoverCalls) != 0 {
		t.Fatalf("expected Recover NOT to be called with invalid phrase")
	}
	if recScreen.err == nil {
		t.Fatalf("expected checksum error on invalid word count")
	}

	// 3. Valid phrase with extra spaces and uppercase
	validPhrase := "  ABANDON abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art  "
	recScreen.input.SetValue(validPhrase)
	recScreen.Update(keyMsg("enter"))

	if len(svc.recoverCalls) != 1 {
		t.Fatalf("expected Recover to be called once with normalized phrase, got %d", len(svc.recoverCalls))
	}

	// Model input should be reset
	if recScreen.input.Value() != "" {
		t.Fatalf("expected phrase input to be cleared after recovery, got %q", recScreen.input.Value())
	}
}

func TestNoV1CharmbraceletImports(t *testing.T) {
	// Guard rule: No files in internal/ or cmd/ may import github.com/charmbracelet/bubble* or lipgloss
	err := filepath.Walk("../..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(contentBytes)
		forbiddenV1 := []string{
			"github.com/" + "charmbracelet/bubbletea",
			"github.com/" + "charmbracelet/bubbles",
			"github.com/" + "charmbracelet/lipgloss",
		}
		for _, forbidden := range forbiddenV1 {
			if strings.Contains(content, forbidden) {
				t.Errorf("file %s imports forbidden v1 charmbracelet package!", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk failed: %v", err)
	}
}
