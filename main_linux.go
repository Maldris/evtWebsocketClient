package evtWebsocketClient

import (
	"context"
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
	c.ws, _, _, err = ws.Dial(context.Background(), url)
	if err != nil {
		return err
	}
	c.closed = false
	if c.OnConnected != nil {
		go c.OnConnected(c)
	}

	// setup reader
	c.pollerDesc, err = netpoll.Handle(c.ws, netpoll.EventRead|netpoll.EventEdgeTriggered|netpoll.EventHup|netpoll.EventReadHup|netpoll.EventWriteHup|netpoll.EventErr)
	if err != nil {
		return err
	}

	c.poller.Start(c.pollerDesc, func(evt netpoll.Event) {
		if !c.closed {
			if evt == netpoll.EventRead || evt == netpoll.EventEdgeTriggered {
				go c.read()
			} else {
				go c.close()
			}
		}
	})

	// setup write channels
	c.addToQueue = make(chan msgOperation) // , 100

	// start que manager
	go func() {
		for {
			msg, ok := <-c.addToQueue
			if !ok {
				return
			}
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
	c.poller.Stop(c.pollerDesc)
	c.pollerDesc.Close()
	c.poller = nil
	c.pollerDesc = nil
	close(c.readerAvailable)
	for _, ok := <-c.readerAvailable; ok; _, ok = <-c.readerAvailable {
	}
	close(c.writerAvailable)
	for _, ok := <-c.writerAvailable; ok; _, ok = <-c.writerAvailable {
	}
	close(c.addToQueue)
	for _, ok := <-c.addToQueue; ok; _, ok = <-c.addToQueue {
	}
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
