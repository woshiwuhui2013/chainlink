package client

import (
	"context"
	"testing"

	"github.com/smartcontractkit/chainlink/core/internal/testutils"
	"github.com/smartcontractkit/chainlink/core/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestUnit_Node_StateTransitions(t *testing.T) {
	wsURL := testutils.NewWSServer(t, testutils.FixtureChainID, func(method string, params gjson.Result) (string, string) {
		return "", ""
	})
	iN := NewNode(TestNodeConfig{}, logger.TestLogger(t), *wsURL, nil, "test node", 42, nil)
	n := iN.(*node)

	assert.Equal(t, NodeStateUndialed, n.State())

	t.Run("setState", func(t *testing.T) {
		n.setState(NodeStateAlive)
		assert.Equal(t, NodeStateAlive, n.state)
		n.setState(NodeStateUndialed)
		assert.Equal(t, NodeStateUndialed, n.state)
	})

	// must dial to set rpc client for use in state transitions
	err := n.dial(context.Background())
	require.NoError(t, err)

	t.Run("transitionToAlive", func(t *testing.T) {
		assert.Panics(t, func() {
			n.transitionToAlive()
		})
		n.setState(NodeStateDialed)
		n.transitionToAlive()
	})

	t.Run("transitionToInSync", func(t *testing.T) {
		n.setState(NodeStateAlive)
		assert.Panics(t, func() {
			n.transitionToInSync()
		})
		n.setState(NodeStateOutOfSync)
		n.transitionToInSync()
	})
	t.Run("transitionToOutOfSync", func(t *testing.T) {
		n.setState(NodeStateOutOfSync)
		assert.Panics(t, func() {
			n.transitionToOutOfSync()
		})
		n.setState(NodeStateAlive)
		n.transitionToOutOfSync()
	})
	t.Run("transitionToUnreachable", func(t *testing.T) {
		n.setState(NodeStateUnreachable)
		assert.Panics(t, func() {
			n.transitionToUnreachable()
		})
		n.setState(NodeStateDialed)
		n.transitionToUnreachable()
		n.setState(NodeStateAlive)
		n.transitionToUnreachable()
		n.setState(NodeStateOutOfSync)
		n.transitionToUnreachable()
	})
	t.Run("transitionToInvalidChainID", func(t *testing.T) {
		n.setState(NodeStateUnreachable)
		assert.Panics(t, func() {
			n.transitionToInvalidChainID()
		})
		n.setState(NodeStateDialed)
		n.transitionToInvalidChainID()
	})
	t.Run("Close", func(t *testing.T) {
		// first attempt panics due to node being unstarted
		assert.Panics(t, n.Close)
		// must start to allow closing
		err := n.StartOnce("test node", func() error { return nil })
		assert.NoError(t, err)
		n.Close()
		assert.Equal(t, NodeStateClosed, n.State())
		// second attempt panics due to node being stopped twice
		assert.Panics(t, n.Close)
	})
}
