package tui

import (
	"charm.land/lipgloss/v2"
)

var (
	ColorPrimary   = lipgloss.Color("#38BDF8")
	ColorSecondary = lipgloss.Color("#94A3B8")
	ColorSuccess   = lipgloss.Color("#10B981")
	ColorDanger    = lipgloss.Color("#EF4444")
	ColorWarning   = lipgloss.Color("#F59E0B")
	ColorMuted     = lipgloss.Color("#64748B")

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Padding(0, 1)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Padding(0, 1)

	StatusBarStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Padding(0, 1)

	FingerprintStyle = lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Bold(true)

	DiffAddedStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	DiffRemovedStyle = lipgloss.NewStyle().
				Foreground(ColorDanger).
				Bold(true)

	DiffModifiedStyle = lipgloss.NewStyle().
				Foreground(ColorWarning).
				Bold(true)

	PhraseWordBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorSecondary).
				Padding(0, 1).
				Width(16)

	PhraseNumberStyle = lipgloss.NewStyle().
				Foreground(ColorMuted)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorDanger).
			Bold(true).
			Padding(0, 1)

	WarningBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(ColorWarning).
			Padding(0, 1).
			Foreground(ColorWarning)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)
)
