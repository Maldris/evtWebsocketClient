package evtWebsocketClient

import (
	"errors"
	"time"

	"github.com/gobwas/ws/wsutil"
)

// Msg is the message structure.
type Msg struct {
	Body     []byte
	Callback func(Msg, *Conn)
	Params   map[string]interface{}
}

type msgOperation struct {
	add bool
	msg *Msg
	pos int
}

func (c *Conn) onMsg(pkt []byte) {
	msg := Msg{
		Body: pkt,
	}
	if c.MsgPrep != nil {
		c.MsgPrep(&msg)
	}
	var calledBack = false
	if c.MatchMsg != nil {
		for i, m := range c.msgQueue {
			if m.Callback != nil && c.MatchMsg(msg, m) {
				go m.Callback(msg, c)
				c.addToQueue <- msgOperation{
					add: false,
					pos: i,
					msg: &m,
				}
				calledBack = true
				break
			}
		}
	}
	// Fire OnMessage every message that hasnt already been handled in a callback
	if c.OnMessage != nil && !calledBack {
		go c.OnMessage(msg, c)
	}
}

func (c *Conn) onError(err error) {
	if c.OnError != nil {
		c.OnError(err)
	}
	c.close()
}

func (c *Conn) setupPing() {
	if c.PingIntervalSecs > 0 && (c.ComposePingMessage != nil || len(c.PingMsg) > 0) {
		if c.CountPongs && c.OnMessage == nil {
			c.CountPongs = false
		}
		c.pingTimer = time.Now().Add(time.Second * time.Duration(c.PingIntervalSecs))
		go func() {
			for {
				if !time.Now().After(c.pingTimer) {
					time.Sleep(time.Millisecond * 100)
					continue
				}
				var msg []byte
				if c.ComposePingMessage != nil {
					msg = c.ComposePingMessage()
				} else {
					msg = c.PingMsg
				}
				if err := c.Send(Msg{
					Body:     msg,
					Callback: nil,
					Params:   nil,
				}); err != nil {
					c.onError(errors.New("Error sending ping: " + err.Error()))
					return
				}
				if c.CountPongs {
					c.pingCount++
				}
				if c.pingCount > c.UnreceivedPingThreshold+1 {
					c.onError(errors.New("too many pings not responded too"))
					return
				}
				c.pingTimer = time.Now().Add(time.Second * time.Duration(c.PingIntervalSecs))
			}
		}()
	}
}

// PongReceived notify the socket that a ping response (pong) was received, this is left to the user as the message structure can differ between servers
func (c *Conn) PongReceived() {
	c.pingCount--
}

// IsConnected tells wether the connection is
// opened or closed.
func (c *Conn) IsConnected() bool {
	return !c.closed
}

// Send sends a message through the connection.
func (c *Conn) Send(msg Msg) error {
	if msg.Body == nil {
		return errors.New("No message body")
	}
	if c.closed {
		return errors.New("closed connection")
	}
	if msg.Callback != nil && c.addToQueue != nil {
		c.addToQueue <- msgOperation{
			add: true,
			pos: -1,
			msg: &msg,
		}
	}
	c.write(msg.Body)
	return nil
}

// RemoveFromQueue unregisters a callback from the queue in the event it has timed out
func (c *Conn) RemoveFromQueue(msg Msg) error {
	if c.closed {
		return errors.New("closed connection")
	}
	c.addToQueue <- msgOperation{
		add: false,
		pos: -1,
		msg: &msg,
	}
	return nil
}

func (c *Conn) read() bool {
	_, ok := <-c.readerAvailable
	if !ok {
		return false
	}
	pkt, _, err := wsutil.ReadServerData(c.ws)
	if err != nil {
		c.onError(err)
		return false
	}
	c.readerAvailable <- struct{}{}
	go c.onMsg(pkt)
	return true
}

func (c *Conn) write(pkt []byte) {
	_, ok := <-c.writerAvailable
	if !ok {
		return
	}
	err := wsutil.WriteClientText(c.ws, pkt)
	if err != nil {
		c.onError(err)
		return
	}
	c.writerAvailable <- struct{}{}
}
