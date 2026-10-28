package tui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ibrahimraimi/envil/internal/errs"
	"github.com/ibrahimraimi/envil/internal/service"
)

type DoctorScreen struct {
	svc       service.Service
	table     table.Model
	spinner   spinner.Model
	checks    []service.Check
	isLoading bool
	err       error
	width     int
	height    int
}

func NewDoctorScreen(svc service.Service) *DoctorScreen {
	cols := []table.Column{
		{Title: "STATUS", Width: 8},
		{Title: "CHECK", Width: 20},
		{Title: "DETAIL", Width: 44},
	}

	tbl := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(11),
	)

	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorSecondary).
		BorderBottom(true).
		Bold(true)
	styles.Selected = styles.Selected.
		Foreground(ColorPrimary).
		Bold(true)
	tbl.SetStyles(styles)

	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = lipgloss.NewStyle().Foreground(ColorPrimary)

	return &DoctorScreen{
		svc:       svc,
		table:     tbl,
		spinner:   sp,
		isLoading: true,
	}
}

func (s *DoctorScreen) Init() tea.Cmd {
	return tea.Batch(
		s.spinner.Tick,
		loadDoctorCmd(s.svc),
	)
}

func (s *DoctorScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return s, tea.Quit
		case "esc":
			return s, func() tea.Msg {
				return NavigateMsg{To: NewDashboardScreen(s.svc)}
			}
		case "r":
			s.isLoading = true
			s.err = nil
			return s, tea.Batch(s.spinner.Tick, loadDoctorCmd(s.svc))
		}

	case DoctorLoadedMsg:
		s.isLoading = false
		if msg.Err != nil {
			s.err = msg.Err
			return s, nil
		}
		s.checks = msg.Report.Checks
		s.updateRows()
		return s, nil

	case spinner.TickMsg:
		if s.isLoading {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(msg)
			return s, cmd
		}
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

func (s *DoctorScreen) updateRows() {
	rows := make([]table.Row, len(s.checks))
	for i, c := range s.checks {
		label := c.Label
		if label == "" {
			label = c.Status.String()
		}
		detail := c.Detail
		if detail == "" {
			detail = c.Message
		}
		rows[i] = table.Row{label, c.Name, detail}
	}
	s.table.SetRows(rows)
	if len(rows) > 0 && s.table.Cursor() < 0 {
		s.table.SetCursor(0)
	}
	s.table.UpdateViewport()
}

func (s *DoctorScreen) View(width, height int) string {
	s.width, s.height = width, height
	if width > 0 {
		s.table.SetWidth(width)
	}

	header := TitleStyle.Render("ENVIL DOCTOR  System & Security Diagnostics")

	if s.isLoading {
		return fmt.Sprintf("%s\n\n  %s Running diagnostics...", header, s.spinner.View())
	}

	if s.err != nil {
		return fmt.Sprintf("%s\n\n  %s\n\n  Press 'r' to re-run or 'esc' to go back.",
			header,
			ErrorStyle.Render(fmt.Sprintf("Error: %s", errs.Format(s.err))),
		)
	}

	var detailPane string
	cursor := s.table.Cursor()
	if cursor >= 0 && cursor < len(s.checks) {
		selected := s.checks[cursor]
		label := selected.Label
		if label == "" {
			label = selected.Status.String()
		}
		detail := selected.Detail
		if detail == "" {
			detail = selected.Message
		}
		hint := selected.Hint
		if hint == "" {
			hint = "No action required."
		}
		detailPane = fmt.Sprintf(
			"\n  Selected: [%s] %s — %s\n  Hint: %s",
			styleStatusLabel(label),
			selected.Name,
			detail,
			hint,
		)
	}

	footer := "\n\n  ↑/↓ navigate • r re-run • esc back • q quit"
	return header + "\n\n" + s.table.View() + detailPane + footer
}

func styleStatusLabel(label string) string {
	switch label {
	case "OK":
		return SuccessStyle.Render(label)
	case "WARN":
		return DiffModifiedStyle.Render(label)
	case "FAIL":
		return DiffRemovedStyle.Render(label)
	default:
		return SubtitleStyle.Render(label)
	}
}

func loadDoctorCmd(svc service.Service) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		report, err := svc.Doctor(ctx)
		return DoctorLoadedMsg{Report: report, Err: err}
	}
}

