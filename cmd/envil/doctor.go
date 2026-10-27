package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/keystore"
)

func newDoctorCommand(ks keystore.Keystore, getServerURL func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose local keystore, server connectivity, clock skew, and git safety",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			serverURL := getServerURL()
			return executeDoctor(ctx, ks, serverURL)
		},
	}
}

func executeDoctor(ctx context.Context, ks keystore.Keystore, serverURL string) error {
	fmt.Println("Running Envil diagnostics...")
	fmt.Println()

	keys := checkKeystore(ks)
	checkServer(serverURL)
	checkCurrentProject(ctx, ks, serverURL, keys)
	checkGitSafety()

	fmt.Println("\nDiagnostics completed.")
	return nil
}

func checkKeystore(ks keystore.Keystore) *crypto.Keys {
	keys, err := ks.GetKeys()
	if err != nil {
		fmt.Printf("❌ Local Keystore: %v\n", err)
		return nil
	}

	fmt.Println("✓ Local Keystore: OK (keys loaded successfully)")
	return keys
}

func checkServer(serverURL string) {
	startTime := time.Now()
	request, err := http.NewRequest(http.MethodGet, serverURL+"/v1/healthz", nil)
	if err != nil {
		fmt.Printf("❌ Server URL: invalid URL %s (%v)\n", serverURL, err)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		fmt.Printf("❌ Server Connectivity: failed to reach %s (%v)\n", serverURL, err)
		return
	}
	defer response.Body.Close()

	roundTripDuration := time.Since(startTime)
	if response.StatusCode != http.StatusOK {
		fmt.Printf("❌ Server Health: returned status %d\n", response.StatusCode)
		return
	}
	fmt.Printf("✓ Server Connectivity: OK (%s, latency %v)\n", serverURL, roundTripDuration.Round(time.Millisecond))

	checkClockSkew(response.Header.Get("Date"))
}

func checkClockSkew(dateHeader string) {
	if dateHeader == "" {
		fmt.Println("⚠️  Server Clock Skew: server did not send Date header")
		return
	}

	serverTime, err := http.ParseTime(dateHeader)
	if err != nil {
		fmt.Printf("⚠️  Server Clock Skew: unparseable server Date '%s'\n", dateHeader)
		return
	}

	skewSeconds := math.Abs(time.Since(serverTime).Seconds())
	if skewSeconds > 60 {
		fmt.Printf("❌ Server Clock Skew: %.1f seconds detected (skew > 300s will cause signature failure)\n", skewSeconds)
	} else {
		fmt.Printf("✓ Server Clock Skew: OK (%.1f seconds)\n", skewSeconds)
	}
}

func checkCurrentProject(ctx context.Context, ks keystore.Keystore, serverURL string, keys *crypto.Keys) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	projectCfg, err := LoadProjectConfig(cwd)
	if err != nil {
		fmt.Println("- Current Directory: not linked to a project (run 'envil init' to link)")
		return
	}

	fmt.Printf("✓ Project Linkage: linked to %s\n", projectCfg.ProjectID)

	if keys == nil {
		return
	}

	apiClient, _, err := ResolveClientAndKeys(ks, serverURL)
	if err != nil {
		return
	}

	backups, err := apiClient.ListBackups(ctx, projectCfg.ProjectID)
	if err != nil {
		fmt.Printf("⚠️  Remote Project Check: %v\n", err)
	} else {
		fmt.Printf("✓ Remote Project: accessible (%d backup versions found)\n", len(backups))
	}
}

func checkGitSafety() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	if CheckGitIgnoreProtectsEnv(cwd) {
		fmt.Println("✓ Git Safety: .gitignore protects .env files")
	} else {
		fmt.Println("⚠️  Git Safety: .env is not ignored in .gitignore")
	}
}
