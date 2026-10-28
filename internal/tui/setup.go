package tui

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/ibrahimraimi/envil/internal/service"
)

type setupStep int

const (
	setupStepDisplay setupStep = iota
	setupStepVerify
	setupStepComplete
)

type SetupScreen struct {
	svc          service.Service
	step         setupStep
	phrase       string
	words        []string
	verifyIndices []int
	inputs       []textinput.Model
	focusIndex   int
	err          error
	width        int
	height       int
}

func NewSetupScreen(svc service.Service) *SetupScreen {
	phrase, err := svc.NewPhrase(context.Background())
	var words []string
	if err == nil {
		words = strings.Fields(phrase)
	}

	return &SetupScreen{
		svc:    svc,
		step:   setupStepDisplay,
		phrase: phrase,
		words:  words,
		err:    err,
	}
}

func (s *SetupScreen) Init() tea.Cmd {
	return nil
}

func (s *SetupScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			s.wipePhrase()
			return s, tea.Quit

		case "esc":
			s.wipePhrase()
			return s, func() tea.Msg {
				return NavigateMsg{To: NewDashboardScreen(s.svc)}
			}

		case "enter":
			if s.step == setupStepDisplay {
				if err := s.setupVerificationInputs(); err != nil {
					s.err = err
					return s, nil
				}
				s.step = setupStepVerify
				return s, s.inputs[0].Focus()
			}

			if s.step == setupStepVerify {
				if s.focusIndex < len(s.inputs)-1 {
					s.inputs[s.focusIndex].Blur()
					s.focusIndex++
					return s, s.inputs[s.focusIndex].Focus()
				}
				// All 3 filled: verify
				return s, s.verifyAndComplete()
			}

			if s.step == setupStepComplete {
				return s, func() tea.Msg {
					return NavigateMsg{To: NewDashboardScreen(s.svc)}
				}
			}

		case "tab":
			if s.step == setupStepVerify {
				s.inputs[s.focusIndex].Blur()
				s.focusIndex = (s.focusIndex + 1) % len(s.inputs)
				return s, s.inputs[s.focusIndex].Focus()
			}

		case "r":
			if s.step == setupStepVerify {
				s.step = setupStepDisplay
				s.err = nil
				return s, nil
			}
		}
	}

	if s.step == setupStepVerify && s.focusIndex < len(s.inputs) {
		var cmd tea.Cmd
		s.inputs[s.focusIndex], cmd = s.inputs[s.focusIndex].Update(msg)
		return s, cmd
	}

	return s, nil
}

func (s *SetupScreen) setupVerificationInputs() error {
	indices, err := selectRandomIndices(3, len(s.words))
	if err != nil {
		return fmt.Errorf("choose verification words: %w", err)
	}
	s.verifyIndices = indices

	s.inputs = make([]textinput.Model, 3)
	for i, idx := range indices {
		ti := textinput.New()
		ti.Placeholder = fmt.Sprintf("Word #%d", idx+1)
		ti.CharLimit = 32
		s.inputs[i] = ti
	}
	s.focusIndex = 0
	return nil
}

func (s *SetupScreen) verifyAndComplete() tea.Cmd {
	for i, idx := range s.verifyIndices {
		expected := s.words[idx]
		entered := strings.TrimSpace(strings.ToLower(s.inputs[i].Value()))
		if entered != expected {
			s.err = fmt.Errorf("word #%d does not match. Press 'r' to view phrase again", idx+1)
			return nil
		}
	}

	// Phrase words verified: complete setup
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := s.svc.CompleteSetup(ctx, s.phrase)
	s.wipePhrase()

	if err != nil {
		s.err = err
		return nil
	}

	s.step = setupStepComplete
	s.err = nil
	return nil
}

func (s *SetupScreen) wipePhrase() {
	s.phrase = ""
	s.words = nil
}

func (s *SetupScreen) View(width, height int) string {
	s.width, s.height = width, height

	switch s.step {
	case setupStepDisplay:
		return s.viewDisplayPhrase()
	case setupStepVerify:
		return s.viewVerifyWords()
	case setupStepComplete:
		return s.viewComplete()
	}
	return ""
}

func (s *SetupScreen) viewDisplayPhrase() string {
	header := TitleStyle.Render("INITIAL SETUP: 24-WORD RECOVERY PHRASE")
	warning := WarningBoxStyle.Render(
		"WARNING: Anyone with this phrase can decrypt your environment backups.\n" +
			"If you lose your device and your phrase, your data is PERMANENTLY UNRECOVERABLE.\n" +
			"Write down all 24 words in order and store them in a secure location.",
	)

	var grid string
	for i, word := range s.words {
		grid += fmt.Sprintf("  %2d. %-14s", i+1, word)
		if (i+1)%4 == 0 {
			grid += "\n"
		}
	}

	prompt := "\n  Press [enter] when you have written down all words. Press [esc] to cancel."
	return fmt.Sprintf("%s\n\n%s\n\n%s\n%s", header, warning, grid, prompt)
}

func (s *SetupScreen) viewVerifyWords() string {
	header := TitleStyle.Render("CONFIRM RECOVERY PHRASE")
	instruction := "  To ensure your phrase is recorded correctly, enter the 3 words requested below:\n"

	var inputsView string
	for i, idx := range s.verifyIndices {
		inputsView += fmt.Sprintf("  Word #%2d: %s\n", idx+1, s.inputs[i].View())
	}

	var errorView string
	if s.err != nil {
		errorView = fmt.Sprintf("\n  %s\n", ErrorStyle.Render(s.err.Error()))
	}

	footer := "\n  Press [tab] or [enter] to move between fields. Press [r] to review phrase again."
	return fmt.Sprintf("%s\n\n%s\n%s%s%s", header, instruction, inputsView, errorView, footer)
}

func (s *SetupScreen) viewComplete() string {
	header := TitleStyle.Render("SETUP COMPLETE")
	body := fmt.Sprintf("\n  %s Encryption keys derived and saved securely.\n  Account registered on Envil server.\n\n  Press [enter] to go to Dashboard.",
		SuccessStyle.Render("✓"),
	)
	return header + "\n" + body
}

func selectRandomIndices(count, max int) ([]int, error) {
	selected := make(map[int]bool)
	var indices []int
	for len(indices) < count {
		nBig, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
		if err != nil {
			return nil, err
		}
		idx := int(nBig.Int64())
		if !selected[idx] {
			selected[idx] = true
			indices = append(indices, idx)
		}
	}
	sort.Ints(indices)
	return indices, nil
}
