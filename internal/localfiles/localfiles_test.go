package localfiles

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBackupCopyNamingAndSameSecondCollision(t *testing.T) {
	tempDir := t.TempDir()
	fixedTime := time.Date(2026, 10, 28, 10, 15, 0, 0, time.UTC)

	name1, err := CreateBackupCopy(tempDir, ".env", []byte("A=1\n"), fixedTime)
	if err != nil {
		t.Fatalf("CreateBackupCopy 1: %v", err)
	}
	if name1 != ".env.bak.20261028T101500" {
		t.Fatalf("expected .env.bak.20261028T101500, got %s", name1)
	}

	// Second restore in the exact same second adds a counter suffix
	name2, err := CreateBackupCopy(tempDir, ".env", []byte("A=2\n"), fixedTime)
	if err != nil {
		t.Fatalf("CreateBackupCopy 2: %v", err)
	}
	if name2 != ".env.bak.20261028T101500.1" {
		t.Fatalf("expected .env.bak.20261028T101500.1, got %s", name2)
	}

	// Verify neither overwrote the other
	b1, err := os.ReadFile(filepath.Join(tempDir, name1))
	if err != nil || string(b1) != "A=1\n" {
		t.Fatalf("copy 1 content mismatch: %q (err=%v)", string(b1), err)
	}
	b2, err := os.ReadFile(filepath.Join(tempDir, name2))
	if err != nil || string(b2) != "A=2\n" {
		t.Fatalf("copy 2 content mismatch: %q (err=%v)", string(b2), err)
	}

	// Verify mode 0600 on Unix
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(tempDir, name1))
		if err != nil {
			t.Fatalf("stat copy 1: %v", err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("expected mode 0600, got %o", info.Mode().Perm())
		}
	}
}

func TestPruneBackupCopiesKeepsNewestAndIgnoresUnrelated(t *testing.T) {
	tempDir := t.TempDir()
	baseTime := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)

	// Unrelated files that must never be touched
	unrelatedFiles := []string{
		".env.backup.txt",
		".env.bak",
		".env.bak.custom",
		".env.local.bak.20261028T100000",
	}
	for _, name := range unrelatedFiles {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte("keep-me"), 0600); err != nil {
			t.Fatalf("write unrelated file %s: %v", name, err)
		}
	}

	var created []string
	for i := 0; i < 5; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Minute)
		name, err := CreateBackupCopy(tempDir, ".env", []byte("V"), ts)
		if err != nil {
			t.Fatalf("CreateBackupCopy %d: %v", i, err)
		}
		created = append(created, name)
	}

	// Prune to newest 2
	if err := PruneBackupCopies(tempDir, ".env", 2); err != nil {
		t.Fatalf("PruneBackupCopies: %v", err)
	}

	remaining, err := ListBackupCopiesForFile(tempDir, ".env")
	if err != nil {
		t.Fatalf("ListBackupCopiesForFile: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining copies, got %d", len(remaining))
	}
	if remaining[0].Name != created[4] || remaining[1].Name != created[3] {
		t.Fatalf("expected newest copies [%s, %s], got [%s, %s]",
			created[4], created[3], remaining[0].Name, remaining[1].Name)
	}

	// Verify unrelated files are untouched
	for _, name := range unrelatedFiles {
		if _, err := os.Stat(filepath.Join(tempDir, name)); err != nil {
			t.Errorf("unrelated file %s was unexpectedly removed: %v", name, err)
		}
	}
}

func initTestGitRepo(t *testing.T, dir string) {
	t.Helper()
	if !IsGitAvailable() {
		t.Skip("git binary not installed")
	}
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, string(out))
		}
	}
	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")
}

func TestGitCheckScenarios(t *testing.T) {
	ctx := context.Background()

	// 1. Directory without Git returns skipped, no error
	nonGitDir := t.TempDir()
	resNonGit := CheckGitStatus(ctx, nonGitDir, ".env")
	if resNonGit.Status != GitSkipped {
		t.Fatalf("expected GitSkipped outside git repo, got %v", resNonGit.Status)
	}

	// 2. Missing git binary (simulate with empty PATH) returns skipped
	t.Run("missing git binary", func(t *testing.T) {
		t.Setenv("PATH", "")
		resNoGit := CheckGitStatus(ctx, nonGitDir, ".env")
		if resNoGit.Status != GitSkipped || !resNoGit.GitMissing {
			t.Fatalf("expected GitSkipped with GitMissing=true when PATH is empty, got %+v", resNoGit)
		}
	})

	// 3. Git repository checks: ignored, non-ignored, tracked, spaces and leading dashes
	repoDir := t.TempDir()
	initTestGitRepo(t, repoDir)

	// Non-ignored file
	if err := os.WriteFile(filepath.Join(repoDir, ".env"), []byte("SECRET=1\n"), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	resNotIgnored := CheckGitStatus(ctx, repoDir, ".env")
	if resNotIgnored.Status != GitNotIgnored {
		t.Fatalf("expected GitNotIgnored for .env, got %v", resNotIgnored.Status)
	}

	// Ignored file
	if err := AppendGitIgnorePattern(repoDir, ".env"); err != nil {
		t.Fatalf("AppendGitIgnorePattern: %v", err)
	}
	resIgnored := CheckGitStatus(ctx, repoDir, ".env")
	if resIgnored.Status != GitIgnored {
		t.Fatalf("expected GitIgnored after adding .env to .gitignore, got %v", resIgnored.Status)
	}

	// Tracked file
	trackedFile := ".env.tracked"
	if err := os.WriteFile(filepath.Join(repoDir, trackedFile), []byte("TRACKED=1\n"), 0600); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	addCmd := exec.Command("git", "add", "--", trackedFile)
	addCmd.Dir = repoDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v (%s)", err, string(out))
	}
	resTracked := CheckGitStatus(ctx, repoDir, trackedFile)
	if resTracked.Status != GitTracked {
		t.Fatalf("expected GitTracked for git-added file, got %v", resTracked.Status)
	}

	// File names containing spaces or leading dashes (-- separator check)
	weirdFiles := []string{
		".env with spaces",
		"-.env-leading-dash",
	}
	for _, wf := range weirdFiles {
		if err := os.WriteFile(filepath.Join(repoDir, wf), []byte("X=1\n"), 0600); err != nil {
			t.Fatalf("write %s: %v", wf, err)
		}
		resWeird := CheckGitStatus(ctx, repoDir, wf)
		if resWeird.Status != GitNotIgnored {
			t.Fatalf("expected GitNotIgnored for %q, got %v", wf, resWeird.Status)
		}
	}
}

