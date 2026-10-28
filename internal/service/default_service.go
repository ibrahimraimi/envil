package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ibrahimraimi/envil/internal/bundle"
	"github.com/ibrahimraimi/envil/internal/client"
	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/server"
)

type DefaultService struct {
	keystore     keystore.Keystore
	getServerURL func() string
}

func NewDefaultService(ks keystore.Keystore, getServerURL func() string) *DefaultService {
	return &DefaultService{
		keystore:     ks,
		getServerURL: getServerURL,
	}
}

func (s *DefaultService) resolveClientAndKeys() (*client.Client, *crypto.Keys, error) {
	keys, err := s.keystore.GetKeys()
	if err != nil {
		return nil, nil, err
	}
	apiClient := client.NewClient(s.getServerURL(), keys.Signing)
	return apiClient, keys, nil
}

func (s *DefaultService) HasKeys(ctx context.Context) (bool, error) {
	_, err := s.keystore.GetKeys()
	if err != nil {
		if errors.Is(err, keystore.ErrKeysNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *DefaultService) AccountFingerprint(ctx context.Context) (string, error) {
	keys, err := s.keystore.GetKeys()
	if err != nil {
		return "", err
	}
	accountID := keys.AccountID()
	return fmt.Sprintf("%x", accountID[:6]), nil
}

func (s *DefaultService) NewPhrase(ctx context.Context) (string, error) {
	return crypto.NewPhrase()
}

func (s *DefaultService) CompleteSetup(ctx context.Context, phrase string) error {
	cleanPhrase := strings.Join(strings.Fields(phrase), " ")
	if err := crypto.ValidatePhrase(cleanPhrase); err != nil {
		return fmt.Errorf("invalid recovery phrase: %w", err)
	}

	keys, err := crypto.KeysFromPhrase(cleanPhrase)
	if err != nil {
		return fmt.Errorf("derive keys: %w", err)
	}

	if err := s.keystore.SaveKeys(keys); err != nil {
		return fmt.Errorf("save keys locally: %w", err)
	}

	apiClient := client.NewClient(s.getServerURL(), keys.Signing)
	if _, err := apiClient.RegisterAccount(ctx); err != nil {
		return fmt.Errorf("register account on server: %w", err)
	}

	return nil
}

func (s *DefaultService) Recover(ctx context.Context, phrase string) error {
	cleanPhrase := strings.Join(strings.Fields(phrase), " ")
	if err := crypto.ValidatePhrase(cleanPhrase); err != nil {
		return errors.New("invalid recovery phrase: check word spelling and order")
	}

	keys, err := crypto.KeysFromPhrase(cleanPhrase)
	if err != nil {
		return fmt.Errorf("derive keys: %w", err)
	}

	if err := s.keystore.SaveKeys(keys); err != nil {
		return fmt.Errorf("save keys to keystore: %w", err)
	}

	return nil
}

func (s *DefaultService) ListProjects(ctx context.Context) ([]ProjectSummary, error) {
	apiClient, keys, err := s.resolveClientAndKeys()
	if err != nil {
		return nil, err
	}

	projectList, err := apiClient.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch projects list: %w", err)
	}

	summaries := make([]ProjectSummary, 0, len(projectList))
	for _, project := range projectList {
		summary := s.buildProjectSummary(ctx, apiClient, keys, project)
		summaries = append(summaries, summary)
	}

	return summaries, nil
}

func (s *DefaultService) buildProjectSummary(ctx context.Context, apiClient *client.Client, keys *crypto.Keys, project server.ProjectResponse) ProjectSummary {
	name, gitRemote := decryptProjectMeta(keys, project)

	latestVersion := 0
	updatedAt := project.CreatedAt

	backups, err := apiClient.ListBackups(ctx, project.ID)
	if err == nil && len(backups) > 0 {
		latestVersion = backups[0].Version
		updatedAt = backups[0].CreatedAt
	}

	return ProjectSummary{
		ID:            project.ID.String(),
		Name:          name,
		GitRemote:     gitRemote,
		LatestVersion: latestVersion,
		UpdatedAt:     updatedAt,
		CreatedAt:     project.CreatedAt,
	}
}

func (s *DefaultService) resolveTargetProjectID(projectID string) (uuid.UUID, string, error) {
	if projectID != "" {
		parsedUUID, err := uuid.Parse(projectID)
		if err != nil {
			return uuid.Nil, "", fmt.Errorf("invalid project id: %w", err)
		}
		return parsedUUID, "", nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("get current directory: %w", err)
	}

	projectCfg, err := LoadProjectConfig(cwd)
	if err != nil {
		return uuid.Nil, "", err
	}

	return projectCfg.ProjectID, cwd, nil
}

func (s *DefaultService) History(ctx context.Context, projectID string) ([]VersionInfo, error) {
	targetUUID, _, err := s.resolveTargetProjectID(projectID)
	if err != nil {
		return nil, err
	}

	apiClient, _, err := s.resolveClientAndKeys()
	if err != nil {
		return nil, err
	}

	backups, err := apiClient.ListBackups(ctx, targetUUID)
	if err != nil {
		return nil, fmt.Errorf("query backup history: %w", err)
	}

	versions := make([]VersionInfo, len(backups))
	for i, backup := range backups {
		versions[i] = VersionInfo{
			Number:    backup.Version,
			SizeBytes: backup.SizeBytes,
			CreatedAt: backup.CreatedAt,
		}
	}

	return versions, nil
}

func (s *DefaultService) Backup(ctx context.Context, projectID string, files []string) (int, error) {
	targetUUID, cwd, err := s.resolveTargetProjectID(projectID)
	if err != nil {
		return 0, err
	}

	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return 0, fmt.Errorf("get current directory: %w", err)
		}
	}

	filesToBackup := files
	if len(filesToBackup) == 0 {
		filesToBackup = FindDefaultEnvFiles(cwd)
	}
	if len(filesToBackup) == 0 {
		return 0, errors.New("no .env files found to back up")
	}

	bundleBytes, err := createEncryptedBundleBytes(cwd, filesToBackup)
	if err != nil {
		return 0, err
	}

	apiClient, keys, err := s.resolveClientAndKeys()
	if err != nil {
		return 0, err
	}

	nextVersion, expectedVersion, err := s.determineNextVersion(ctx, apiClient, targetUUID)
	if err != nil {
		return 0, err
	}

	sealedBlob, err := encryptBackupBundle(keys, targetUUID, nextVersion, bundleBytes)
	if err != nil {
		return 0, err
	}

	resp, err := apiClient.CreateBackup(ctx, targetUUID, expectedVersion, sealedBlob)
	if err != nil {
		if errors.Is(err, client.ErrVersionConflict) {
			return 0, errors.New("backup conflict: remote version has changed. Run diff or restore first")
		}
		return 0, fmt.Errorf("upload backup to server: %w", err)
	}

	return resp.Version, nil
}

