package client

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/smartcontractkit/chainlink/core/internal/testutils"
	"github.com/smartcontractkit/chainlink/core/logger"
)

// nInvalid := evmclient.NewNode(logger.TestLogger(t), , nil, "test node")
// wsURL := testutils.NewWSServer(t, &testutils.FixtureChainID, func(method string, params gjson.Result) (string, string) {
//     return "", ""
// })

func newTestNode(t *testing.T, cfg NodeConfig) *node {
	wsURL := testutils.NewWSServer(t, testutils.FixtureChainID, func(method string, params gjson.Result) (string, string) {
		return "", ""
	})
	iN := NewNode(cfg, logger.TestLogger(t), *wsURL, nil, "test node", 42, nil)
	n := iN.(*node)
	return n
}

// dial setups up the node and puts it into the live state, bypassing the
// normal Start() method which would fire off unwanted goroutines
func dial(t *testing.T, n *node) {
	ctx, cancel := testutils.TestCtx(t)
	defer cancel()
	require.NoError(t, n.dial(ctx))
	n.setState(NodeStateAlive)
	// must start to allow closing
	err := n.StartOnce("test node", func() error { return nil })
	assert.NoError(t, err)
}

func TestUnit_NodeLifecycle_aliveLoop(t *testing.T) {
	t.Run("with no poll and sync timeouts, exits on close", func(t *testing.T) {
		pollAndSyncTimeoutsDisabledCfg := TestNodeConfig{}
		n := newTestNode(t, pollAndSyncTimeoutsDisabledCfg)
		dial(t, n)

		ch := make(chan struct{})
		go func() {
			n.wg.Add(1)
			n.aliveLoop()
			close(ch)
		}()
		n.Close()
		select {
		case <-ch:
		case <-time.After(testutils.WaitTimeout(t)):
			t.Fatal("expected aliveLoop to exit")
		}
	})

	t.Run("with threshold poll failures, transitions to unreachable", func(t *testing.T) {
		syncTimeoutsDisabledCfg := TestNodeConfig{PollFailureThreshold: 3, PollInterval: time.Microsecond}
		n := newTestNode(t, syncTimeoutsDisabledCfg)
		dial(t, n)
		defer n.Close()

		n.wg.Add(1)
		go n.aliveLoop()

		assert.Eventually(t, func() bool {
			return n.State() == NodeStateUnreachable
		}, testutils.WaitTimeout(t), 10*time.Millisecond)
	})
}

func TestIntegration_NodeLifecycle(t *testing.T) {
	t.Fatal("TODO")
}
