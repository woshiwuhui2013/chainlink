package testutils

import (
	"context"
	"fmt"
	"math/big"
	mrand "math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"

	"github.com/stretchr/testify/require"
	// NOTE: To avoid circular dependencies, this package may not import anything
	// from "github.com/smartcontractkit/chainlink/core"
)

// FixtureChainID matches the chain always added by fixtures.sql
// It is set to 0 since no real chain ever has this ID and allows a virtual
// "test" chain ID to be used without clashes
var FixtureChainID = big.NewInt(0)

// NewAddress return a random new address
func NewAddress() common.Address {
	return common.BytesToAddress(randomBytes(20))
}

// TestCtx returns a context that will be cancelled on test timeout
func TestCtx(t *testing.T) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), WaitTimeout(t))
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = mrand.Read(b) // Assignment for errcheck. Only used in tests so we can ignore.
	return b
}

// Random32Byte returns a random [32]byte
func Random32Byte() (b [32]byte) {
	copy(b[:], randomBytes(32))
	return b
}

// DefaultWaitTimeout is the default wait timeout. If you have a *testing.T, use WaitTimeout instead.
const DefaultWaitTimeout = 30 * time.Second

// WaitTimeout returns a timeout based on the test's Deadline, if available.
// Especially important to use in parallel tests, as their individual execution
// can get paused for arbitrary amounts of time.
func WaitTimeout(t *testing.T) time.Duration {
	if d, ok := t.Deadline(); ok {
		// 10% buffer for cleanup and scheduling delay
		return time.Until(d) * 9 / 10
	}
	return DefaultWaitTimeout
}

// Context returns a context with the test's deadline, if available.
func Context(t *testing.T) (ctx context.Context) {
	ctx = context.Background()
	if d, ok := t.Deadline(); ok {
		var cancel func()
		ctx, cancel = context.WithDeadline(ctx, d)
		t.Cleanup(cancel)
	}
	return ctx
}

// MustParseURL parses the URL or fails the test
func MustParseURL(t *testing.T, input string) *url.URL {
	u, err := url.Parse(input)
	require.NoError(t, err)
	return u
}

// JSONRPCHandler is called with the method and request param(s).
// respResult will be sent immediately. notifyResult is optional, and sent after a short delay.
type JSONRPCHandler func(reqMethod string, reqParams gjson.Result) (respResult, notifyResult string)

// NewWSServer starts a websocket server which invokes callback for each message received.
// If chainID is set, then eth_chainId calls will be automatically handled.
func NewWSServer(t *testing.T, chainID *big.Int, callback JSONRPCHandler) *url.URL {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err, "Failed to upgrade WS connection")
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseAbnormalClosure) {
					t.Log("Websocket closing")
					return
				}
				t.Logf("Failed to read message: %v", err)
				return
			}
			t.Log("Received message", string(data))
			req := gjson.ParseBytes(data)
			if !req.IsObject() {
				t.Logf("Request must be object: %v", req.Type)
				return
			}
			if e := req.Get("error"); e.Exists() {
				t.Logf("Received jsonrpc error message: %v", e)
				break
			}
			m := req.Get("method")
			if m.Type != gjson.String {
				t.Logf("Method must be string: %v", m.Type)
				return
			}

			var resp, notify string
			if chainID != nil && m.String() == "eth_chainId" {
				resp = `"0x` + chainID.Text(16) + `"`
			} else {
				resp, notify = callback(m.String(), req.Get("params"))
			}
			id := req.Get("id")
			msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, id, resp)
			t.Logf("Sending message: %v", msg)
			err = conn.WriteMessage(websocket.BinaryMessage, []byte(msg))
			if err != nil {
				t.Logf("Failed to write message: %v", err)
				return
			}

			if notify != "" {
				time.Sleep(100 * time.Millisecond)
				msg := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_subscription","params":{"subscription":"0x00","result":%s}}`, notify)
				t.Log("Sending message", msg)
				err = conn.WriteMessage(websocket.BinaryMessage, []byte(msg))
				if err != nil {
					t.Logf("Failed to write message: %v", err)
					return
				}
			}
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	require.NoError(t, err, "Failed to parse url")
	u.Scheme = "ws"

	return u
}
