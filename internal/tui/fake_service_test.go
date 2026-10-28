package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/ibrahimraimi/envil/internal/service"
)

type fakeService struct {
	hasKeys            bool
	hasKeysErr         error
	fingerprint        string
	phrase             string
	phraseErr          error
	completeSetupCalls []string
	completeSetupErr   error
	recoverCalls       []string
	recoverErr         error
	projects           []service.ProjectSummary
	projectsErr        error
	history            []service.VersionInfo
	historyErr         error
	backupCalls        []string
	backupVersion      int
	backupErr          error
	diffs              []service.DiffEntry
	diffErr            error
	revealedLocal      string
	revealedRemote     string
	revealErr          error
	restoreCalls       []int
	restoreAcceptCalls int
	restoreErr         error
	resetStateCalls    []string
	deleteProjectCalls []string
	deleteAccountCalls int
	doctorReport       service.DoctorReport
	doctorErr          error
}

func (f *fakeService) HasKeys(ctx context.Context) (bool, error) {
	return f.hasKeys, f.hasKeysErr
}

func (f *fakeService) AccountFingerprint(ctx context.Context) (string, error) {
	if f.fingerprint != "" {
		return f.fingerprint, nil
	}
	return "abcd1234ef56", nil
}

func (f *fakeService) NewPhrase(ctx context.Context) (string, error) {
	if f.phraseErr != nil {
		return "", f.phraseErr
	}
	if f.phrase != "" {
		return f.phrase, nil
	}
	return "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art", nil
}

func (f *fakeService) CompleteSetup(ctx context.Context, phrase string) error {
	f.completeSetupCalls = append(f.completeSetupCalls, phrase)
	return f.completeSetupErr
}

func (f *fakeService) Recover(ctx context.Context, phrase string) error {
	f.recoverCalls = append(f.recoverCalls, phrase)
	return f.recoverErr
}

func (f *fakeService) ListProjects(ctx context.Context) ([]service.ProjectSummary, error) {
	return f.projects, f.projectsErr
}

func (f *fakeService) History(ctx context.Context, projectID string) ([]service.VersionInfo, error) {
	return f.history, f.historyErr
}

func (f *fakeService) Backup(ctx context.Context, projectID string, files []string) (int, error) {
	f.backupCalls = append(f.backupCalls, projectID)
	return f.backupVersion, f.backupErr
}

func (f *fakeService) Diff(ctx context.Context, projectID string, version int) ([]service.DiffEntry, error) {
	return f.diffs, f.diffErr
}

func (f *fakeService) RevealDiffValue(ctx context.Context, projectID string, version int, filename, key string) (string, string, error) {
	if f.revealErr != nil {
		return "", "", f.revealErr
	}
	return f.revealedLocal, f.revealedRemote, nil
}

func (f *fakeService) Restore(ctx context.Context, projectID string, version int, force bool) error {
	f.restoreCalls = append(f.restoreCalls, version)
	if service.ShouldAcceptServerState(ctx) {
		f.restoreAcceptCalls++
		return nil
	}
	return f.restoreErr
}

func (f *fakeService) ResetState(ctx context.Context, projectID string) error {
	f.resetStateCalls = append(f.resetStateCalls, projectID)
	return nil
}

func (f *fakeService) DeleteProject(ctx context.Context, projectID string) error {
	f.deleteProjectCalls = append(f.deleteProjectCalls, projectID)
	return nil
}

func (f *fakeService) DeleteAccount(ctx context.Context) error {
	f.deleteAccountCalls++
	return nil
}

func (f *fakeService) Doctor(ctx context.Context) (service.DoctorReport, error) {
	return f.doctorReport, f.doctorErr
}

func (f *fakeService) LinkCurrentDirectory(ctx context.Context, dir, name string) (string, error) {
	return "00000000-0000-0000-0000-000000000001", nil
}

func (f *fakeService) LoadProjectConfig(dir string) (string, error) {
	return "00000000-0000-0000-0000-000000000001", nil
}

var _ service.Service = (*fakeService)(nil)

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		r := rune(0)
		if len(s) == 1 {
			r = rune(s[0])
		}
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}
