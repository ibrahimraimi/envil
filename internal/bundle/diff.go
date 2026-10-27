package bundle

import (
	"bufio"
	"sort"
	"strings"
)

type DiffChange string

const (
	ChangeAdded    DiffChange = "ADDED"
	ChangeRemoved  DiffChange = "REMOVED"
	ChangeModified DiffChange = "MODIFIED"
	ChangeEqual    DiffChange = "EQUAL"
)

type VariableDiff struct {
	Key         string     `json:"key"`
	LocalValue  string     `json:"local_value,omitempty"`
	RemoteValue string     `json:"remote_value,omitempty"`
	Change      DiffChange `json:"change"`
}

type FileDiff struct {
	Filename string         `json:"filename"`
	Entries  []VariableDiff `json:"entries"`
}

func DiffEnvContent(filename, localContent, remoteContent string) FileDiff {
	localPairs := ParseEnvPairs(localContent)
	remotePairs := ParseEnvPairs(remoteContent)

	allKeysMap := make(map[string]bool)
	for key := range localPairs {
		allKeysMap[key] = true
	}
	for key := range remotePairs {
		allKeysMap[key] = true
	}

	sortedKeys := make([]string, 0, len(allKeysMap))
	for key := range allKeysMap {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	entries := make([]VariableDiff, 0, len(sortedKeys))
	for _, key := range sortedKeys {
		localVal, hasLocal := localPairs[key]
		remoteVal, hasRemote := remotePairs[key]

		diffEntry := buildVariableDiff(key, localVal, remoteVal, hasLocal, hasRemote)
		entries = append(entries, diffEntry)
	}

	return FileDiff{
		Filename: filename,
		Entries:  entries,
	}
}

func buildVariableDiff(key, localVal, remoteVal string, hasLocal, hasRemote bool) VariableDiff {
	if !hasLocal && hasRemote {
		return VariableDiff{
			Key:         key,
			RemoteValue: remoteVal,
			Change:      ChangeAdded,
		}
	}

	if hasLocal && !hasRemote {
		return VariableDiff{
			Key:        key,
			LocalValue: localVal,
			Change:     ChangeRemoved,
		}
	}

	if localVal != remoteVal {
		return VariableDiff{
			Key:         key,
			LocalValue:  localVal,
			RemoteValue: remoteVal,
			Change:      ChangeModified,
		}
	}

	return VariableDiff{
		Key:         key,
		LocalValue:  localVal,
		RemoteValue: remoteVal,
		Change:      ChangeEqual,
	}
}

func ParseEnvPairs(content string) map[string]string {
	pairs := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		rawLine := strings.TrimSpace(scanner.Text())
		if isIgnoredLine(rawLine) {
			continue
		}

		cleanLine := strings.TrimPrefix(rawLine, "export ")
		separatorIndex := strings.IndexByte(cleanLine, '=')
		if separatorIndex <= 0 {
			continue
		}

		key := strings.TrimSpace(cleanLine[:separatorIndex])
		rawValue := cleanLine[separatorIndex+1:]
		unquotedValue := stripQuotes(strings.TrimSpace(rawValue))

		pairs[key] = unquotedValue
	}

	return pairs
}

func isIgnoredLine(line string) bool {
	return line == "" || strings.HasPrefix(line, "#")
}

func stripQuotes(input string) string {
	if len(input) >= 2 {
		if (strings.HasPrefix(input, `"`) && strings.HasSuffix(input, `"`)) ||
			(strings.HasPrefix(input, `'`) && strings.HasSuffix(input, `'`)) {
			return input[1 : len(input)-1]
		}
	}
	return input
}

func MaskSecret(secret string, shouldReveal bool) string {
	if shouldReveal {
		return secret
	}
	if len(secret) == 0 {
		return ""
	}
	return "********"
}
