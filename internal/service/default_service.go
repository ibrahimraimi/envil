package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ibrahimraimi/envil/internal/bundle"
	"github.com/ibrahimraimi/envil/internal/client"
	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/errs"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/localfiles"
	"github.com/ibrahimraimi/envil/internal/server"
	"github.com/ibrahimraimi/envil/internal/state"
)

type DefaultService struct {
	keystore     keystore.Keystore
	getServerURL func() string
	statePath    string
}

func NewDefaultService(ks keystore.Keystore, getServerURL func() string) *DefaultService {
	return &DefaultService{
		keystore:     ks,
		getServerURL: getServerURL,
	}
}

func (s *DefaultService) WithStatePath(statePath string) *DefaultService {
	s.statePath = statePath
	return s
}

func (s *DefaultService) resolveClientAndKeys() (*client.Client, *crypto.Keys, error) {
	keys, err := s.keystore.GetKeys()
	if err != nil {
		return nil, nil, err
	}
	apiClient := client.NewClient(s.getServerURL(), keys.Signing)
	return apiClient, keys, nil
}

func (s *DefaultService) openStateStore(keys *crypto.Keys) (*state.Store, error) {
	stateKey, err := keys.StateKey()
	if err != nil {
		return nil, fmt.Errorf("derive state key: %w", err)
	}

	targetPath := s.statePath
	if targetPath == "" {
		if fs, ok := s.keystore.(*keystore.FileStore); ok {
			targetPath = filepath.Join(filepath.Dir(fs.FilePath()), state.StateFileName)
		} else {
			defaultPath, pathErr := state.GetDefaultStateFilePath()
			if pathErr != nil {
				return nil, pathErr
			}
			targetPath = defaultPath
		}
	}

	return state.NewStore(targetPath, stateKey), nil
}

func (s *DefaultService) verifyAndRecordVersion(
	ctx context.Context,
	keys *crypto.Keys,
	projectID string,
	serverLatest int,
	serverHash string,
	recordOnSuccess bool,
) (state.Verdict, error) {
	store, err := s.openStateStore(keys)
	if err != nil {
		return state.OK, err
	}

	prev, err := store.Get(projectID)
	if err != nil {
		return state.OK, err
	}

	if serverLatest == 0 && prev == nil {
		return state.OK, nil
	}

	verdict := state.Check(prev, serverLatest, serverHash)
	switch verdict {
	case state.Rollback:
		if ShouldAcceptServerState(ctx) {
			if err := store.Put(projectID, state.Entry{
				LatestVersion: serverLatest,
				BlobSHA256:    serverHash,
				SeenAt:        time.Now().UTC(),
			}); err != nil {
				return verdict, err
			}
			return verdict, nil
		}
		return verdict, errs.NewRollback(serverLatest, prev.LatestVersion)

	case state.Fork:
		if ShouldAcceptServerState(ctx) {
			if err := store.Put(projectID, state.Entry{
				LatestVersion: serverLatest,
				BlobSHA256:    serverHash,
				SeenAt:        time.Now().UTC(),
			}); err != nil {
				return verdict, err
			}
			return verdict, nil
		}
		return verdict, errs.NewFork(serverLatest)

	case state.FirstSeen, state.Advanced:
		if recordOnSuccess && serverLatest > 0 {
			if err := store.Put(projectID, state.Entry{
				LatestVersion: serverLatest,
				BlobSHA256:    serverHash,
				SeenAt:        time.Now().UTC(),
			}); err != nil {
				return verdict, err
			}
		}
		return verdict, nil

	default:
		if recordOnSuccess && serverLatest > 0 && serverHash != "" && (prev == nil || prev.BlobSHA256 == "") {
			if err := store.Put(projectID, state.Entry{
				LatestVersion: serverLatest,
				BlobSHA256:    serverHash,
				SeenAt:        time.Now().UTC(),
			}); err != nil {
				return verdict, err
			}
		}
		return verdict, nil
	}
}

