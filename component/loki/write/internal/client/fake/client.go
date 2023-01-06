package fake

// This code is copied from Promtail. The client package is used to configure
// and run the clients that can send log entries to a Loki instance.

import (
	"sync"

	"github.com/grafana/agent/component/common/loki"
)

// Client is a fake client used for testing.
type Client struct {
	entries  loki.LogsReceiver
	received []loki.Entry
	once     sync.Once
	mtx      sync.Mutex
	wg       sync.WaitGroup
	OnStop   func()
}

func New(stop func()) *Client {
	c := &Client{
		OnStop:  stop,
		entries: make(loki.LogsReceiver),
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for e := range c.entries {
			c.mtx.Lock()
			c.received = append(c.received, e)
			c.mtx.Unlock()
		}
	}()
	return c
}

// Stop implements client.Client
func (c *Client) Stop() {
	c.once.Do(func() { close(c.entries) })
	c.wg.Wait()
	c.OnStop()
}

func (c *Client) Chan() chan<- loki.Entry {
	return c.entries
}

func (c *Client) Received() []loki.Entry {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	cpy := make([]loki.Entry, len(c.received))
	copy(cpy, c.received)
	return cpy
}

// StopNow implements client.Client
func (c *Client) StopNow() {
	c.Stop()
}

func (c *Client) Name() string {
	return "fake"
}

// Clear is used to cleanup the buffered received entries, so the same client can be re-used between
// test cases.
func (c *Client) Clear() {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.received = []loki.Entry{}
}
