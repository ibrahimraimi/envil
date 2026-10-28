package localfiles

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	BackupTimestampFormat = "20060102T150405"
)

type BackupCopyInfo struct {
	Name      string
	Timestamp time.Time
	Sequence  int
	ModTime   time.Time
}

func NextBackupCopyName(dir, filename string, now time.Time) string {
	ts := now.UTC().Format(BackupTimestampFormat)
	baseCandidate := fmt.Sprintf("%s.bak.%s", filename, ts)
	if _, err := os.Stat(filepath.Join(dir, baseCandidate)); os.IsNotExist(err) {
		return baseCandidate
	}

	for seq := 1; ; seq++ {
		candidate := fmt.Sprintf("%s.%d", baseCandidate, seq)
		if _, err := os.Stat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
			return candidate
		}
	}
}

func CreateBackupCopy(dir, filename string, content []byte, now time.Time) (string, error) {
	copyName := NextBackupCopyName(dir, filename, now)
	copyPath := filepath.Join(dir, copyName)

	if err := os.WriteFile(copyPath, content, 0600); err != nil {
		return "", fmt.Errorf("write backup copy '%s': %w", copyName, err)
	}
	_ = os.Chmod(copyPath, 0600)

	return copyName, nil
}

func PruneBackupCopies(dir, filename string, keepCount int) error {
	if keepCount < 0 {
		keepCount = 0
	}

	copies, err := ListBackupCopiesForFile(dir, filename)
	if err != nil {
		return err
	}

	if len(copies) <= keepCount {
		return nil
	}

	for _, oldCopy := range copies[keepCount:] {
		targetPath := filepath.Join(dir, oldCopy.Name)
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("prune backup copy '%s': %w", oldCopy.Name, err)
		}
	}

	return nil
}

func ListBackupCopiesForFile(dir, filename string) ([]BackupCopyInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var copies []BackupCopyInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ts, seq, ok := parseBackupCopySuffix(entry.Name(), filename)
		if !ok {
			continue
		}
		info, err := entry.Info()
		var modTime time.Time
		if err == nil {
			modTime = info.ModTime()
		}
		copies = append(copies, BackupCopyInfo{
			Name:      entry.Name(),
			Timestamp: ts,
			Sequence:  seq,
			ModTime:   modTime,
		})
	}

	sortBackupCopiesNewestFirst(copies)
	return copies, nil
}

func ListAllBackupCopies(dir string) ([]BackupCopyInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var copies []BackupCopyInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		bakIdx := strings.Index(name, ".bak.")
		if bakIdx <= 0 {
			continue
		}
		baseFilename := name[:bakIdx]
		if !strings.HasPrefix(baseFilename, ".env") {
			continue
		}
		ts, seq, ok := parseBackupCopySuffix(name, baseFilename)
		if !ok {
			continue
		}
		info, err := entry.Info()
		var modTime time.Time
		if err == nil {
			modTime = info.ModTime()
		}
		copies = append(copies, BackupCopyInfo{
			Name:      name,
			Timestamp: ts,
			Sequence:  seq,
			ModTime:   modTime,
		})
	}

	sortBackupCopiesNewestFirst(copies)
	return copies, nil
}

func parseBackupCopySuffix(entryName, filename string) (time.Time, int, bool) {
	prefix := filename + ".bak."
	if !strings.HasPrefix(entryName, prefix) {
		return time.Time{}, 0, false
	}

	rest := strings.TrimPrefix(entryName, prefix)
	parts := strings.Split(rest, ".")
	if len(parts) != 1 && len(parts) != 2 {
		return time.Time{}, 0, false
	}

	ts, err := time.Parse(BackupTimestampFormat, parts[0])
	if err != nil {
		return time.Time{}, 0, false
	}

	seq := 0
	if len(parts) == 2 {
		parsedSeq, err := strconv.Atoi(parts[1])
		if err != nil || parsedSeq <= 0 {
			return time.Time{}, 0, false
		}
		seq = parsedSeq
	}

	return ts, seq, true
}

func sortBackupCopiesNewestFirst(copies []BackupCopyInfo) {
	sort.Slice(copies, func(i, j int) bool {
		if !copies[i].Timestamp.Equal(copies[j].Timestamp) {
			return copies[i].Timestamp.After(copies[j].Timestamp)
		}
		return copies[i].Sequence > copies[j].Sequence
	})
}

