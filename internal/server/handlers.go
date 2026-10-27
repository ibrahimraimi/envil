package server

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ibrahimraimi/envil/internal/auth"
	"github.com/ibrahimraimi/envil/internal/store"
)

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func WriteError(w http.ResponseWriter, statusCode int, code, message string) {
	WriteJSON(w, statusCode, ErrorResponse{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}

type AccountResponse struct {
	ID         string    `json:"id"`
	PublicKey  string    `json:"public_key"`
	QuotaBytes int64     `json:"quota_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

type ProjectResponse struct {
	ID        uuid.UUID `json:"id"`
	MetaBlob  string    `json:"meta_blob"`
	CreatedAt time.Time `json:"created_at"`
}

type UpsertProjectRequest struct {
	MetaBlob string `json:"meta_blob"`
}

type CreateBackupRequest struct {
	ExpectedVersion int    `json:"expected_version"`
	Blob            string `json:"blob"`
}

type BackupResponse struct {
	ID        int64     `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Version   int       `json:"version"`
	Blob      string    `json:"blob,omitempty"`
	SizeBytes int       `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

func HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func HandleRegisterAccount(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.GetIdentity(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing identity context")
			return
		}

		account, err := st.RegisterAccount(r.Context(), identity.AccountID, identity.PublicKey)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to register account")
			return
		}

		response := AccountResponse{
			ID:         hex.EncodeToString(account.ID[:]),
			PublicKey:  base64.StdEncoding.EncodeToString(account.PublicKey),
			QuotaBytes: account.QuotaBytes,
			CreatedAt:  account.CreatedAt,
		}
		WriteJSON(w, http.StatusOK, map[string]any{"account": response})
	}
}

func HandleDeleteAccount(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.GetIdentity(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing identity context")
			return
		}

		if err := st.DeleteAccount(r.Context(), identity.AccountID); err != nil {
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to delete account")
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func HandleListProjects(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.GetIdentity(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing identity context")
			return
		}

		projects, err := st.ListProjects(r.Context(), identity.AccountID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list projects")
			return
		}

		responseList := make([]ProjectResponse, len(projects))
		for index, project := range projects {
			responseList[index] = ProjectResponse{
				ID:        project.ID,
				MetaBlob:  base64.StdEncoding.EncodeToString(project.MetaBlob),
				CreatedAt: project.CreatedAt,
			}
		}

		WriteJSON(w, http.StatusOK, map[string]any{"projects": responseList})
	}
}

func HandleUpsertProject(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.GetIdentity(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing identity context")
			return
		}

		projectIDString := chi.URLParam(r, "id")
		projectID, err := uuid.Parse(projectIDString)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_project_id", "project id must be a valid uuid")
			return
		}

		var requestBody UpsertProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "failed to parse request payload")
			return
		}

		metaBytes, err := base64.StdEncoding.DecodeString(requestBody.MetaBlob)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_base64", "meta_blob must be valid base64")
			return
		}

		project, err := st.UpsertProject(r.Context(), projectID, identity.AccountID, metaBytes)
		if err != nil {
			mapStoreError(w, err)
			return
		}

		response := ProjectResponse{
			ID:        project.ID,
			MetaBlob:  base64.StdEncoding.EncodeToString(project.MetaBlob),
			CreatedAt: project.CreatedAt,
		}
		WriteJSON(w, http.StatusOK, map[string]any{"project": response})
	}
}

func HandleDeleteProject(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.GetIdentity(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing identity context")
			return
		}

		projectIDString := chi.URLParam(r, "id")
		projectID, err := uuid.Parse(projectIDString)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_project_id", "project id must be a valid uuid")
			return
		}

		if err := st.SoftDeleteProject(r.Context(), projectID, identity.AccountID); err != nil {
			mapStoreError(w, err)
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func HandleCreateBackup(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.GetIdentity(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing identity context")
			return
		}

		projectIDString := chi.URLParam(r, "id")
		projectID, err := uuid.Parse(projectIDString)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_project_id", "project id must be a valid uuid")
			return
		}

		var requestBody CreateBackupRequest
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "failed to parse request payload")
			return
		}

		blobBytes, err := base64.StdEncoding.DecodeString(requestBody.Blob)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_base64", "blob must be valid base64")
			return
		}

		backup, err := st.CreateBackup(r.Context(), projectID, identity.AccountID, requestBody.ExpectedVersion, blobBytes)
		if err != nil {
			mapStoreError(w, err)
			return
		}

		response := BackupResponse{
			ID:        backup.ID,
			ProjectID: backup.ProjectID,
			Version:   backup.Version,
			SizeBytes: backup.SizeBytes,
			CreatedAt: backup.CreatedAt,
		}
		WriteJSON(w, http.StatusCreated, map[string]any{"backup": response})
	}
}

func HandleListBackups(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.GetIdentity(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing identity context")
			return
		}

		projectIDString := chi.URLParam(r, "id")
		projectID, err := uuid.Parse(projectIDString)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_project_id", "project id must be a valid uuid")
			return
		}

		headers, err := st.ListBackups(r.Context(), projectID, identity.AccountID)
		if err != nil {
			mapStoreError(w, err)
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{"backups": headers})
	}
}

func HandleGetBackup(st store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.GetIdentity(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "missing identity context")
			return
		}

		projectIDString := chi.URLParam(r, "id")
		projectID, err := uuid.Parse(projectIDString)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_project_id", "project id must be a valid uuid")
			return
		}

		versionParam := chi.URLParam(r, "version")
		versionNumber := 0
		if versionParam != "latest" {
			parsedVersion, err := strconv.Atoi(versionParam)
			if err != nil || parsedVersion <= 0 {
				WriteError(w, http.StatusBadRequest, "invalid_version", "version must be 'latest' or a positive integer")
				return
			}
			versionNumber = parsedVersion
		}

		backup, err := st.GetBackup(r.Context(), projectID, identity.AccountID, versionNumber)
		if err != nil {
			mapStoreError(w, err)
			return
		}

		response := BackupResponse{
			ID:        backup.ID,
			ProjectID: backup.ProjectID,
			Version:   backup.Version,
			Blob:      base64.StdEncoding.EncodeToString(backup.Blob),
			SizeBytes: backup.SizeBytes,
			CreatedAt: backup.CreatedAt,
		}
		WriteJSON(w, http.StatusOK, map[string]any{"backup": response})
	}
}

func mapStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrVersionConflict):
		WriteError(w, http.StatusConflict, "version_conflict", "backup conflict: remote version has changed, please run diff or restore")
	case errors.Is(err, store.ErrQuotaExceeded):
		WriteError(w, http.StatusRequestEntityTooLarge, "quota_exceeded", "account storage quota exceeded")
	case errors.Is(err, store.ErrProjectNotFound):
		WriteError(w, http.StatusNotFound, "project_not_found", "project not found")
	case errors.Is(err, store.ErrBackupNotFound):
		WriteError(w, http.StatusNotFound, "backup_not_found", "backup not found")
	case errors.Is(err, store.ErrUnauthorized):
		WriteError(w, http.StatusForbidden, "forbidden", "access to project is forbidden")
	default:
		WriteError(w, http.StatusInternalServerError, "internal_error", fmt.Sprintf("database operation failed: %v", err))
	}
}