func (s *DefaultService) determineNextVersion(ctx context.Context, apiClient *client.Client, projectID uuid.UUID) (int, int, error) {
	backups, err := apiClient.ListBackups(ctx, projectID)
	if err != nil {
		return 0, 0, fmt.Errorf("query project history: %w", err)
	}

	expectedVersion := 0
	if len(backups) > 0 {
		expectedVersion = backups[0].Version
	}
	return expectedVersion + 1, expectedVersion, nil
}

func (s *DefaultService) Diff(ctx context.Context, projectID string, version int) ([]DiffEntry, error) {
	targetUUID, cwd, err := s.resolveTargetProjectID(projectID)
	if err != nil {
		return nil, err
	}

	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get current directory: %w", err)
		}
	}

	remoteBundle, err := s.fetchAndDecryptBackup(ctx, targetUUID, version)
	if err != nil {
		return nil, err
	}

	var entries []DiffEntry
	for _, remoteFile := range remoteBundle.Files {
		localPath := filepath.Join(cwd, remoteFile.Name)
		localBytes, _ := os.ReadFile(localPath)
		fileDiff := bundle.DiffEnvContent(remoteFile.Name, string(localBytes), remoteFile.Content)

		for _, item := range fileDiff.Entries {
			if item.Change == bundle.ChangeEqual {
				continue
			}
			entries = append(entries, DiffEntry{
				Filename: remoteFile.Name,
				Key:      item.Key,
				Status:   string(item.Change),
			})
		}
	}

	return entries, nil
}

