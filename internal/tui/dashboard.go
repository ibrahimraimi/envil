package tui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ibrahimraimi/envil/internal/service"
)

type fingerprintLoadedMsg struct {
	fingerprint string
}

type DashboardScreen struct {
	svc         service.Service
	table       table.Model
	spinner     spinner.Model
	help        help.Model
	keyMap      DashboardKeyMap
	projects    []service.ProjectSummary
	fingerprint string
	isLoading   bool
	err         error
	width       int
	height      int
}

func NewDashboardScreen(svc service.Service) *DashboardScreen {
	cols := []table.Column{
		{Title: "NAME", Width: 20},
		{Title: "PROJECT ID", Width: 38},
		{Title: "VERSION", Width: 9},
		{Title: "LAST UPDATED", Width: 18},
	}

	tbl := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(10),
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

	return &DashboardScreen{
		svc:       svc,
		table:     tbl,
		spinner:   sp,
		help:      help.New(),
		keyMap:    DefaultDashboardKeyMap,
		isLoading: true,
	}
}

func (s *DashboardScreen) Init() tea.Cmd {
	return tea.Batch(
		s.spinner.Tick,
		loadProjectsCmd(s.svc),
		loadFingerprintCmd(s.svc),
	)
}

func (s *DashboardScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, s.keyMap.Quit):
			return s, tea.Quit
		case key.Matches(msg, s.keyMap.Refresh):
			s.isLoading = true
			s.err = nil
			return s, tea.Batch(s.spinner.Tick, loadProjectsCmd(s.svc))
		case key.Matches(msg, s.keyMap.Help):
			s.help.ShowAll = !s.help.ShowAll
			return s, nil
		case key.Matches(msg, s.keyMap.Backup):
			targetID := ""
			selectedRow := s.table.Cursor()
			if selectedRow >= 0 && selectedRow < len(s.projects) {
				targetID = s.projects[selectedRow].ID
			}
			return s, func() tea.Msg {
				return NavigateMsg{To: NewBackupScreen(s.svc, targetID)}
			}
		case key.Matches(msg, s.keyMap.Enter):
			selectedRow := s.table.Cursor()
			if selectedRow >= 0 && selectedRow < len(s.projects) {
				proj := s.projects[selectedRow]
				return s, func() tea.Msg {
					return NavigateMsg{To: NewProjectDetailScreen(s.svc, proj.ID, proj.Name)}
				}
			}
		}

	case ProjectsLoadedMsg:
		s.isLoading = false
		if msg.Err != nil {
			s.err = msg.Err
			return s, nil
		}
		s.projects = msg.Projects
		s.updateTableRows()
		return s, nil

	case fingerprintLoadedMsg:
		s.fingerprint = msg.fingerprint
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

func (s *DashboardScreen) updateTableRows() {
	rows := make([]table.Row, len(s.projects))
	for i, p := range s.projects {
		verStr := "-"
		if p.LatestVersion > 0 {
			verStr = fmt.Sprintf("v%d", p.LatestVersion)
		}
		rows[i] = table.Row{
			p.Name,
			p.ID,
			verStr,
			p.UpdatedAt.Format("2006-01-02 15:04"),
		}
	}
	s.table.SetRows(rows)
	if len(rows) > 0 && s.table.Cursor() < 0 {
		s.table.SetCursor(0)
	}
	s.table.UpdateViewport()
}

func (s *DashboardScreen) View(width, height int) string {
	s.width, s.height = width, height
	s.help.SetWidth(width)
	s.table.SetWidth(width)
	tableHeight := height - 7
	if tableHeight < 5 {
		tableHeight = 5
	}
	s.table.SetHeight(tableHeight)
	s.table.UpdateViewport()

	fingerprintText := "Account: [not loaded]"
	if s.fingerprint != "" {
		fingerprintText = fmt.Sprintf("Account: [%s]", FingerprintStyle.Render(s.fingerprint))
	}

	header := fmt.Sprintf("%s\n%s",
		TitleStyle.Render("ENVIL  Zero-Knowledge Environment Manager"),
		StatusBarStyle.Render(fingerprintText),
	)

	var content string
	switch {
	case s.isLoading:
		content = fmt.Sprintf("\n  %s Loading projects from server...", s.spinner.View())
	case s.err != nil:
		content = fmt.Sprintf("\n  %s\n\n  Press 'r' to retry or 'q' to quit.", ErrorStyle.Render(fmt.Sprintf("Error: %v", s.err)))
	case len(s.projects) == 0:
		content = "\n  No projects found on this account.\n  Press 'b' to backup current directory, or 'envil init' to link a project."
	default:
		content = "\n" + s.table.View()
	}

	footer := "\n" + s.help.View(s.keyMap)
	return header + content + footer
}

func loadProjectsCmd(svc service.Service) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		projects, err := svc.ListProjects(ctx)
		return ProjectsLoadedMsg{Projects: projects, Err: err}
	}
}

func loadFingerprintCmd(svc service.Service) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		fp, err := svc.AccountFingerprint(ctx)
		if err != nil {
			return fingerprintLoadedMsg{fingerprint: ""}
		}
		return fingerprintLoadedMsg{fingerprint: fp}
	}
}
