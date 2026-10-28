package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ibrahimraimi/envil/internal/errs"
	"github.com/ibrahimraimi/envil/internal/service"
)

type BackupScreen struct {
	svc        service.Service
	projectID  string
	spinner    spinner.Model
	isDone     bool
	newVersion int
	isConflict bool
	err        error
	width      int
	height     int
}

func NewBackupScreen(svc service.Service, projectID string) *BackupScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = lipgloss.NewStyle().Foreground(ColorPrimary)

	return &BackupScreen{
		svc:       svc,
		projectID: projectID,
		spinner:   sp,
	}
}

func (s *BackupScreen) Init() tea.Cmd {
	return tea.Batch(
		s.spinner.Tick,
		runBackupCmd(s.svc, s.projectID),
	)
}

func (s *BackupScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return s, tea.Quit
		case "esc", "q":
			if s.isDone || s.err != nil {
				return s, func() tea.Msg {
					return NavigateMsg{To: NewDashboardScreen(s.svc)}
				}
			}
		case "d":
			if s.isConflict {
				return s, func() tea.Msg {
					return NavigateMsg{To: NewRestoreScreen(s.svc, s.projectID, 0)}
				}
			}
		}

	case BackupDoneMsg:
		s.isDone = true
		if msg.Err != nil {
			s.err = msg.Err
			if errs.IsCode(msg.Err, errs.Conflict) || strings.Contains(strings.ToLower(msg.Err.Error()), "conflict") {
				s.isConflict = true
			}
			return s, nil
		}
		s.newVersion = msg.Version
		return s, nil

	case spinner.TickMsg:
		if !s.isDone && s.err == nil {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(msg)
			return s, cmd
		}
	}

	return s, nil
}

func (s *BackupScreen) View(width, height int) string {
	s.width, s.height = width, height

	header := TitleStyle.Render("BACKUP ENVIRONMENT FILES")

	var body string
	switch {
	case !s.isDone && s.err == nil:
		body = fmt.Sprintf("\n  %s Reading local .env files, encrypting, and uploading to Envil server...\n", s.spinner.View())

	case s.isConflict:
		body = fmt.Sprintf("\n  %s\n\n  %s\n  Press 'd' to view diff, or 'esc' to return to dashboard.",
			WarningBoxStyle.Render("CONFLICT: Remote backup version has changed"),
			errs.Format(errs.New(errs.Conflict, nil)))

	case s.err != nil:
		body = fmt.Sprintf("\n  %s\n\n  Press 'esc' to return to dashboard.",
			ErrorStyle.Render(fmt.Sprintf("Backup failed: %s", errs.Format(s.err))))

	default:
		body = fmt.Sprintf("\n  %s Backup completed successfully!\n\n  Created version: %s\n  Encrypted with:  XChaCha20-Poly1305 (client-side)\n\n  Press 'esc' to return to dashboard.",
			SuccessStyle.Render("✓"),
			FingerprintStyle.Render(fmt.Sprintf("v%d", s.newVersion)),
		)
	}

	return header + "\n" + body
}

func runBackupCmd(svc service.Service, projectID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		version, err := svc.Backup(ctx, projectID, nil)
		return BackupDoneMsg{Version: version, Err: err}
	}
}
