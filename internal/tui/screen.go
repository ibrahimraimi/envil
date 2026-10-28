package tui

import (
	tea "charm.land/bubbletea/v2"
)

type Screen interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (Screen, tea.Cmd)
	View(width, height int) string
}
