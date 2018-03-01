package evtWebsocketClient

import (
	"context"
	"net"
	"time"

	"github.com/gobwas/ws"
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
	msgQueue    []Msg
	addToQueue  chan msgOperation

	PingMsg                 []byte
	ComposePingMessage      func() []byte
	PingIntervalSecs        int
	CountPongs              bool
	UnreceivedPingThreshold int
	pingCount               int
	pingTimer               time.Time

	writerAvailable chan struct{}
	readerAvailable chan struct{}
}

// Dial sets up the connection with the remote
// host provided in the url parameter.
// Note that all the parameters of the structure
// must have been set before calling it.
func (c *Conn) Dial(url string) error {
	c.closed = true
	c.url = url
	if c.msgQueue == nil {
		c.msgQueue = []Msg{}
	}
	c.readerAvailable = make(chan struct{}, 1)
	c.writerAvailable = make(chan struct{}, 1)
	c.pingCount = 0

	var err error
	c.ws, _, _, err = ws.Dial(context.Background(), url)
	if err != nil {
		return err
	}
	c.closed = false
	if c.OnConnected != nil {
		go c.OnConnected(c)
	}

	// setup reader
	go func() {
		for {
			if !c.read() {
				return
			}
		}
	}()

	// setup write channel
	c.addToQueue = make(chan msgOperation, 100)

	// start que manager
	go func() {
		for msg := range c.addToQueue {
			if msg.pos == 0 && msg.msg == nil {
				return
			}
			if msg.add {
				c.msgQueue = append(c.msgQueue, *msg.msg)
			} else {
				if msg.pos >= 0 {
					c.msgQueue = append(c.msgQueue[:msg.pos], c.msgQueue[msg.pos+1:]...)
				} else if c.MatchMsg != nil {
					for i, m := range c.msgQueue {
						if c.MatchMsg(m, *msg.msg) {
							// Delete this element from the queue
							c.msgQueue = append(c.msgQueue[:i], c.msgQueue[i+1:]...)
							break
						}
					}
				}
			}
		}
	}()

	c.setupPing()

	c.readerAvailable <- struct{}{}
	c.writerAvailable <- struct{}{}

	// resend dropped messages if this is a reconnect
	if len(c.msgQueue) > 0 {
		for _, msg := range c.msgQueue {
			go c.write(ws.OpText, msg.Body)
		}
	}

	return nil
}

func (c *Conn) close() {
	if c.closed {
		return
	}
	c.closed = true
	c.sendCloseFrame()
	close(c.readerAvailable)
	close(c.writerAvailable)
	close(c.addToQueue)
	c.addToQueue = nil
	c.ws.Close()

	if c.Reconnect {
		for {
			if err := c.Dial(c.url); err == nil {
				break
			}
			time.Sleep(time.Second * 1)
		}
	}
}

// Disconnect sends a close frame and disconnects from the server
func (c *Conn) Disconnect() {
	c.close()
}
