package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/agent/pkg/server"
	"github.com/prometheus/common/config"
)

const cacheFilename = "remote-config-cache.yaml"

type labelMap map[string]string

type RemoteConfig struct {
	Labels    labelMap `yaml:"labels"`
	Namespace string   `yaml:"namespace"`
}

type AgentManagementConfig struct {
	Enabled         bool             `yaml:"-"` // Derived from enable-features=agent-management
	Url             string           `yaml:"api_url"`
	BasicAuth       config.BasicAuth `yaml:"basic_auth"`
	Protocol        string           `yaml:"protocol"`
	PollingInterval string           `yaml:"polling_interval"`
	CacheLocation   string           `yaml:"remote_config_cache_location"`

	RemoteConfiguration RemoteConfig `yaml:"remote_configuration"`
}

// fullUrl creates and returns the URL that should be used when querying the Agent Management API,
// including the namespace, base config id, and any labels that have been specified.
func (am *AgentManagementConfig) fullUrl() (string, error) {
	fullPath, err := url.JoinPath(am.Url, "namespace", am.RemoteConfiguration.Namespace, "remote_config")
	if err != nil {
		return "", fmt.Errorf("error trying to join url: %w", err)
	}
	u, err := url.Parse(fullPath)
	if err != nil {
		return "", fmt.Errorf("error trying to parse url: %w", err)
	}
	q := u.Query()
	for label, value := range am.RemoteConfiguration.Labels {
		q.Add(label, value)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// SleepTime returns the parsed duration in between config fetches.
func (am *AgentManagementConfig) SleepTime() (time.Duration, error) {
	return time.ParseDuration(am.PollingInterval)
}

// Validate checks that necessary portions of the config have been set.
func (am *AgentManagementConfig) Validate() error {
	if am.BasicAuth.Username == "" || am.BasicAuth.PasswordFile == "" {
		return errors.New("both username and password_file fields must be specified")
	}

	if _, err := time.ParseDuration(am.PollingInterval); err != nil {
		return fmt.Errorf("error trying to parse polling interval: %w", err)
	}

	if am.RemoteConfiguration.Namespace == "" {
		return errors.New("namespace must be specified in 'remote_configuration' block of the config")
	}

	if am.CacheLocation == "" {
		return errors.New("path to cache must be specified in 'agent_management.remote_config_cache_location'")
	}

	return nil
}

type RemoteConfigProvider struct {
	fetcher fetcher
	cache   cache
}
type fetcher interface {
	fetchRemoteConfig() ([]byte, error)
}

type cache interface {
	getCacheRemoteConfig(expandEnvVars bool) (*Config, error)
	setCacheRemoteConfig(remoteConfigBytes []byte) error
}

// NewRemoteConfigProvider creates a remoteConfigProvider based on the protocol
// specified in c.AgentManagement
func NewRemoteConfigProvider(c AgentManagementConfig) (*RemoteConfigProvider, error) {
	switch p := c.Protocol; {
	case p == "http":
		r := &RemoteConfigProvider{
			fetcher: newHTTPFetcher(c),
			cache:   newFSCache(c.CacheLocation),
		}
		return r, nil
	default:
		return nil, fmt.Errorf("unsupported protocol for agent management api: %s", p)
	}
}

type httpFetcher struct {
	config AgentManagementConfig
}

type fsCache struct {
	cacheLocation string
}

func newHTTPFetcher(c AgentManagementConfig) httpFetcher {
	return httpFetcher{
		config: c,
	}
}

func newFSCache(cacheLocation string) fsCache {
	return fsCache{
		cacheLocation: cacheLocation,
	}
}

// GetRemoteConfig gets the remote config specified in the initial config, falling back to a local, cached copy
// of the remote config if the request to the remote fails. If both fail, an empty config and an
// error will be returned.
func (r RemoteConfigProvider) GetRemoteConfig(expandEnvVars bool, log *server.Logger) (*Config, error) {
	// if err := r.config.Validate(); err != nil {
	// 	return nil, fmt.Errorf("invalid initial config: %w", err)
	// }
	remoteConfigBytes, err := r.fetcher.fetchRemoteConfig()
	if err != nil {
		level.Error(log).Log("msg", "could not fetch from API, falling back to cache", "err", err)
		return r.cache.getCacheRemoteConfig(expandEnvVars)
	}
	var remoteConfig Config

	err = LoadBytes(remoteConfigBytes, expandEnvVars, &remoteConfig)
	if err != nil {
		level.Error(log).Log("msg", "could not load the response from the API, falling back to cache", "err", err)
		return r.cache.getCacheRemoteConfig(expandEnvVars)
	}

	level.Info(log).Log("msg", "fetched and loaded remote config from API")

	if err = r.cache.setCacheRemoteConfig(remoteConfigBytes); err != nil {
		level.Error(log).Log("err", fmt.Errorf("could not cache config locally: %w", err))
	}
	return &remoteConfig, nil
}

// getCacheRemoteConfig retrieves the cached remote config from the location specified
// in r.AgentManagement.CacheLocation
func (c fsCache) getCacheRemoteConfig(expandEnvVars bool) (*Config, error) {
	cachePath := filepath.Join(c.cacheLocation, cacheFilename)
	var cachedConfig Config
	if err := LoadFile(cachePath, expandEnvVars, &cachedConfig); err != nil {
		return nil, fmt.Errorf("error trying to load cached remote config from file: %w", err)
	}
	return &cachedConfig, nil
}

// setCacheRemoteConfig caches the remote config to the location specified in
// r.AgentManagement.CacheLocation
func (c fsCache) setCacheRemoteConfig(remoteConfigBytes []byte) error {
	cachePath := filepath.Join(c.cacheLocation, cacheFilename)
	return os.WriteFile(cachePath, remoteConfigBytes, 0666)
}

// FetchRemoteConfig fetches the raw bytes of the config from a remote API using
// the values in r.AgentManagement.
func (f httpFetcher) fetchRemoteConfig() ([]byte, error) {
	httpClientConfig := &config.HTTPClientConfig{
		BasicAuth: &f.config.BasicAuth,
	}

	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current working directory: %w", err)
	}
	httpClientConfig.SetDirectory(dir)

	remoteOpts := &remoteOpts{
		HTTPClientConfig: httpClientConfig,
	}

	url, err := f.config.fullUrl()
	if err != nil {
		return nil, fmt.Errorf("error trying to create full url: %w", err)
	}
	rc, err := newRemoteConfig(url, remoteOpts)
	if err != nil {
		return nil, fmt.Errorf("error reading remote config: %w", err)
	}

	bb, err := rc.retrieve()
	if err != nil {
		return nil, fmt.Errorf("error retrieving remote config: %w", err)
	}
	return bb, nil
}
