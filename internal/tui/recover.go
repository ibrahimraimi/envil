package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/service"
)

type RecoverScreen struct {
	svc          service.Service
	input        textinput.Model
	isSubmitting bool
	err          error
	width        int
	height       int
}

func NewRecoverScreen(svc service.Service) *RecoverScreen {
	ti := textinput.New()
	ti.EchoMode = textinput.EchoPassword
	ti.Placeholder = "paste or type your 24 words separated by spaces"
	ti.CharLimit = 512
	ti.Focus()

	return &RecoverScreen{
		svc:   svc,
		input: ti,
	}
}

func (s *RecoverScreen) Init() tea.Cmd {
	return textinput.Blink
}

func (s *RecoverScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			s.input.Reset()
			return s, tea.Quit

		case "esc":
			s.input.Reset()
			return s, func() tea.Msg {
				return NavigateMsg{To: NewDashboardScreen(s.svc)}
			}

		case "enter":
			rawInput := s.input.Value()
			cleanPhrase := strings.ToLower(strings.Join(strings.Fields(rawInput), " "))

			if err := crypto.ValidatePhrase(cleanPhrase); err != nil {
				s.err = fmt.Errorf("invalid phrase: check spelling and ensure all 24 words are entered")
				return s, nil
			}

			s.isSubmitting = true
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err := s.svc.Recover(ctx, cleanPhrase)
			s.input.Reset()

			if err != nil {
				s.isSubmitting = false
				s.err = err
				return s, nil
			}

			return s, func() tea.Msg {
				return NavigateMsg{To: NewDashboardScreen(s.svc)}
			}
		}
	}

	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

func (s *RecoverScreen) View(width, height int) string {
	s.width, s.height = width, height

	header := TitleStyle.Render("ACCOUNT RECOVERY")
	instruction := "  Enter your 24-word recovery phrase to restore encryption keys on this device.\n  Input is masked for security.\n"

	words := strings.Fields(s.input.Value())
	wordCountColor := ColorMuted
	if len(words) == 24 {
		wordCountColor = ColorSuccess
	}

	counter := StatusBarStyle.Render(
		fmt.Sprintf("Words entered: %s / 24",
			lipgloss.NewStyle().Foreground(wordCountColor).Bold(true).Render(fmt.Sprintf("%d", len(words))),
		),
	)

	inputBox := fmt.Sprintf("  Phrase: %s\n\n  %s", s.input.View(), counter)

	var errorView string
	if s.err != nil {
		errorView = fmt.Sprintf("\n  %s\n", ErrorStyle.Render(s.err.Error()))
	}

	footer := "\n  Press [enter] to submit phrase and recover account. Press [esc] to cancel."
	return fmt.Sprintf("%s\n\n%s\n%s%s%s", header, instruction, inputBox, errorView, footer)
}