func (s *DefaultService) ResetState(ctx context.Context, projectID string) error {
	_, keys, err := s.resolveClientAndKeys()
	if err != nil {
		return err
	}

	targetID := projectID
	if targetID == "current" {
		targetUUID, _, resolveErr := s.resolveTargetProjectID("")
		if resolveErr != nil {
			return resolveErr
		}
		targetID = targetUUID.String()
	}

	store, err := s.openStateStore(keys)
	if err != nil {
		return err
	}
	return store.Reset(targetID)
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
		return nil, err
	}

	summaries := make([]ProjectSummary, 0, len(projectList))
	for _, project := range projectList {
		summary, checkErr := s.buildProjectSummary(ctx, apiClient, keys, project)
		if checkErr != nil {
			return nil, checkErr
		}
		summaries = append(summaries, summary)
	}

	return summaries, nil
}

func (s *DefaultService) buildProjectSummary(
	ctx context.Context,
	apiClient *client.Client,
	keys *crypto.Keys,
	project server.ProjectResponse,
) (ProjectSummary, error) {
	name, gitRemote := decryptProjectMeta(keys, project)

	latestVersion := 0
	updatedAt := project.CreatedAt

	backups, err := apiClient.ListBackups(ctx, project.ID)
	if err == nil && len(backups) > 0 {
		latestVersion = backups[0].Version
		updatedAt = backups[0].CreatedAt
	}

	if err == nil {
		if _, verifyErr := s.verifyAndRecordVersion(ctx, keys, project.ID.String(), latestVersion, "", false); verifyErr != nil {
			return ProjectSummary{}, verifyErr
		}
	}

	return ProjectSummary{
		ID:            project.ID.String(),
		Name:          name,
		GitRemote:     gitRemote,
		LatestVersion: latestVersion,
		UpdatedAt:     updatedAt,
		CreatedAt:     project.CreatedAt,
	}, nil
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

	apiClient, keys, err := s.resolveClientAndKeys()
	if err != nil {
		return nil, err
	}

	backups, err := apiClient.ListBackups(ctx, targetUUID)
	if err != nil {
		return nil, err
	}

	serverLatest := 0
	if len(backups) > 0 {
		serverLatest = backups[0].Version
	}

	if _, err := s.verifyAndRecordVersion(ctx, keys, targetUUID.String(), serverLatest, "", false); err != nil {
		return nil, err
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

	nextVersion, expectedVersion, err := s.determineNextVersion(ctx, apiClient, keys, targetUUID)
	if err != nil {
		return 0, err
	}

	sealedBlob, err := encryptBackupBundle(keys, targetUUID, nextVersion, bundleBytes)
	if err != nil {
		return 0, err
	}

	resp, err := apiClient.CreateBackup(ctx, targetUUID, expectedVersion, sealedBlob)
	if err != nil {
		return 0, err
	}

	store, err := s.openStateStore(keys)
	if err != nil {
		return 0, err
	}
	if err := store.Put(targetUUID.String(), state.Entry{
		LatestVersion: resp.Version,
		BlobSHA256:    state.ComputeBlobHash(sealedBlob),
		SeenAt:        time.Now().UTC(),
	}); err != nil {
		return 0, err
	}

	return resp.Version, nil
}

func (s *DefaultService) determineNextVersion(
	ctx context.Context,
	apiClient *client.Client,
	keys *crypto.Keys,
	projectID uuid.UUID,
) (int, int, error) {
	backups, err := apiClient.ListBackups(ctx, projectID)
	if err != nil {
		return 0, 0, err
	}

	expectedVersion := 0
	if len(backups) > 0 {
		expectedVersion = backups[0].Version
	}

	if _, err := s.verifyAndRecordVersion(ctx, keys, projectID.String(), expectedVersion, "", false); err != nil {
		return 0, 0, err
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

	remoteBundle, _, err := s.fetchAndDecryptBackup(ctx, targetUUID, version)
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

	remoteBundle, _, err := s.fetchAndDecryptBackup(ctx, targetUUID, version)
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

	remoteBundle, _, err := s.fetchAndDecryptBackup(ctx, targetUUID, version)
	if err != nil {
		return err
	}

	cfg := config.LoadClientConfig("")
	now := time.Now().UTC()
	for _, entry := range remoteBundle.Files {
		if _, err := writeRestoredFile(cwd, entry, force, cfg.BackupCopies, now); err != nil {
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
		return err
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
		return err
	}

	if err := s.keystore.DeleteKeys(); err != nil {
		return fmt.Errorf("delete local keys: %w", err)
	}

	return nil
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
		return "", err
	}

	projectCfg := &ProjectConfig{ProjectID: projectUUID}
	if err := SaveProjectConfig(dir, projectCfg); err != nil {
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

func (s *DefaultService) fetchAndDecryptBackup(ctx context.Context, projectID uuid.UUID, version int) (*bundle.Bundle, int, error) {
	apiClient, keys, err := s.resolveClientAndKeys()
	if err != nil {
		return nil, 0, err
	}

	backups, err := apiClient.ListBackups(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}

	serverLatest := 0
	if len(backups) > 0 {
		serverLatest = backups[0].Version
	}

	backupResp, err := apiClient.GetBackup(ctx, projectID, version)
	if err != nil {
		return nil, 0, err
	}

	if serverLatest == 0 || (version == 0 && backupResp.Version > 0) {
		serverLatest = backupResp.Version
	}

	rawCiphertext, err := base64.StdEncoding.DecodeString(backupResp.Blob)
	if err != nil {
		return nil, 0, fmt.Errorf("decode backup blob: %w", err)
	}

	blobHash := ""
	isLatestBlob := backupResp.Version == serverLatest
	if isLatestBlob {
		blobHash = state.ComputeBlobHash(rawCiphertext)
	}

	if _, err := s.verifyAndRecordVersion(ctx, keys, projectID.String(), serverLatest, blobHash, false); err != nil {
		return nil, 0, err
	}

	projectKey, err := keys.ProjectKey(projectID.String())
	if err != nil {
		return nil, 0, fmt.Errorf("derive project key: %w", err)
	}

	aad := crypto.BuildAAD(keys.AccountID(), projectID.String(), uint32(backupResp.Version))
	decryptedBytes, err := crypto.Open(projectKey, rawCiphertext, aad)
	if err != nil {
		return nil, 0, fmt.Errorf("decrypt backup: %w", err)
	}

	decodedBundle, err := bundle.DecodeBundle(decryptedBytes)
	if err != nil {
		return nil, 0, err
	}

	if isLatestBlob {
		if _, err := s.verifyAndRecordVersion(ctx, keys, projectID.String(), serverLatest, blobHash, true); err != nil {
			return nil, 0, err
		}
	}

	return decodedBundle, backupResp.Version, nil
}

func createEncryptedBundleBytes(cwd string, files []string) ([]byte, error) {
	entries := make([]bundle.FileEntry, 0, len(files))
	for _, filename := range files {
		contentBytes, err := os.ReadFile(filepath.Join(cwd, filename))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filename, err)
		}
		entries = append(entries, bundle.FileEntry{
			Name:    filepath.Base(filename),
			Content: string(contentBytes),
			SHA256:  bundle.ComputeContentHash(string(contentBytes)),
		})
	}

	rawBundle, err := bundle.CreateBundle(entries, "envil/"+config.CLI_VERSION)
	if err != nil {
		return nil, err
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

func writeRestoredFile(baseDir string, entry bundle.FileEntry, forceOverwrite bool, backupCopies int, now time.Time) (string, error) {
	targetPath := filepath.Join(baseDir, entry.Name)
	var createdBackup string

	if existingBytes, err := os.ReadFile(targetPath); err == nil {
		if string(existingBytes) == entry.Content {
			return "", nil
		}
		if !forceOverwrite {
			return "", fmt.Errorf("local file '%s' exists and has differing contents. Use --force to overwrite", entry.Name)
		}
		if backupCopies > 0 {
			backupName, err := localfiles.CreateBackupCopy(baseDir, entry.Name, existingBytes, now)
			if err != nil {
				return "", err
			}
			createdBackup = backupName
			_ = localfiles.PruneBackupCopies(baseDir, entry.Name, backupCopies)
		}
	}

	if err := os.WriteFile(targetPath, []byte(entry.Content), 0600); err != nil {
		return "", fmt.Errorf("write restored file '%s': %w", targetPath, err)
	}
	_ = os.Chmod(targetPath, 0600)

	return createdBackup, nil
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
