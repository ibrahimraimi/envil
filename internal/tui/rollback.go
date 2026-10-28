package tui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/ibrahimraimi/envil/internal/errs"
	"github.com/ibrahimraimi/envil/internal/service"
)

type RollbackScreen struct {
	svc         service.Service
	projectID   string
	version     int
	rollbackErr error
	spinner     spinner.Model
	isAccepting bool
	isDone      bool
	err         error
	width       int
	height      int
}

func NewRollbackScreen(svc service.Service, projectID string, version int, rollbackErr error) *RollbackScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = TitleStyle

	return &RollbackScreen{
		svc:         svc,
		projectID:   projectID,
		version:     version,
		rollbackErr: rollbackErr,
		spinner:     sp,
	}
}

func (s *RollbackScreen) Init() tea.Cmd {
	return nil
}

func (s *RollbackScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return s, tea.Quit
		case "n", "esc", "q":
			if !s.isAccepting {
				return s, func() tea.Msg {
					return NavigateMsg{To: NewDashboardScreen(s.svc)}
				}
			}
		case "y":
			if !s.isAccepting && !s.isDone {
				s.isAccepting = true
				return s, tea.Batch(
					s.spinner.Tick,
					acceptAndRestoreCmd(s.svc, s.projectID, s.version),
				)
			}
		}

	case RestoreDoneMsg:
		s.isAccepting = false
		s.isDone = true
		if msg.Err != nil {
			s.err = msg.Err
		}
		return s, nil

	case spinner.TickMsg:
		if s.isAccepting {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(msg)
			return s, cmd
		}
	}

	return s, nil
}

func (s *RollbackScreen) View(width, height int) string {
	s.width, s.height = width, height

	header := TitleStyle.Render("INTEGRITY WARNING: ROLLBACK OR FORK DETECTED")

	if s.isAccepting {
		return fmt.Sprintf("%s\n\n  %s Accepting server state and restoring...", header, s.spinner.View())
	}

	if s.isDone {
		if s.err != nil {
			return fmt.Sprintf("%s\n\n  %s\n\n  Press 'esc' to return to dashboard.",
				header,
				ErrorStyle.Render(fmt.Sprintf("Error: %s", errs.Format(s.err))),
			)
		}
		return fmt.Sprintf("%s\n\n  %s Server state accepted and files restored.\n\n  Press 'esc' to return to dashboard.",
			header,
			SuccessStyle.Render("✓"),
		)
	}

	projLabel := s.projectID
	if projLabel == "" {
		projLabel = "(current project)"
	}

	warningContent := fmt.Sprintf("Project: %s\n\n%s", projLabel, errs.Format(s.rollbackErr))
	box := WarningBoxStyle.Render(warningContent)

	choices := "\n\n  Choices:\n    [n / esc] Cancel and keep local files unchanged (recommended)\n    [y]       Accept server state for this project and proceed\n"

	return header + "\n\n" + box + choices
}

func acceptAndRestoreCmd(svc service.Service, projectID string, version int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		acceptCtx := service.WithAcceptServerState(ctx, true)
		err := svc.Restore(acceptCtx, projectID, version, true)
		return RestoreDoneMsg{Err: err}
	}
}

