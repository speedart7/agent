package gelf

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/grafana/agent/component"
	"github.com/grafana/agent/component/common/loki"
	"github.com/grafana/agent/pkg/flow/logging"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/require"
)

func TestGelf(t *testing.T) {
	// Create opts for component
	l, err := logging.New(os.Stderr, logging.DefaultOptions)
	require.NoError(t, err)

	opts := component.Options{Logger: l}

	testMsg := `{"version":"1.1","host":"example.org","short_message":"A short message","timestamp":1231231123,"level":5,"_some_extra":"extra"}`
	ch1 := make(chan loki.Entry)

	udpListenerAddr := getFreeAddr(t)
	args := Arguments{
		ListenAddress: udpListenerAddr,
		Receivers:     []loki.LogsReceiver{ch1},
	}
	c, err := New(opts, args)
	ctx := context.Background()
	ctx, cancelFunc := context.WithTimeout(ctx, 5*time.Second)
	defer cancelFunc()
	go c.Run(ctx)
	require.NoError(t, err)
	wr, err := net.Dial("udp", udpListenerAddr)
	require.NoError(t, err)
	_, err = wr.Write([]byte(testMsg))
	require.NoError(t, err)
	found := false
	select {
	case <-ctx.Done():
		// If this is called then it failed.
		require.True(t, false)
	case e := <-ch1:
		require.True(t, strings.Contains(e.Entry.Line, "A short message"))
		found = true
	}
	require.True(t, found)
}

func getFreeAddr(t *testing.T) string {
	t.Helper()

	portNumber, err := freeport.GetFreePort()
	require.NoError(t, err)

	return fmt.Sprintf("127.0.0.1:%d", portNumber)
}
