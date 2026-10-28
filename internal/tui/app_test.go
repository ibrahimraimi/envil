package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ibrahimraimi/envil/internal/service"
)

func TestRootModelWindowSizeAndTerminalTooSmall(t *testing.T) {
	svc := &fakeService{hasKeys: true}
	root := New(svc, nil)

	// Below minimum size (width 50 < 60)
	rootModel, _ := root.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	view := rootModel.View()
	if !strings.Contains(view.Content, "Terminal too small") {
		t.Fatalf("expected 'Terminal too small' message, got: %s", view.Content)
	}

	// Below minimum height (height 15 < 18)
	rootModel, _ = rootModel.Update(tea.WindowSizeMsg{Width: 80, Height: 15})
	view = rootModel.View()
	if !strings.Contains(view.Content, "Terminal too small") {
		t.Fatalf("expected 'Terminal too small' message for height=15, got: %s", view.Content)
	}

	// Valid size (80x24)
	rootModel, _ = rootModel.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view = rootModel.View()
	if strings.Contains(view.Content, "Terminal too small") {
		t.Fatalf("did not expect 'Terminal too small' for 80x24, got: %s", view.Content)
	}
	if !view.AltScreen {
		t.Fatalf("expected AltScreen to be true")
	}
}

func TestRootModelCtrlCQuits(t *testing.T) {
	svc := &fakeService{hasKeys: true}
	root := New(svc, nil)

	_, cmd := root.Update(keyMsg("ctrl+c"))
	if cmd == nil {
		t.Fatalf("expected quit command on ctrl+c, got nil")
	}
}

func TestRootModelDefaultScreens(t *testing.T) {
	// Case 1: Has keys -> DashboardScreen
	svcWithKeys := &fakeService{hasKeys: true}
	rootWithKeys := New(svcWithKeys, nil)
	if _, ok := rootWithKeys.active.(*DashboardScreen); !ok {
		t.Fatalf("expected active screen to be *DashboardScreen, got %T", rootWithKeys.active)
	}

	// Case 2: No keys -> SetupScreen
	svcNoKeys := &fakeService{hasKeys: false}
	rootNoKeys := New(svcNoKeys, nil)
	if _, ok := rootNoKeys.active.(*SetupScreen); !ok {
		t.Fatalf("expected active screen to be *SetupScreen, got %T", rootNoKeys.active)
	}
}

func TestRootModelNavigation(t *testing.T) {
	svc := &fakeService{hasKeys: true}
	root := New(svc, nil)

	dash, ok := root.active.(*DashboardScreen)
	if !ok {
		t.Fatalf("expected DashboardScreen")
	}

	// Send NavigateMsg to BackupScreen
	backup := NewBackupScreen(svc, "proj-123")
	updatedModel, cmd := root.Update(NavigateMsg{To: backup})
	updatedRoot := updatedModel.(RootModel)

	if _, ok := updatedRoot.active.(*BackupScreen); !ok {
		t.Fatalf("expected active screen to become *BackupScreen, got %T", updatedRoot.active)
	}
	if cmd == nil {
		t.Fatalf("expected Init() cmd to be returned upon navigation")
	}

	_ = dash
}

func TestDashboardLoadingAndEmptyState(t *testing.T) {
	svc := &fakeService{
		hasKeys:  true,
		projects: []service.ProjectSummary{},
	}
	dash := NewDashboardScreen(svc)

	// 1. Initial loading state
	initView := dash.View(80, 24)
	if !strings.Contains(initView, "Loading projects") {
		t.Fatalf("expected 'Loading projects' during initial state, got: %s", initView)
	}

	// 2. Projects loaded: empty
	screen, _ := dash.Update(ProjectsLoadedMsg{Projects: []service.ProjectSummary{}})
	emptyView := screen.View(80, 24)
	if !strings.Contains(emptyView, "No projects found") {
		t.Fatalf("expected 'No projects found' empty state, got: %s", emptyView)
	}

	// 3. Projects loaded: with items
	projList := []service.ProjectSummary{
		{
			ID:            "11111111-1111-1111-1111-111111111111",
			Name:          "prod-api",
			LatestVersion: 4,
			UpdatedAt:     time.Now(),
		},
	}
	screen, _ = dash.Update(ProjectsLoadedMsg{Projects: projList})
	populatedView := screen.View(80, 24)
	if !strings.Contains(populatedView, "prod-api") || !strings.Contains(populatedView, "v4") {
		t.Fatalf("expected project table to show 'prod-api' and 'v4', got: %s", populatedView)
	}
}

func TestDashboardSelectionAndNavigation(t *testing.T) {
	svc := &fakeService{
		hasKeys: true,
		projects: []service.ProjectSummary{
			{
				ID:            "11111111-1111-1111-1111-111111111111",
				Name:          "prod-api",
				LatestVersion: 1,
			},
		},
	}
	dash := NewDashboardScreen(svc)
	dash.Update(ProjectsLoadedMsg{Projects: svc.projects})

	// Press Enter to open project detail
	_, cmd := dash.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatalf("expected navigation cmd on enter, got nil")
	}
	msg := cmd()
	nav, ok := msg.(NavigateMsg)
	if !ok {
		t.Fatalf("expected NavigateMsg, got %T", msg)
	}
	if _, ok := nav.To.(*ProjectDetailScreen); !ok {
		t.Fatalf("expected NavigateMsg to *ProjectDetailScreen, got %T", nav.To)
	}

	// Press 'b' to open backup
	_, cmd = dash.Update(keyMsg("b"))
	if cmd == nil {
		t.Fatalf("expected navigation cmd on 'b', got nil")
	}
	msg = cmd()
	nav, ok = msg.(NavigateMsg)
	if !ok {
		t.Fatalf("expected NavigateMsg, got %T", msg)
	}
	if _, ok := nav.To.(*BackupScreen); !ok {
		t.Fatalf("expected NavigateMsg to *BackupScreen, got %T", nav.To)
	}
}

func TestDashboardErrorAndRetry(t *testing.T) {
	svc := &fakeService{hasKeys: true}
	dash := NewDashboardScreen(svc)

	screen, _ := dash.Update(ProjectsLoadedMsg{Err: service.ErrNotLinked})
	errView := screen.View(80, 24)
	if !strings.Contains(errView, "Error:") {
		t.Fatalf("expected error display in View, got: %s", errView)
	}

	// Press 'r' to retry
	_, cmd := screen.Update(keyMsg("r"))
	if cmd == nil {
		t.Fatalf("expected batch command with loadProjects on 'r'")
	}
}
