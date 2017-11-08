package evtWebsocketClient

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// Conn is the connection structure.
type Conn struct {
	OnMessage   func(Msg, *Conn)
	OnError     func(error)
	OnConnected func(*Conn)
	MatchMsg    func(Msg, Msg) bool
	Reconnect   bool
	MsgPrep     func(*Msg)
	ws          net.Conn
	url         string
	closed      bool
	MsgQueue    []Msg
	sendQueue   chan Msg
	addToQueue  chan msgOperation

	PingMsg                 []byte
	ComposePingMessage      func() []byte
	PingIntervalSecs        int
	CountPongs              bool
	UnreceivedPingThreshold int
	pingCount               int
	pingTimer               time.Time

	reader     func()
	writer     func()
	readEvents chan struct{}
}

// Dial sets up the connection with the remote
// host provided in the url parameter.
// Note that all the parameters of the structure
// must have been set before calling it.
func (c *Conn) Dial(url string) error {
	c.closed = true
	c.url = url
	if c.MsgQueue == nil {
		c.MsgQueue = []Msg{}
	}
	c.pingCount = 0

	var err error
	var resp ws.Response
	// c.ws, err = websocket.Dial(url, subprotocol, "http://localhost/")
	c.ws, resp, err = ws.Dial(context.Background(), url, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != 101 {
		log.Print("Error dialing server, status code: ", resp.StatusCode, ", message:", resp.Status)
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	c.closed = false
	if c.OnConnected != nil {
		go c.OnConnected(c)
	}

	// setup reader
	if c.reader == nil {
		c.reader = func() {
			for {
				pkt, _, err := wsutil.ReadServerData(c.ws)
				if err != nil {
					c.onError(err)
					return
				}
				c.onMsg(pkt)
			}
		}
		go c.reader()
	}

	// setup write channel
	if c.writer == nil {
		c.sendQueue = make(chan Msg, 100)
		c.addToQueue = make(chan msgOperation, 100)
		c.writer = func() {
			for msg := range c.sendQueue {
				if msg.Body == nil {
					return
				}
				err := wsutil.WriteClientText(c.ws, msg.Body)
				if err != nil {
					c.onError(err)
					return
				}
			}
		}
		go c.writer()
	}

	// start que manager
	go func() {
		for msg := range c.addToQueue {
			if msg.pos == 0 && msg.msg == nil {
				return
			}
			if msg.add {
				c.MsgQueue = append(c.MsgQueue, *msg.msg)
			} else {
				if msg.pos >= 0 {
					c.MsgQueue = append(c.MsgQueue[:msg.pos], c.MsgQueue[msg.pos+1:]...)
				} else if c.MatchMsg != nil {
					for i, m := range c.MsgQueue {
						if c.MatchMsg(m, *msg.msg) {
							// Delete this element from the queue
							c.MsgQueue = append(c.MsgQueue[:i], c.MsgQueue[i+1:]...)
							break
						}
					}
				}
			}
		}
	}()

	c.setupPing()

	// resend dropped messages if this is a reconnect
	if len(c.MsgQueue) > 0 {
		for _, msg := range c.MsgQueue {
			c.sendQueue <- msg
		}
	}

	return nil
}
