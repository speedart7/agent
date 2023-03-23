package file

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grafana/agent/component"
	"github.com/grafana/agent/pkg/river"
	"github.com/grafana/agent/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestModule(t *testing.T) {
	tempDir := t.TempDir()
	debugLevelFile := filepath.Join(tempDir, "debug_level.txt")
	os.WriteFile(debugLevelFile, []byte("info"), 0664)

	tt := []struct {
		name                              string
		moduleContents                    string
		expectedHealthType                component.HealthType
		expectedHealthMessagePrefix       string
		expectedModuleHealthType          component.HealthType
		expectedModuleHealthMessagePrefix string
	}{
		{
			name: "Good Module",
			moduleContents: `local.file "log_level" {
				filename  = "` + debugLevelFile + `"
			}`,
			expectedHealthType:          component.HealthTypeHealthy,
			expectedHealthMessagePrefix: "module content loaded",

			expectedModuleHealthType:          component.HealthTypeHealthy,
			expectedModuleHealthMessagePrefix: "read file",
		},
		{
			name:                        "Bad Module",
			moduleContents:              `this isn't a valid module config`,
			expectedHealthType:          component.HealthTypeUnhealthy,
			expectedHealthMessagePrefix: "failed to parse module content",

			expectedModuleHealthType:          component.HealthTypeHealthy,
			expectedModuleHealthMessagePrefix: "read file",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			moduleFile := filepath.Join(tempDir, "module.river")
			os.WriteFile(moduleFile, []byte(tc.moduleContents), 0664)

			opts := component.Options{
				ID:            "module.file.test",
				Logger:        util.TestFlowLogger(t),
				Registerer:    prometheus.NewRegistry(),
				OnStateChange: func(e component.Exports) {},
				DataPath:      t.TempDir(),
			}

			moduleFileConfig := `filename = "` + moduleFile + `"`

			var args Arguments
			require.NoError(t, river.Unmarshal([]byte(moduleFileConfig), &args))

			c, err := New(opts, args)
			require.NoError(t, err)

			go c.Run(context.Background())
			time.Sleep(200 * time.Millisecond)

			require.Equal(t, tc.expectedHealthType, c.CurrentHealth().Health)
			require.True(t, strings.HasPrefix(c.CurrentHealth().Message, tc.expectedHealthMessagePrefix))
			require.Equal(t, tc.moduleContents, c.content.Value)

			require.Equal(t, tc.expectedModuleHealthType, c.managedLocalFile.CurrentHealth().Health)
			require.True(t, strings.HasPrefix(c.managedLocalFile.CurrentHealth().Message, tc.expectedModuleHealthMessagePrefix))
		})
	}
}

func TestMissingFile(t *testing.T) {
	opts := component.Options{
		ID:            "module.file.test",
		Logger:        util.TestFlowLogger(t),
		Registerer:    prometheus.NewRegistry(),
		OnStateChange: func(e component.Exports) {},
		DataPath:      t.TempDir(),
	}

	cfg := `
		filename = "` + filepath.Join(t.TempDir(), "module.river") + `"
	`
	var args Arguments
	require.NoError(t, river.Unmarshal([]byte(cfg), &args))

	_, err := New(opts, args)
	require.ErrorContains(t, err, "failed to read file:")
}
