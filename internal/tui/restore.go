package tui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/ibrahimraimi/envil/internal/service"
)

type RestoreScreen struct {
	svc            service.Service
	projectID      string
	version        int
	diffs          []service.DiffEntry
	cursor         int
	revealedKey    string
	revealedLocal  string
	revealedRemote string
	spinner        spinner.Model
	help           help.Model
	keyMap         RestoreKeyMap
	isLoading      bool
	isRestoring    bool
	isDone         bool
	err            error
	width          int
	height         int
}

func NewRestoreScreen(svc service.Service, projectID string, version int) *RestoreScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = TitleStyle

	return &RestoreScreen{
		svc:         svc,
		projectID:   projectID,
		version:     version,
		spinner:     sp,
		help:        help.New(),
		keyMap:      DefaultRestoreKeyMap,
		isLoading:   true,
		isRestoring: false,
		isDone:      false,
	}
}

func (s *RestoreScreen) Init() tea.Cmd {
	return tea.Batch(
		s.spinner.Tick,
		loadDiffCmd(s.svc, s.projectID, s.version),
	)
}

func (s *RestoreScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+c":
			s.clearRevealedValues()
			return s, tea.Quit

		case key.Matches(msg, s.keyMap.Cancel):
			s.clearRevealedValues()
			return s, func() tea.Msg {
				return NavigateMsg{To: NewDashboardScreen(s.svc)}
			}

		case key.Matches(msg, s.keyMap.Up):
			if s.cursor > 0 {
				s.cursor--
				s.clearRevealedValues()
			}

		case key.Matches(msg, s.keyMap.Down):
			if s.cursor < len(s.diffs)-1 {
				s.cursor++
				s.clearRevealedValues()
			}

		case key.Matches(msg, s.keyMap.Reveal):
			if len(s.diffs) > 0 && s.cursor < len(s.diffs) {
				selected := s.diffs[s.cursor]
				if s.revealedKey == selected.Key {
					s.clearRevealedValues()
				} else {
					return s, revealValueCmd(s.svc, s.projectID, s.version, selected.Filename, selected.Key)
				}
			}

		case key.Matches(msg, s.keyMap.Confirm):
			if !s.isLoading && !s.isRestoring && !s.isDone && s.err == nil {
				s.isRestoring = true
				s.clearRevealedValues()
				return s, runRestoreCmd(s.svc, s.projectID, s.version)
			}
		}

	case DiffLoadedMsg:
		s.isLoading = false
		if msg.Err != nil {
			s.err = msg.Err
			return s, nil
		}
		s.diffs = msg.Diffs
		return s, nil

	case RevealValueMsg:
		if msg.Err == nil {
			s.revealedKey = msg.Key
			s.revealedLocal = msg.LocalVal
			s.revealedRemote = msg.RemoteVal
		}
		return s, nil

	case RestoreDoneMsg:
		s.isRestoring = false
		s.isDone = true
		if msg.Err != nil {
			s.err = msg.Err
		}
		return s, nil

	case spinner.TickMsg:
		if s.isLoading || s.isRestoring {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(msg)
			return s, cmd
		}
	}

	return s, nil
}

func (s *RestoreScreen) clearRevealedValues() {
	s.revealedKey = ""
	s.revealedLocal = ""
	s.revealedRemote = ""
}

func (s *RestoreScreen) View(width, height int) string {
	s.width, s.height = width, height
	s.help.SetWidth(width)

	header := fmt.Sprintf("%s\n%s",
		TitleStyle.Render("RESTORE ENVIRONMENT FILES"),
		SubtitleStyle.Render(fmt.Sprintf("Comparing local environment against backup version v%d", s.version)),
	)

	var body string
	switch {
	case s.isLoading:
		body = fmt.Sprintf("\n  %s Fetching diff from server...", s.spinner.View())

	case s.isRestoring:
		body = fmt.Sprintf("\n  %s Restoring files from backup...", s.spinner.View())

	case s.isDone && s.err == nil:
		body = fmt.Sprintf("\n  %s Files restored successfully!\n  Existing modified files were backed up as .env.bak with permissions 0600.\n\n  Press 'esc' or 'n' to return to dashboard.",
			SuccessStyle.Render("✓"))

	case s.err != nil:
		body = fmt.Sprintf("\n  %s\n\n  Press 'esc' to return to dashboard.",
			ErrorStyle.Render(fmt.Sprintf("Error: %v", s.err)))

	case len(s.diffs) == 0:
		body = "\n  Local files are identical to remote backup.\n\n  Press 'y' to force overwrite, or 'n' to cancel."

	default:
		body = "\n" + s.renderDiffList()
	}

	footer := "\n" + s.help.View(s.keyMap)
	return header + body + footer
}

func (s *RestoreScreen) renderDiffList() string {
	var result string
	result += "  SYMBOL  FILE          KEY                             VALUE\n"
	result += "  -------------------------------------------------------------------\n"

	for i, diff := range s.diffs {
		cursorPrefix := "  "
		if i == s.cursor {
			cursorPrefix = "> "
		}

		symbol := "~"
		switch diff.Status {
		case "ADDED":
			symbol = DiffAddedStyle.Render("+")
		case "REMOVED":
			symbol = DiffRemovedStyle.Render("-")
		case "MODIFIED":
			symbol = DiffModifiedStyle.Render("~")
		}

		valDisplay := "********"
		if s.revealedKey == diff.Key {
			valDisplay = fmt.Sprintf("%s -> %s", s.revealedLocal, s.revealedRemote)
		}

		line := fmt.Sprintf("%s  %s    %-12s  %-30s  %s\n",
			cursorPrefix,
			symbol,
			diff.Filename,
			diff.Key,
			valDisplay,
		)

		if i == s.cursor {
			line = FingerprintStyle.Render(line)
		}
		result += line
	}

	result += "\n  Notice: Restoring will overwrite differing files and create .env.bak copies.\n"
	result += "  Press 'y' to confirm restore, 'v' to reveal/hide selected key, or 'n' to cancel.\n"
	return result
}

func loadDiffCmd(svc service.Service, projectID string, version int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		diffs, err := svc.Diff(ctx, projectID, version)
		return DiffLoadedMsg{Diffs: diffs, Err: err}
	}
}

func revealValueCmd(svc service.Service, projectID string, version int, filename, key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		localVal, remoteVal, err := svc.RevealDiffValue(ctx, projectID, version, filename, key)
		return RevealValueMsg{
			Filename:  filename,
			Key:       key,
			LocalVal:  localVal,
			RemoteVal: remoteVal,
			Err:       err,
		}
	}
}

func runRestoreCmd(svc service.Service, projectID string, version int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := svc.Restore(ctx, projectID, version, true)
		return RestoreDoneMsg{Err: err}
	}
}
