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

type ProjectDetailScreen struct {
	svc         service.Service
	projectID   string
	projectName string
	table       table.Model
	spinner     spinner.Model
	help        help.Model
	keyMap      ProjectDetailKeyMap
	versions    []service.VersionInfo
	isLoading   bool
	err         error
	width       int
	height      int
}

func NewProjectDetailScreen(svc service.Service, projectID, projectName string) *ProjectDetailScreen {
	cols := []table.Column{
		{Title: "VERSION", Width: 12},
		{Title: "SIZE", Width: 16},
		{Title: "CREATED AT", Width: 24},
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

	return &ProjectDetailScreen{
		svc:         svc,
		projectID:   projectID,
		projectName: projectName,
		table:       tbl,
		spinner:     sp,
		help:        help.New(),
		keyMap:      DefaultProjectDetailKeyMap,
		isLoading:   true,
	}
}

func (s *ProjectDetailScreen) Init() tea.Cmd {
	return tea.Batch(
		s.spinner.Tick,
		loadHistoryCmd(s.svc, s.projectID),
	)
}

func (s *ProjectDetailScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, s.keyMap.Quit):
			return s, tea.Quit
		case key.Matches(msg, s.keyMap.Back):
			return s, func() tea.Msg {
				return NavigateMsg{To: NewDashboardScreen(s.svc)}
			}
		case key.Matches(msg, s.keyMap.Diff):
			selectedVersion := s.getSelectedVersion()
			if selectedVersion > 0 {
				return s, func() tea.Msg {
					return NavigateMsg{To: NewRestoreScreen(s.svc, s.projectID, selectedVersion)}
				}
			}
		case key.Matches(msg, s.keyMap.Restore):
			selectedVersion := s.getSelectedVersion()
			if selectedVersion > 0 {
				return s, func() tea.Msg {
					return NavigateMsg{To: NewRestoreScreen(s.svc, s.projectID, selectedVersion)}
				}
			}
		}

	case HistoryLoadedMsg:
		s.isLoading = false
		if msg.Err != nil {
			s.err = msg.Err
			return s, nil
		}
		s.versions = msg.Versions
		s.updateTableRows()
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

func (s *ProjectDetailScreen) getSelectedVersion() int {
	cursor := s.table.Cursor()
	if cursor >= 0 && cursor < len(s.versions) {
		return s.versions[cursor].Number
	}
	return 0
}

func (s *ProjectDetailScreen) updateTableRows() {
	rows := make([]table.Row, len(s.versions))
	for i, v := range s.versions {
		rows[i] = table.Row{
			fmt.Sprintf("v%d", v.Number),
			fmt.Sprintf("%d B", v.SizeBytes),
			v.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}
	s.table.SetRows(rows)
	if len(rows) > 0 && s.table.Cursor() < 0 {
		s.table.SetCursor(0)
	}
	s.table.UpdateViewport()
}

func (s *ProjectDetailScreen) View(width, height int) string {
	s.width, s.height = width, height
	s.help.SetWidth(width)
	s.table.SetWidth(width)
	tableHeight := height - 7
	if tableHeight < 5 {
		tableHeight = 5
	}
	s.table.SetHeight(tableHeight)
	s.table.UpdateViewport()

	header := fmt.Sprintf("%s\n%s",
		TitleStyle.Render(fmt.Sprintf("PROJECT: %s", s.projectName)),
		SubtitleStyle.Render(fmt.Sprintf("ID: %s", s.projectID)),
	)

	var content string
	switch {
	case s.isLoading:
		content = fmt.Sprintf("\n  %s Loading backup history...", s.spinner.View())
	case s.err != nil:
		content = fmt.Sprintf("\n  %s\n\n  Press 'esc' to go back.", ErrorStyle.Render(fmt.Sprintf("Error: %v", s.err)))
	case len(s.versions) == 0:
		content = "\n  No backups found for this project.\n  Press 'esc' to return to dashboard."
	default:
		content = "\n" + s.table.View()
	}

	footer := "\n" + s.help.View(s.keyMap)
	return header + content + footer
}

func loadHistoryCmd(svc service.Service, projectID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		versions, err := svc.History(ctx, projectID)
		return HistoryLoadedMsg{Versions: versions, Err: err}
	}
}
