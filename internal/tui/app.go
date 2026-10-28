package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/ibrahimraimi/envil/internal/service"
)

const (
	MinTerminalWidth  = 60
	MinTerminalHeight = 18
)

type RootModel struct {
	svc    service.Service
	active Screen
	width  int
	height int
}

func New(svc service.Service, start Screen) RootModel {
	if start == nil {
		hasKeys, err := svc.HasKeys(context.Background())
		if err == nil && hasKeys {
			start = NewDashboardScreen(svc)
		} else {
			start = NewSetupScreen(svc)
		}
	}
	return RootModel{
		svc:    svc,
		active: start,
	}
}

func (m RootModel) Init() tea.Cmd {
	if m.active == nil {
		return nil
	}
	return m.active.Init()
}

func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case NavigateMsg:
		m.active = msg.To
		if m.active == nil {
			return m, nil
		}
		return m, m.active.Init()
	}

	if m.active == nil {
		return m, nil
	}

	var cmd tea.Cmd
	m.active, cmd = m.active.Update(msg)
	return m, cmd
}

func (m RootModel) View() tea.View {
	var body string
	if m.width > 0 && m.height > 0 && (m.width < MinTerminalWidth || m.height < MinTerminalHeight) {
		body = "Terminal too small. Please enlarge the window (minimum 60x18)."
	} else if m.active != nil {
		body = m.active.View(m.width, m.height)
	}

	v := tea.NewView(body)
	v.AltScreen = true
	return v
}

func Run(svc service.Service, start Screen) error {
	program := tea.NewProgram(New(svc, start))
	_, err := program.Run()
	return err
}
