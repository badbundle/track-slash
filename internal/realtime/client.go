package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/coder/websocket"
)

const (
	sendBuffer       = 64
	writeTimeout     = 5 * time.Second
	readMessageLimit = 1 << 14 // 16 KiB; control frames are tiny
)

// Client wraps a single WebSocket connection. The read pump handles
// subscribe/unsubscribe control frames; the write pump drains events
// that the Hub publishes into the bounded send channel.
type Client struct {
	conn      *websocket.Conn
	hub       *Hub
	authorize TopicAuthorizer
	send      chan Event
}

func newClient(conn *websocket.Conn, hub *Hub, authorize TopicAuthorizer) *Client {
	conn.SetReadLimit(readMessageLimit)
	return &Client{
		conn:      conn,
		hub:       hub,
		authorize: authorize,
		send:      make(chan Event, sendBuffer),
	}
}

type controlMsg struct {
	Action string `json:"action"`
	Topic  string `json:"topic"`
}

type serverError struct {
	Error string `json:"error"`
}

// run blocks until either pump exits, then tears down. It is called
// inline from the HTTP handler so the request goroutine stays alive
// for the duration of the WS connection.
func (c *Client) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	defer c.hub.Remove(c)
	defer func() { _ = c.conn.CloseNow() }()

	go c.writePump(ctx, cancel)
	c.readPump(ctx)
}

func (c *Client) readPump(ctx context.Context) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		var msg controlMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			c.writeError(ctx, "invalid json")
			continue
		}
		kind, id, terr := ParseTopic(msg.Topic)
		if terr != nil {
			c.writeError(ctx, terr.Error())
			continue
		}
		switch msg.Action {
		case "subscribe":
			if c.authorize != nil {
				if err := c.authorize(ctx, kind, id); err != nil {
					c.writeError(ctx, "forbidden")
					continue
				}
			}
			c.hub.Subscribe(c, msg.Topic)
		case "unsubscribe":
			c.hub.Unsubscribe(c, msg.Topic)
		default:
			c.writeError(ctx, `unknown action; want "subscribe" or "unsubscribe"`)
		}
	}
}

func (c *Client) writePump(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.writeJSON(ctx, ev); err != nil {
				return
			}
		case <-pingTicker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				return
			}
		}
	}
}

func (c *Client) writeJSON(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := c.conn.Write(wctx, websocket.MessageText, data); err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("realtime client write: %v", err)
		}
		return err
	}
	return nil
}

func (c *Client) writeError(ctx context.Context, msg string) {
	_ = c.writeJSON(ctx, serverError{Error: msg})
}
