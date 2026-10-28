package tui

import (
	"github.com/ibrahimraimi/envil/internal/service"
)

type NavigateMsg struct {
	To Screen
}

type ProjectsLoadedMsg struct {
	Projects []service.ProjectSummary
	Err      error
}

type HistoryLoadedMsg struct {
	Versions []service.VersionInfo
	Err      error
}

type DiffLoadedMsg struct {
	Diffs []service.DiffEntry
	Err   error
}

type RevealValueMsg struct {
	Filename  string
	Key       string
	LocalVal  string
	RemoteVal string
	Err       error
}

type BackupProgressMsg struct {
	Step string
}

type BackupDoneMsg struct {
	Version int
	Err     error
}

type RestoreDoneMsg struct {
	Err error
}

type ErrMsg struct {
	Err error
}
