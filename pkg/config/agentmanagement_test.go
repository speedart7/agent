package config

import (
	"errors"
	"testing"
	"time"

	"github.com/grafana/agent/pkg/server"
	"github.com/grafana/agent/pkg/util"
	"github.com/prometheus/common/config"
	"github.com/stretchr/testify/assert"
)

// testRemoteConfigProvider is an implementation of remoteConfigProvider that can be
// used for testing. It allows setting the values to return for both fetching the
// remote config bytes & errors as well as the cached config & errors.
type testFetcher struct {
	config *AgentManagementConfig

	fetchedConfigBytesToReturn []byte
	fetchedConfigErrorToReturn error
}

type testCache struct {
	cachedConfigToReturn      *Config
	cachedConfigErrorToReturn error
	didCacheRemoteConfig      bool
}

func (t *testFetcher) fetchRemoteConfig() ([]byte, error) {
	return t.fetchedConfigBytesToReturn, t.fetchedConfigErrorToReturn
}

func (t *testCache) getCacheRemoteConfig(expandEnvVars bool) (*Config, error) {
	return t.cachedConfigToReturn, t.cachedConfigErrorToReturn
}

func (t *testCache) setCacheRemoteConfig(r []byte) error {
	t.didCacheRemoteConfig = true
	return nil
}

var validConfig = AgentManagementConfig{
	Enabled: true,
	Url:     "https://localhost:1234/example/api",
	BasicAuth: config.BasicAuth{
		Username:     "test",
		PasswordFile: "/test/path",
	},
	Protocol:        "http",
	PollingInterval: "1m",
	CacheLocation:   "/test/path/",
	RemoteConfiguration: RemoteConfig{
		Labels:    labelMap{"b": "B", "a": "A"},
		Namespace: "test_namespace",
	},
}

func TestValidate_ValidConfig(t *testing.T) {
	assert.NoError(t, validConfig.Validate())
}

func TestValidate_InvalidBasicAuth(t *testing.T) {
	invalidConfig := &AgentManagementConfig{
		Enabled:         true,
		Url:             "https://localhost:1234",
		BasicAuth:       config.BasicAuth{},
		Protocol:        "https",
		PollingInterval: "1m",
		CacheLocation:   "/test/path/",
		RemoteConfiguration: RemoteConfig{
			Namespace: "test_namespace",
		},
	}
	assert.Error(t, invalidConfig.Validate())

	invalidConfig.BasicAuth.Username = "test"
	assert.Error(t, invalidConfig.Validate()) // Should still error as there is no password file set

	invalidConfig.BasicAuth.Username = ""
	invalidConfig.BasicAuth.PasswordFile = "/test/path"
	assert.Error(t, invalidConfig.Validate()) // Should still error as there is no username set
}

func TestValidate_InvalidPollingInterval(t *testing.T) {
	invalidConfig := &AgentManagementConfig{
		Enabled: true,
		Url:     "https://localhost:1234",
		BasicAuth: config.BasicAuth{
			Username:     "test",
			PasswordFile: "/test/path",
		},
		Protocol:        "https",
		PollingInterval: "1?",
		CacheLocation:   "/test/path/",
		RemoteConfiguration: RemoteConfig{
			Namespace: "test_namespace",
		},
	}
	assert.Error(t, invalidConfig.Validate())

	invalidConfig.PollingInterval = ""
	assert.Error(t, invalidConfig.Validate())
}

func TestValidate_MissingCacheLocation(t *testing.T) {
	invalidConfig := &AgentManagementConfig{
		Enabled: true,
		Url:     "https://localhost:1234",
		BasicAuth: config.BasicAuth{
			Username:     "test",
			PasswordFile: "/test/path",
		},
		Protocol:        "https",
		PollingInterval: "1?",
		RemoteConfiguration: RemoteConfig{
			Namespace: "test_namespace",
		},
	}
	assert.Error(t, invalidConfig.Validate())
}

// func TestGetCachedRemoteConfig(t *testing.T) {
// 	cwd := filepath.Clean("./testdata/")
// 	_, err := getCachedRemoteConfig(cwd, false)
// 	assert.NoError(t, err)
// }

func TestSleepTime(t *testing.T) {
	c := validConfig
	st, err := c.SleepTime()
	assert.NoError(t, err)
	assert.Equal(t, time.Minute*1, st)

	c.PollingInterval = "15s"
	st, err = c.SleepTime()
	assert.NoError(t, err)
	assert.Equal(t, time.Second*15, st)
}

func TestFullUrl(t *testing.T) {
	actual, err := validConfig.fullUrl()
	assert.NoError(t, err)
	assert.Equal(t, "https://localhost:1234/example/api/namespace/test_namespace/remote_config?a=A&b=B", actual)
}

