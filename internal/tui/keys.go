package tui

import (
	"charm.land/bubbles/v2/key"
)

type DashboardKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Backup  key.Binding
	Doctor  key.Binding
	Refresh key.Binding
	Help    key.Binding
	Quit    key.Binding
}

func (k DashboardKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Enter, k.Backup, k.Doctor, k.Refresh, k.Quit}
}

func (k DashboardKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter},
		{k.Backup, k.Doctor, k.Refresh, k.Help, k.Quit},
	}
}

var DefaultDashboardKeyMap = DashboardKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Backup: key.NewBinding(
		key.WithKeys("b"),
		key.WithHelp("b", "backup"),
	),
	Doctor: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "doctor"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

type ProjectDetailKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Diff    key.Binding
	Restore key.Binding
	Back    key.Binding
	Quit    key.Binding
}

func (k ProjectDetailKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Diff, k.Restore, k.Back, k.Quit}
}

func (k ProjectDetailKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},
		{k.Diff, k.Restore, k.Back, k.Quit},
	}
}

var DefaultProjectDetailKeyMap = ProjectDetailKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Diff: key.NewBinding(
		key.WithKeys("d", "enter"),
		key.WithHelp("d", "diff"),
	),
	Restore: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "restore"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

type RestoreKeyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
	Reveal  key.Binding
	Up      key.Binding
	Down    key.Binding
}

func (k RestoreKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel, k.Reveal}
}

func (k RestoreKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},
		{k.Confirm, k.Cancel, k.Reveal},
	}
}

var DefaultRestoreKeyMap = RestoreKeyMap{
	Confirm: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "confirm restore"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("n", "esc"),
		key.WithHelp("n/esc", "cancel"),
	),
	Reveal: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "reveal secret"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
}
