// Package websocket integrates websockets with capnproto.
package websocket

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc/transport"
	"github.com/gorilla/websocket"
)

// ServeCodec performs a websocket handshake and returns a transport.Codec,
// which sends each capnp in its own websocket binary message.
func ServeCodec(w http.ResponseWriter, req *http.Request) (transport.Codec, error) {
	conn, err := (&websocket.Upgrader{}).Upgrade(w, req, http.Header{})
	if err != nil {
		return nil, fmt.Errorf("Wpgrading to websocket protocol: %w", err)
	}
	return websocketCodec{conn: conn}, nil
}

// DialCodec opens a websocket client connection and returns a transport.Codec,
// which sends each capnp in its own websocket binary message.
func DialCodec(ctx context.Context, urlStr string, reqHeader http.Header) (transport.Codec, *http.Response, error) {
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, urlStr, reqHeader)
	return websocketCodec{conn: conn}, resp, err
}

type websocketCodec struct {
	conn *websocket.Conn
}

func (c websocketCodec) Decode(ctx context.Context) (*capnp.Message, error) {
	typ, wsMsg, err := c.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("Reading websocket message: %w", err)
	}
	if typ != websocket.BinaryMessage {
		return nil, fmt.Errorf("Unexpected websocket message type: %w", typ)
	}
	return capnp.Unmarshal(wsMsg)
}

func (c websocketCodec) Encode(ctx context.Context, msg *capnp.Message) error {
	w, err := c.conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return err
	}
	defer w.Close()
	return capnp.NewEncoder(w).Encode(msg)
}

func (c websocketCodec) SetPartialWriteTimeout(dur time.Duration) {
	// TODO. We should factor out the logic around ctxReader in the
	// transport pacakge so we can re-use it here.
}

func (c websocketCodec) Close() error {
	return c.conn.Close()
}
