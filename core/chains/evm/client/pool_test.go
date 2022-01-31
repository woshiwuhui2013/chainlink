package client_test

import (
	"context"
	"math/big"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	evmclient "github.com/smartcontractkit/chainlink/core/chains/evm/client"
	evmmocks "github.com/smartcontractkit/chainlink/core/chains/evm/mocks"
	"github.com/smartcontractkit/chainlink/core/internal/cltest"
	"github.com/smartcontractkit/chainlink/core/internal/testutils"
	"github.com/smartcontractkit/chainlink/core/logger"
)

func TestPool_Dial(t *testing.T) {
	tests := []struct {
		name            string
		poolChainID     *big.Int
		nodeChainID     int64
		sendNodeChainID int64
		nodes           []chainIDResps
		sendNodes       []chainIDResp
		errStr          string
	}{
		{
			name:            "no nodes",
			poolChainID:     testutils.FixtureChainID,
			nodeChainID:     testutils.FixtureChainID.Int64(),
			sendNodeChainID: testutils.FixtureChainID.Int64(),
			nodes:           []chainIDResps{},
			sendNodes:       []chainIDResp{},
			errStr:          "no available nodes for chain 0",
		},
		{
			name:            "normal",
			poolChainID:     testutils.FixtureChainID,
			nodeChainID:     testutils.FixtureChainID.Int64(),
			sendNodeChainID: testutils.FixtureChainID.Int64(),
			nodes: []chainIDResps{
				{ws: chainIDResp{testutils.FixtureChainID.Int64(), nil}},
			},
			sendNodes: []chainIDResp{
				{testutils.FixtureChainID.Int64(), nil},
			},
		},
		{
			name:            "node has wrong chain ID compared to pool",
			poolChainID:     testutils.FixtureChainID,
			nodeChainID:     42,
			sendNodeChainID: testutils.FixtureChainID.Int64(),
			nodes: []chainIDResps{
				{ws: chainIDResp{1, nil}},
			},
			sendNodes: []chainIDResp{
				{1, nil},
			},
			errStr: "has chain ID 42 which does not match pool chain ID of 0",
		},
		{
			name:            "sendonly node has wrong chain ID compared to pool",
			poolChainID:     testutils.FixtureChainID,
			nodeChainID:     testutils.FixtureChainID.Int64(),
			sendNodeChainID: 42,
			nodes: []chainIDResps{
				{ws: chainIDResp{testutils.FixtureChainID.Int64(), nil}},
			},
			sendNodes: []chainIDResp{
				{testutils.FixtureChainID.Int64(), nil},
			},
			errStr: "has chain ID 42 which does not match pool chain ID of 0",
		},
		{
			name:            "remote RPC has wrong chain ID for primary node (ws) - no error, it will go into retry loop",
			poolChainID:     testutils.FixtureChainID,
			nodeChainID:     testutils.FixtureChainID.Int64(),
			sendNodeChainID: testutils.FixtureChainID.Int64(),
			nodes: []chainIDResps{
				{
					ws:   chainIDResp{42, nil},
					http: &chainIDResp{testutils.FixtureChainID.Int64(), nil},
				},
			},
			sendNodes: []chainIDResp{
				{testutils.FixtureChainID.Int64(), nil},
			},
		},
		{
			name:            "remote RPC has wrong chain ID for primary node (http) - no error, it will go into retry loop",
			poolChainID:     testutils.FixtureChainID,
			nodeChainID:     testutils.FixtureChainID.Int64(),
			sendNodeChainID: testutils.FixtureChainID.Int64(),
			nodes: []chainIDResps{
				{
					ws:   chainIDResp{testutils.FixtureChainID.Int64(), nil},
					http: &chainIDResp{42, nil},
				},
			},
			sendNodes: []chainIDResp{
				{testutils.FixtureChainID.Int64(), nil},
			},
		},
		{
			name:            "remote RPC has wrong chain ID for sendonly node",
			poolChainID:     testutils.FixtureChainID,
			nodeChainID:     testutils.FixtureChainID.Int64(),
			sendNodeChainID: testutils.FixtureChainID.Int64(),
			nodes: []chainIDResps{
				{ws: chainIDResp{testutils.FixtureChainID.Int64(), nil}},
			},
			sendNodes: []chainIDResp{
				{42, nil},
			},
			// TODO: Followup; sendonly nodes should not halt if they fail to
			// dail on startup; instead should go into retry loop like
			// primaries
			// See: https://app.shortcut.com/chainlinklabs/story/31338/sendonly-nodes-should-not-halt-node-boot-if-they-fail-to-dial-instead-should-have-retry-loop-like-primaries
			errStr: "sendonly rpc ChainID doesn't match local chain ID: RPC ID=42, local ID=0",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), cltest.WaitTimeout(t))
			defer cancel()

			nodes := make([]evmclient.Node, len(test.nodes))
			for i, n := range test.nodes {
				nodes[i] = n.newNode(t, test.nodeChainID)
			}
			sendNodes := make([]evmclient.SendOnlyNode, len(test.sendNodes))
			for i, n := range test.sendNodes {
				sendNodes[i] = n.newSendOnlyNode(t, test.sendNodeChainID)
			}
			p := evmclient.NewPool(logger.TestLogger(t), nodes, sendNodes, test.poolChainID)
			err := p.Dial(ctx)
			if test.errStr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.errStr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPool_Dial_Errors(t *testing.T) {
	t.Run("starts and kicks off retry loop even if dial errors", func(t *testing.T) {
		node := new(evmmocks.Node)
		node.On("String").Return("node").Maybe()
		node.On("ChainID").Return(testutils.FixtureChainID)
		node.On("Close")
		node.Test(t)
		nodes := []evmclient.Node{node}
		p := newPool(t, nodes)

		node.On("Dial", mock.Anything).Return(errors.New("error"))

		err := p.Dial(context.Background())
		require.NoError(t, err)

		p.Close()

		node.AssertExpectations(t)
	})

	t.Run("starts and kicks off retry loop even on verification errors", func(t *testing.T) {
		node := new(evmmocks.Node)
		node.On("String").Return("node").Maybe()
		node.On("ChainID").Return(testutils.FixtureChainID)
		node.On("Close")
		node.Test(t)
		nodes := []evmclient.Node{node}
		p := newPool(t, nodes)

		node.On("Dial", mock.Anything).Return(nil)
		node.On("Verify", mock.Anything, &cltest.FixtureChainID).Return(errors.New("error"))

		err := p.Dial(context.Background())
		require.NoError(t, err)

		p.Close()

		node.AssertExpectations(t)
	})
}

type chainIDResp struct {
	chainID int64
	err     error
}

func (r *chainIDResp) newSendOnlyNode(t *testing.T, nodeChainID int64) evmclient.SendOnlyNode {
	httpURL := r.newHTTPServer(t)
	return evmclient.NewSendOnlyNode(logger.TestLogger(t), *httpURL, t.Name(), big.NewInt(nodeChainID))
}
func (r *chainIDResp) newHTTPServer(t *testing.T) *url.URL {
	rpcSrv := rpc.NewServer()
	t.Cleanup(rpcSrv.Stop)
	rpcSrv.RegisterName("eth", &chainIDService{*r})
	ts := httptest.NewServer(rpcSrv)
	t.Cleanup(ts.Close)

	httpURL, err := url.Parse(ts.URL)
	require.NoError(t, err)
	return httpURL
}

type chainIDResps struct {
	ws   chainIDResp
	http *chainIDResp
	id   int32
}

func (r *chainIDResps) newNode(t *testing.T, nodeChainID int64) evmclient.Node {
	ws := cltest.NewWSServer(t, big.NewInt(r.ws.chainID), func(method string, params gjson.Result) (string, string) {
		t.Errorf("Unexpected method call: %s(%s)", method, params)
		return "", ""
	})

	wsURL, err := url.Parse(ws)
	require.NoError(t, err)

	var httpURL *url.URL
	if r.http != nil {
		httpURL = r.http.newHTTPServer(t)
	}

	defer func() { r.id++ }()
	return evmclient.NewNode(evmclient.TestNodeConfig{}, logger.TestLogger(t), *wsURL, httpURL, t.Name(), r.id, big.NewInt(nodeChainID))
}

type chainIDService struct {
	chainIDResp
}

func (x *chainIDService) ChainId(ctx context.Context) (*hexutil.Big, error) {
	if x.err != nil {
		return nil, x.err
	}
	return (*hexutil.Big)(big.NewInt(x.chainID)), nil
}

func newPool(t *testing.T, nodes []evmclient.Node) *evmclient.Pool {
	return evmclient.NewPool(logger.TestLogger(t), nodes, []evmclient.SendOnlyNode{}, &cltest.FixtureChainID)
}

func TestPool_RunLoop(t *testing.T) {
	// TODO: All it does is report, so we might need to mock prometheus and
	// test it here?
	t.Run("with several nodes and different types of errors", func(t *testing.T) {
		// n1 := new(evmmocks.Node)
		// n1.Test(t)
		// n2 := new(evmmocks.Node)
		// n2.Test(t)
		// n3 := new(evmmocks.Node)
		// n3.Test(t)
		// nodes := []evmclient.Node{n1, n2, n3}
		// p := newPool(t, nodes)

		// n1.On("String").Maybe().Return("n1")
		// n2.On("String").Maybe().Return("n2")
		// n3.On("String").Maybe().Return("n3")

		// n1.On("Close").Maybe()
		// n2.On("Close").Maybe()
		// n3.On("Close").Maybe()

		// wait := make(chan struct{})
		// // n1 succeeds
		// n1.On("Dial", mock.Anything).Return(nil).Once()
		// n1.On("Verify", mock.Anything, &cltest.FixtureChainID).Return(nil).Once()
		// n1.On("State").Return(evmclient.NodeStateAlive)
		// // n2 fails once then succeeds in runloop
		// n2.On("Dial", mock.Anything).Return(errors.New("first error")).Once()
		// n2.On("State").Return(evmclient.NodeStateDead)
		// // n3 succeeds dial then fails verification
		// n3.On("Dial", mock.Anything).Return(nil).Once()
		// n3.On("State").Return(evmclient.NodeStateDialed)
		// n3.On("Verify", mock.Anything, &cltest.FixtureChainID).Return(errors.New("Verify error")).Once()
		// n3.On("Verify", mock.Anything, &cltest.FixtureChainID).Once().Return(nil).Run(func(_ mock.Arguments) {
		//     close(wait)
		// })

		// // Handle spurious extra calls after
		// n2.On("Dial", mock.Anything).Maybe().Return(nil)
		// n3.On("Verify", mock.Anything, mock.Anything).Maybe().Return(nil)

		// require.NoError(t, p.Dial(context.Background()))

		// select {
		// case <-wait:
		// case <-time.After(cltest.WaitTimeout(t)):
		//     t.Fatal("timed out waiting for Dial call")
		// }
		// p.Close()

		// n1.AssertExpectations(t)
		// n2.AssertExpectations(t)
	})

}