func TestGetRemoteConfig_InvalidInitialConfig(t *testing.T) {
	// this is invalid because it is missing the password file
	invalidConfig := &AgentManagementConfig{
		Enabled: true,
		Url:     "https://localhost:1234/example/api",
		BasicAuth: config.BasicAuth{
			Username: "test",
		},
		Protocol:        "http",
		PollingInterval: "1m",
		CacheLocation:   "/test/path/",
		RemoteConfiguration: RemoteConfig{
			Labels:    labelMap{"b": "B", "a": "A"},
			Namespace: "test_namespace",
		},
	}

	logger := server.NewLogger(&server.DefaultConfig)
	f := testFetcher{config: invalidConfig}
	c := testCache{}
	rc := &RemoteConfigProvider{fetcher: &f, cache: &c}

	_, err := rc.GetRemoteConfig(true, logger)
	assert.Error(t, err)
	assert.False(t, c.didCacheRemoteConfig)
}

func TestGetRemoteConfig_UnmarshallableRemoteConfig(t *testing.T) {
	brokenCfg := `completely invalid config (maybe it got corrupted, maybe it was somehow set this way)`

	invalidCfgBytes := []byte(brokenCfg)

	config := validConfig
	logger := server.NewLogger(&server.DefaultConfig)
	f := testFetcher{config: &config}
	c := testCache{}
	f.fetchedConfigBytesToReturn = invalidCfgBytes
	c.cachedConfigToReturn = &DefaultConfig

	rc := &RemoteConfigProvider{fetcher: &f, cache: &c}

	cfg, err := rc.GetRemoteConfig(true, logger)
	assert.NoError(t, err)
	assert.False(t, c.didCacheRemoteConfig)

	// check that the returned config is the cached one
	assert.True(t, util.CompareYAML(*cfg, DefaultConfig))
}

func TestGetRemoteConfig_RemoteFetchFails(t *testing.T) {
	config := validConfig
	logger := server.NewLogger(&server.DefaultConfig)
	f := testFetcher{config: &config}
	c := testCache{}
	f.fetchedConfigErrorToReturn = errors.New("connection refused")
	c.cachedConfigToReturn = &DefaultConfig

	rc := &RemoteConfigProvider{fetcher: &f, cache: &c}

	cfg, err := rc.GetRemoteConfig(true, logger)
	assert.NoError(t, err)
	assert.False(t, c.didCacheRemoteConfig)

	// check that the returned config is the cached one
	assert.True(t, util.CompareYAML(*cfg, DefaultConfig))
}

func TestGetRemoteConfig_ValidRemoteConfig(t *testing.T) {
	validConfigStr := `server:
  log_level: info`

	validConfigBytes := []byte(validConfigStr)

	config := validConfig
	logger := server.NewLogger(&server.DefaultConfig)
	f := testFetcher{config: &config}
	c := testCache{}
	f.fetchedConfigBytesToReturn = validConfigBytes

	rc := &RemoteConfigProvider{fetcher: &f, cache: &c}

	_, err := rc.GetRemoteConfig(true, logger)
	assert.NoError(t, err)

	assert.NoError(t, err)
	assert.True(t, c.didCacheRemoteConfig)
}

func TestGetRemoteConfig_InvalidRemoteConfig(t *testing.T) {
	// this is invalid because it has two scrape_configs with
	// the same job_name
	invalidConfig := `
metrics:
    configs:
    - name: Metrics Snippets
      scrape_configs:
      - job_name: agent-metrics
        honor_timestamps: true
        scrape_interval: 15s
        metrics_path: /metrics
        scheme: http
        follow_redirects: true
        enable_http2: true
        static_configs:
        - targets:
          - localhost:12345
      - job_name: agent-metrics
        honor_timestamps: true
        scrape_interval: 15s
        metrics_path: /metrics
        scheme: http
        follow_redirects: true
        enable_http2: true
        static_configs:
        - targets:
          - localhost:12345`
	invalidCfgBytes := []byte(invalidConfig)

	logger := server.NewLogger(&server.DefaultConfig)
	f := testFetcher{config: &validConfig}
	c := testCache{}

	f.fetchedConfigBytesToReturn = invalidCfgBytes
	c.cachedConfigToReturn = &DefaultConfig

	rc := &RemoteConfigProvider{fetcher: &f, cache: &c}
	cfg, err := rc.GetRemoteConfig(true, logger)
	assert.NoError(t, err)
	assert.False(t, c.didCacheRemoteConfig)

	// check that the returned config is the cached one
	assert.True(t, util.CompareYAML(*cfg, DefaultConfig))
}