func (s *DefaultService) RevealDiffValue(ctx context.Context, projectID string, version int, filename, key string) (string, string, error) {
	targetUUID, cwd, err := s.resolveTargetProjectID(projectID)
	if err != nil {
		return "", "", err
	}

	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	remoteBundle, err := s.fetchAndDecryptBackup(ctx, targetUUID, version)
	if err != nil {
		return "", "", err
	}

	var remoteContent string
	for _, file := range remoteBundle.Files {
		if file.Name == filename {
			remoteContent = file.Content
			break
		}
	}

	localPath := filepath.Join(cwd, filename)
	localBytes, _ := os.ReadFile(localPath)

	remoteVars := bundle.ParseEnvPairs(remoteContent)
	localVars := bundle.ParseEnvPairs(string(localBytes))

	return localVars[key], remoteVars[key], nil
}

func (s *DefaultService) Restore(ctx context.Context, projectID string, version int, force bool) error {
	targetUUID, cwd, err := s.resolveTargetProjectID(projectID)
	if err != nil {
		return err
	}

	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get current directory: %w", err)
		}
	}

	remoteBundle, err := s.fetchAndDecryptBackup(ctx, targetUUID, version)
	if err != nil {
		return err
	}

	for _, entry := range remoteBundle.Files {
		if err := writeRestoredFile(cwd, entry, force); err != nil {
			return err
		}
	}

	return nil
}

func (s *DefaultService) DeleteProject(ctx context.Context, projectID string) error {
	targetUUID, cwd, err := s.resolveTargetProjectID(projectID)
	if err != nil {
		return err
	}

	apiClient, _, err := s.resolveClientAndKeys()
	if err != nil {
		return err
	}

	if err := apiClient.DeleteProject(ctx, targetUUID); err != nil {
		return fmt.Errorf("delete project on server: %w", err)
	}

	if cwd != "" {
		_ = os.Remove(filepath.Join(cwd, ProjectConfigFile))
	}

	return nil
}

func (s *DefaultService) DeleteAccount(ctx context.Context) error {
	apiClient, _, err := s.resolveClientAndKeys()
	if err != nil {
		return err
	}

	if err := apiClient.DeleteAccount(ctx); err != nil {
		return fmt.Errorf("delete account on server: %w", err)
	}

	if err := s.keystore.DeleteKeys(); err != nil {
		return fmt.Errorf("delete local keys: %w", err)
	}

	return nil
}

func (s *DefaultService) Doctor(ctx context.Context) (DoctorReport, error) {
	var checks []DoctorCheck

	keys, keyCheck := s.checkKeystoreHealth()
	checks = append(checks, keyCheck)

	serverChecks := s.checkServerHealth()
	checks = append(checks, serverChecks...)

	checks = append(checks, s.checkProjectConfigHealth(ctx, keys))
	checks = append(checks, s.checkGitSafetyHealth())

	return DoctorReport{Checks: checks}, nil
}

func (s *DefaultService) LinkCurrentDirectory(ctx context.Context, dir, name string) (string, error) {
	_, keys, err := s.resolveClientAndKeys()
	if err != nil {
		return "", err
	}

	apiClient := client.NewClient(s.getServerURL(), keys.Signing)

	projectName := name
	if projectName == "" {
		projectName = filepath.Base(dir)
	}
	gitRemote := DiscoverGitRemote(dir)

	projectUUID := uuid.New()
	projectKey, err := keys.ProjectKey(projectUUID.String())
	if err != nil {
		return "", fmt.Errorf("derive project key: %w", err)
	}

	metaBytes, err := bundle.EncodeProjectMetadata(&bundle.ProjectMetadata{
		Name:      projectName,
		GitRemote: gitRemote,
	})
	if err != nil {
		return "", fmt.Errorf("encode project metadata: %w", err)
	}

	aad := crypto.BuildAAD(keys.AccountID(), projectUUID.String(), 0)
	metaCiphertext, err := crypto.Seal(projectKey, metaBytes, aad)
	if err != nil {
		return "", fmt.Errorf("encrypt project metadata: %w", err)
	}

	_, err = apiClient.UpsertProject(ctx, projectUUID, metaCiphertext)
	if err != nil {
		return "", fmt.Errorf("create project on server: %w", err)
	}

	config := &ProjectConfig{ProjectID: projectUUID}
	if err := SaveProjectConfig(dir, config); err != nil {
		return "", fmt.Errorf("save project configuration: %w", err)
	}

	return projectUUID.String(), nil
}

