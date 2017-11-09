package evtWebsocketClient

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/gobwas/ws"
	"github.com/mailru/easygo/netpoll"
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

	poller     netpoll.Poller
	pollerDesc *netpoll.Desc

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
	c.poller, err = netpoll.New(nil)
	if err != nil {
		return err
	}
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

	c.pollerDesc, err = netpoll.HandleRead(c.ws)
	// setup reader
	if err != nil {
		return err
	}

	c.poller.Start(c.pollerDesc, func(evt netpoll.Event) {
		if !c.closed {
			go c.read()
		}
	})

	// setup write channels
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
			go c.write(msg.Body)
		}
	}

	return nil
}

func (c *Conn) close() {
	if c.closed {
		return
	}
	c.closed = true
	c.poller.Stop(c.pollerDesc)
	c.pollerDesc.Close()
	c.poller = nil
	c.pollerDesc = nil
	c.ws.Close()
	close(c.readerAvailable)
	close(c.writerAvailable)
	close(c.addToQueue)
	c.addToQueue = nil

	if c.Reconnect {
		for {
			if err := c.Dial(c.url); err == nil {
				break
			}
			time.Sleep(time.Second * 1)
		}
	}
}
