package main

import (
	"github.com/ibrahimraimi/envil/internal/client"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/service"
)

const (
	PROJECT_CONFIG_FILE = service.ProjectConfigFile
)

var (
	ErrNotLinked = service.ErrNotLinked
)

type ProjectConfig = service.ProjectConfig

var (
	LoadProjectConfig         = service.LoadProjectConfig
	SaveProjectConfig         = service.SaveProjectConfig
	CheckGitIgnoreProtectsEnv = service.CheckGitIgnoreProtectsEnv
	DiscoverGitRemote         = service.DiscoverGitRemote
	FindDefaultEnvFiles       = service.FindDefaultEnvFiles
)

func ResolveClientAndKeys(ks keystore.Keystore, serverURL string) (*client.Client, *crypto.Keys, error) {
	keys, err := ks.GetKeys()
	if err != nil {
		return nil, nil, err
	}

	apiClient := client.NewClient(serverURL, keys.Signing)
	return apiClient, keys, nil
}