func (s *DefaultService) LoadProjectConfig(dir string) (string, error) {
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		return "", err
	}
	return cfg.ProjectID.String(), nil
}

func (s *DefaultService) fetchAndDecryptBackup(ctx context.Context, projectID uuid.UUID, version int) (*bundle.Bundle, error) {
	apiClient, keys, err := s.resolveClientAndKeys()
	if err != nil {
		return nil, err
	}

	backupResp, err := apiClient.GetBackup(ctx, projectID, version)
	if err != nil {
		return nil, fmt.Errorf("fetch backup from server: %w", err)
	}

	rawCiphertext, err := base64.StdEncoding.DecodeString(backupResp.Blob)
	if err != nil {
		return nil, fmt.Errorf("decode backup blob: %w", err)
	}

	projectKey, err := keys.ProjectKey(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("derive project key: %w", err)
	}

	aad := crypto.BuildAAD(keys.AccountID(), projectID.String(), uint32(backupResp.Version))
	decryptedBytes, err := crypto.Open(projectKey, rawCiphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("decrypt backup: %w", err)
	}

	return bundle.DecodeBundle(decryptedBytes)
}

func (s *DefaultService) checkKeystoreHealth() (*crypto.Keys, DoctorCheck) {
	keys, err := s.keystore.GetKeys()
	if err != nil {
		return nil, DoctorCheck{
			Name:    "Local Keystore",
			Status:  "error",
			Message: err.Error(),
		}
	}
	return keys, DoctorCheck{
		Name:    "Local Keystore",
		Status:  "ok",
		Message: "keys loaded successfully",
	}
}

func (s *DefaultService) checkServerHealth() []DoctorCheck {
	serverURL := s.getServerURL()
	var checks []DoctorCheck

	startTime := time.Now()
	request, err := http.NewRequest(http.MethodGet, serverURL+"/v1/healthz", nil)
	if err != nil {
		checks = append(checks, DoctorCheck{Name: "Server Connectivity", Status: "error", Message: err.Error()})
		return checks
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	response, err := httpClient.Do(request)
	if err != nil {
		checks = append(checks, DoctorCheck{Name: "Server Connectivity", Status: "error", Message: err.Error()})
		return checks
	}
	defer response.Body.Close()

	duration := time.Since(startTime).Round(time.Millisecond)
	if response.StatusCode != http.StatusOK {
		checks = append(checks, DoctorCheck{Name: "Server Connectivity", Status: "error", Message: fmt.Sprintf("HTTP %d", response.StatusCode)})
		return checks
	}

	checks = append(checks, DoctorCheck{
		Name:    "Server Connectivity",
		Status:  "ok",
		Message: fmt.Sprintf("%s (latency %v)", serverURL, duration),
	})

	dateHeader := response.Header.Get("Date")
	if dateHeader == "" {
		checks = append(checks, DoctorCheck{Name: "Server Clock Skew", Status: "warning", Message: "server did not send Date header"})
		return checks
	}

	serverTime, err := http.ParseTime(dateHeader)
	if err != nil {
		checks = append(checks, DoctorCheck{Name: "Server Clock Skew", Status: "warning", Message: "unparseable Date header"})
		return checks
	}

	skewSeconds := math.Abs(time.Since(serverTime).Seconds())
	if skewSeconds > 60 {
		checks = append(checks, DoctorCheck{
			Name:    "Server Clock Skew",
			Status:  "error",
			Message: fmt.Sprintf("%.1fs detected (>300s causes signature rejection)", skewSeconds),
		})
	} else {
		checks = append(checks, DoctorCheck{
			Name:    "Server Clock Skew",
			Status:  "ok",
			Message: fmt.Sprintf("%.1fs", skewSeconds),
		})
	}

	return checks
}

func (s *DefaultService) checkProjectConfigHealth(ctx context.Context, keys *crypto.Keys) DoctorCheck {
	cwd, err := os.Getwd()
	if err != nil {
		return DoctorCheck{Name: "Current Project", Status: "warning", Message: err.Error()}
	}

	projectCfg, err := LoadProjectConfig(cwd)
	if err != nil {
		if errors.Is(err, ErrNotLinked) {
			return DoctorCheck{Name: "Current Project", Status: "warning", Message: "directory not linked (run 'envil init')"}
		}
		return DoctorCheck{Name: "Current Project", Status: "error", Message: err.Error()}
	}

	if keys == nil {
		return DoctorCheck{Name: "Current Project", Status: "ok", Message: fmt.Sprintf("linked to project %s", projectCfg.ProjectID)}
	}

	apiClient := client.NewClient(s.getServerURL(), keys.Signing)
	backups, err := apiClient.ListBackups(ctx, projectCfg.ProjectID)
	if err != nil {
		return DoctorCheck{Name: "Current Project", Status: "error", Message: fmt.Sprintf("failed to query backups: %v", err)}
	}

	return DoctorCheck{
		Name:    "Current Project",
		Status:  "ok",
		Message: fmt.Sprintf("linked to %s (%d backup(s))", projectCfg.ProjectID, len(backups)),
	}
}

func (s *DefaultService) checkGitSafetyHealth() DoctorCheck {
	cwd, err := os.Getwd()
	if err != nil {
		return DoctorCheck{Name: "Git Safety", Status: "warning", Message: err.Error()}
	}

	isProtected := CheckGitIgnoreProtectsEnv(cwd)
	if !isProtected {
		return DoctorCheck{
			Name:    "Git Safety",
			Status:  "warning",
			Message: ".gitignore does not protect .env files",
		}
	}

	return DoctorCheck{
		Name:    "Git Safety",
		Status:  "ok",
		Message: ".gitignore protects environment files",
	}
}

func createEncryptedBundleBytes(cwd string, files []string) ([]byte, error) {
	entries := make([]bundle.FileEntry, 0, len(files))
	for _, filename := range files {
		contentBytes, err := os.ReadFile(filepath.Join(cwd, filename))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filename, err)
		}
		entries = append(entries, bundle.FileEntry{
			Name:    filename,
			Content: string(contentBytes),
		})
	}

	rawBundle, err := bundle.CreateBundle(entries, "envil/"+config.CLI_VERSION)
	if err != nil {
		return nil, fmt.Errorf("create backup bundle: %w", err)
	}

	return bundle.EncodeBundle(rawBundle)
}

