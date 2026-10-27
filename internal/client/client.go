package client

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ibrahimraimi/envil/internal/auth"
	"github.com/ibrahimraimi/envil/internal/server"
)

var (
	ErrVersionConflict = errors.New("backup conflict: remote version has changed, run 'envil diff' or 'envil restore' first")
	ErrQuotaExceeded   = errors.New("storage quota exceeded")
	ErrNotFound        = errors.New("resource not found")
	ErrForbidden       = errors.New("forbidden")
)

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error (%d %s): %s", e.StatusCode, e.Code, e.Message)
}

type Client struct {
	baseURL    string
	privateKey ed25519.PrivateKey
	httpClient *http.Client
}

func NewClient(baseURL string, privateKey ed25519.PrivateKey) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		privateKey: privateKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) HealthCheck(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/healthz", nil)
	if err != nil {
		return fmt.Errorf("create health check request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("execute health check: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", response.StatusCode)
	}
	return nil
}

func (c *Client) RegisterAccount(ctx context.Context) (*server.AccountResponse, error) {
	var responseBody struct {
		Account server.AccountResponse `json:"account"`
	}

	if err := c.executeRequest(ctx, http.MethodPost, "/v1/accounts", nil, &responseBody); err != nil {
		return nil, err
	}
	return &responseBody.Account, nil
}

func (c *Client) DeleteAccount(ctx context.Context) error {
	return c.executeRequest(ctx, http.MethodDelete, "/v1/account", nil, nil)
}

func (c *Client) ListProjects(ctx context.Context) ([]server.ProjectResponse, error) {
	var responseBody struct {
		Projects []server.ProjectResponse `json:"projects"`
	}

	if err := c.executeRequest(ctx, http.MethodGet, "/v1/projects", nil, &responseBody); err != nil {
		return nil, err
	}
	return responseBody.Projects, nil
}

func (c *Client) UpsertProject(ctx context.Context, projectID uuid.UUID, metaBlob []byte) (*server.ProjectResponse, error) {
	requestPayload := server.UpsertProjectRequest{
		MetaBlob: base64.StdEncoding.EncodeToString(metaBlob),
	}

	var responseBody struct {
		Project server.ProjectResponse `json:"project"`
	}

	path := fmt.Sprintf("/v1/projects/%s", projectID.String())
	if err := c.executeRequest(ctx, http.MethodPut, path, requestPayload, &responseBody); err != nil {
		return nil, err
	}
	return &responseBody.Project, nil
}

func (c *Client) DeleteProject(ctx context.Context, projectID uuid.UUID) error {
	path := fmt.Sprintf("/v1/projects/%s", projectID.String())
	return c.executeRequest(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) CreateBackup(ctx context.Context, projectID uuid.UUID, expectedVersion int, blob []byte) (*server.BackupResponse, error) {
	requestPayload := server.CreateBackupRequest{
		ExpectedVersion: expectedVersion,
		Blob:            base64.StdEncoding.EncodeToString(blob),
	}

	var responseBody struct {
		Backup server.BackupResponse `json:"backup"`
	}

	path := fmt.Sprintf("/v1/projects/%s/backups", projectID.String())
	if err := c.executeRequest(ctx, http.MethodPost, path, requestPayload, &responseBody); err != nil {
		return nil, err
	}
	return &responseBody.Backup, nil
}

func (c *Client) ListBackups(ctx context.Context, projectID uuid.UUID) ([]server.BackupResponse, error) {
	var responseBody struct {
		Backups []server.BackupResponse `json:"backups"`
	}

	path := fmt.Sprintf("/v1/projects/%s/backups", projectID.String())
	if err := c.executeRequest(ctx, http.MethodGet, path, nil, &responseBody); err != nil {
		return nil, err
	}
	return responseBody.Backups, nil
}

func (c *Client) GetBackup(ctx context.Context, projectID uuid.UUID, version int) (*server.BackupResponse, error) {
	versionString := "latest"
	if version > 0 {
		versionString = strconv.Itoa(version)
	}

	var responseBody struct {
		Backup server.BackupResponse `json:"backup"`
	}

	path := fmt.Sprintf("/v1/projects/%s/backups/%s", projectID.String(), versionString)
	if err := c.executeRequest(ctx, http.MethodGet, path, nil, &responseBody); err != nil {
		return nil, err
	}
	return &responseBody.Backup, nil
}

func (c *Client) executeRequest(ctx context.Context, method, path string, requestBody any, result any) error {
	var bodyReader io.Reader
	if requestBody != nil {
		marshaledBytes, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(marshaledBytes)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	if err := auth.SignRequest(request, c.privateKey); err != nil {
		return fmt.Errorf("sign request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 400 {
		return parseResponseError(response)
	}

	if result != nil && response.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(response.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

func parseResponseError(response *http.Response) error {
	var errorEnvelope server.ErrorResponse
	bodyBytes, err := io.ReadAll(response.Body)
	if err == nil && len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &errorEnvelope)
	}

	code := errorEnvelope.Error.Code
	message := errorEnvelope.Error.Message
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}

	switch response.StatusCode {
	case http.StatusConflict:
		return ErrVersionConflict
	case http.StatusRequestEntityTooLarge:
		return ErrQuotaExceeded
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusForbidden:
		return ErrForbidden
	default:
		return &APIError{
			StatusCode: response.StatusCode,
			Code:       code,
			Message:    message,
		}
	}
}
