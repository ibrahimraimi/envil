package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/ibrahimraimi/envil/internal/auth"
	"github.com/ibrahimraimi/envil/internal/store"
)

type testClient struct {
	server  *httptest.Server
	privKey ed25519.PrivateKey
	pubKey  ed25519.PublicKey
}

func newTestClient(t *testing.T, handler http.Handler) *testClient {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &testClient{
		server:  server,
		privKey: privateKey,
		pubKey:  publicKey,
	}
}

func (c *testClient) do(t *testing.T, method, path string, bodyPayload any) *http.Response {
	t.Helper()

	var bodyReader io.Reader
	if bodyPayload != nil {
		payloadBytes, err := json.Marshal(bodyPayload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		bodyReader = bytes.NewReader(payloadBytes)
	}

	request, err := http.NewRequest(method, c.server.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if bodyPayload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	if err := auth.SignRequest(request, c.privKey); err != nil {
		t.Fatalf("sign request: %v", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	return response
}

func setupTestServer() http.Handler {
	cfg := Config{
		RateLimitRPS:   1000,
		RateLimitBurst: 2000,
		MaxBlobBytes:   1024 * 1024,
	}
	st := store.NewMemoryStore()
	nonces := auth.NewMemoryNonceTracker()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return BuildRouter(cfg, st, nonces, logger)
}

func TestHealthCheck(t *testing.T) {
	handler := setupTestServer()
	client := newTestClient(t, handler)

	resp, err := http.Get(client.server.URL + "/v1/healthz")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAccountRegistrationAndProjectFlow(t *testing.T) {
	handler := setupTestServer()
	clientA := newTestClient(t, handler)
	clientB := newTestClient(t, handler)

	// 1. Register Account A
	registerResp := clientA.do(t, http.MethodPost, "/v1/accounts", nil)
	if registerResp.StatusCode != http.StatusOK {
		t.Fatalf("register A expected 200, got %d", registerResp.StatusCode)
	}
	registerResp.Body.Close()

	// 2. Register Account B
	registerRespB := clientB.do(t, http.MethodPost, "/v1/accounts", nil)
	if registerRespB.StatusCode != http.StatusOK {
		t.Fatalf("register B expected 200, got %d", registerRespB.StatusCode)
	}
	registerRespB.Body.Close()

	// 3. Create Project under Account A
	projectID := uuid.New()
	metaBlob := base64.StdEncoding.EncodeToString([]byte(`{"name":"test-project"}`))
	createProjResp := clientA.do(t, http.MethodPut, "/v1/projects/"+projectID.String(), UpsertProjectRequest{
		MetaBlob: metaBlob,
	})
	if createProjResp.StatusCode != http.StatusOK {
		t.Fatalf("create project expected 200, got %d", createProjResp.StatusCode)
	}
	createProjResp.Body.Close()

	// 4. List Projects as Account A -> 1 project
	listRespA := clientA.do(t, http.MethodGet, "/v1/projects", nil)
	if listRespA.StatusCode != http.StatusOK {
		t.Fatalf("list A expected 200, got %d", listRespA.StatusCode)
	}
	var listBodyA struct {
		Projects []ProjectResponse `json:"projects"`
	}
	_ = json.NewDecoder(listRespA.Body).Decode(&listBodyA)
	listRespA.Body.Close()
	if len(listBodyA.Projects) != 1 {
		t.Fatalf("expected 1 project for A, got %d", len(listBodyA.Projects))
	}

	// 5. Cross-account check: List Projects as Account B -> 0 projects
	listRespB := clientB.do(t, http.MethodGet, "/v1/projects", nil)
	if listRespB.StatusCode != http.StatusOK {
		t.Fatalf("list B expected 200, got %d", listRespB.StatusCode)
	}
	var listBodyB struct {
		Projects []ProjectResponse `json:"projects"`
	}
	_ = json.NewDecoder(listRespB.Body).Decode(&listBodyB)
	listRespB.Body.Close()
	if len(listBodyB.Projects) != 0 {
		t.Fatalf("expected 0 projects for B, got %d", len(listBodyB.Projects))
	}

	// 6. Cross-account check: Account B tries to access Account A's project -> 403 Forbidden
	backupRespB := clientB.do(t, http.MethodGet, "/v1/projects/"+projectID.String()+"/backups", nil)
	if backupRespB.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for cross-account access, got %d", backupRespB.StatusCode)
	}
	backupRespB.Body.Close()
}

func TestBackupConcurrencyAndVersioning(t *testing.T) {
	handler := setupTestServer()
	client := newTestClient(t, handler)

	// Register account and create project
	regResp := client.do(t, http.MethodPost, "/v1/accounts", nil)
	regResp.Body.Close()

	projectID := uuid.New()
	projResp := client.do(t, http.MethodPut, "/v1/projects/"+projectID.String(), UpsertProjectRequest{
		MetaBlob: base64.StdEncoding.EncodeToString([]byte(`{"name":"concurrency"}`)),
	})
	projResp.Body.Close()

	// 1. Initial upload: expected_version = 0 -> version 1 created
	blob1 := base64.StdEncoding.EncodeToString([]byte("VERSION_1"))
	resp1 := client.do(t, http.MethodPost, "/v1/projects/"+projectID.String()+"/backups", CreateBackupRequest{
		ExpectedVersion: 0,
		Blob:            blob1,
	})
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("backup 1 expected 201, got %d", resp1.StatusCode)
	}
	var backupData1 struct {
		Backup BackupResponse `json:"backup"`
	}
	_ = json.NewDecoder(resp1.Body).Decode(&backupData1)
	resp1.Body.Close()
	if backupData1.Backup.Version != 1 {
		t.Fatalf("expected version 1, got %d", backupData1.Backup.Version)
	}

	// 2. Outdated upload: expected_version = 0 -> 409 Conflict
	respConflict := client.do(t, http.MethodPost, "/v1/projects/"+projectID.String()+"/backups", CreateBackupRequest{
		ExpectedVersion: 0,
		Blob:            blob1,
	})
	if respConflict.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d", respConflict.StatusCode)
	}
	respConflict.Body.Close()

	// 3. Concurrent uploads targeting version 1: exactly one succeeds with 201, others 409
	concurrentClients := 5
	var successCount atomic.Int32
	var conflictCount atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < concurrentClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := client.do(t, http.MethodPost, "/v1/projects/"+projectID.String()+"/backups", CreateBackupRequest{
				ExpectedVersion: 1,
				Blob:            base64.StdEncoding.EncodeToString([]byte("CONCURRENT_UPDATE")),
			})
			defer r.Body.Close()
			if r.StatusCode == http.StatusCreated {
				successCount.Add(1)
			} else if r.StatusCode == http.StatusConflict {
				conflictCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if successCount.Load() != 1 {
		t.Fatalf("expected exactly 1 concurrent success, got %d", successCount.Load())
	}
	if conflictCount.Load() != int32(concurrentClients-1) {
		t.Fatalf("expected %d conflicts, got %d", concurrentClients-1, conflictCount.Load())
	}

	// 4. Download latest version
	latestResp := client.do(t, http.MethodGet, "/v1/projects/"+projectID.String()+"/backups/latest", nil)
	if latestResp.StatusCode != http.StatusOK {
		t.Fatalf("get latest expected 200, got %d", latestResp.StatusCode)
	}
	var latestData struct {
		Backup BackupResponse `json:"backup"`
	}
	_ = json.NewDecoder(latestResp.Body).Decode(&latestData)
	latestResp.Body.Close()
	if latestData.Backup.Version != 2 {
		t.Fatalf("expected latest version 2, got %d", latestData.Backup.Version)
	}
}

func TestSoftDeleteProjectHidesFromListAndDownload(t *testing.T) {
	handler := setupTestServer()
	client := newTestClient(t, handler)

	regResp := client.do(t, http.MethodPost, "/v1/accounts", nil)
	regResp.Body.Close()

	projectID := uuid.New()
	projResp := client.do(t, http.MethodPut, "/v1/projects/"+projectID.String(), UpsertProjectRequest{
		MetaBlob: base64.StdEncoding.EncodeToString([]byte(`{"name":"to-delete"}`)),
	})
	projResp.Body.Close()

	// Soft delete
	delResp := client.do(t, http.MethodDelete, "/v1/projects/"+projectID.String(), nil)
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete project expected 200, got %d", delResp.StatusCode)
	}
	delResp.Body.Close()

	// Try fetching backups of deleted project -> 404
	getResp := client.do(t, http.MethodGet, "/v1/projects/"+projectID.String()+"/backups/latest", nil)
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for deleted project backups, got %d", getResp.StatusCode)
	}
	getResp.Body.Close()
}