func encryptBackupBundle(keys *crypto.Keys, projectID uuid.UUID, version int, bundleBytes []byte) ([]byte, error) {
	projectKey, err := keys.ProjectKey(projectID.String())
	if err != nil {
		return nil, fmt.Errorf("derive project key: %w", err)
	}

	aad := crypto.BuildAAD(keys.AccountID(), projectID.String(), uint32(version))
	sealedBlob, err := crypto.Seal(projectKey, bundleBytes, aad)
	if err != nil {
		return nil, fmt.Errorf("encrypt backup blob: %w", err)
	}

	return sealedBlob, nil
}

func writeRestoredFile(baseDir string, entry bundle.FileEntry, forceOverwrite bool) error {
	targetPath := filepath.Join(baseDir, entry.Name)

	if existingBytes, err := os.ReadFile(targetPath); err == nil {
		if string(existingBytes) == entry.Content {
			return nil
		}
		if !forceOverwrite {
			return fmt.Errorf("local file '%s' exists and has differing contents. Use force to overwrite", entry.Name)
		}
		backupPath := targetPath + ".bak"
		if err := os.WriteFile(backupPath, existingBytes, 0600); err != nil {
			return fmt.Errorf("create safety backup '%s': %w", backupPath, err)
		}
	}

	if err := os.WriteFile(targetPath, []byte(entry.Content), 0600); err != nil {
		return fmt.Errorf("write restored file '%s': %w", targetPath, err)
	}

	return nil
}

func decryptProjectMeta(keys *crypto.Keys, project server.ProjectResponse) (string, string) {
	projectKey, err := keys.ProjectKey(project.ID.String())
	if err != nil {
		return "[decrypt error]", ""
	}

	metaCiphertext, err := base64.StdEncoding.DecodeString(project.MetaBlob)
	if err != nil {
		return "[invalid blob]", ""
	}

	aad := crypto.BuildAAD(keys.AccountID(), project.ID.String(), 0)
	plaintextBytes, err := crypto.Open(projectKey, metaCiphertext, aad)
	if err != nil {
		return "[decrypt error]", ""
	}

	meta, err := bundle.DecodeProjectMetadata(plaintextBytes)
	if err != nil {
		return "[unknown]", ""
	}

	return meta.Name, meta.GitRemote
}
