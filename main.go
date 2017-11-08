package evtWebsocketClient

import (
	"errors"
	"time"
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
		for i, m := range c.MsgQueue {
			if m.Callback != nil && c.MatchMsg(msg, m) {
				go m.Callback(msg, c)
				// Delete this element from the queue
				// c.MsgQueue = append(c.MsgQueue[:i], c.MsgQueue[i+1:]...)
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

func (c *Conn) close() {
	if c.closed {
		return
	}
	c.closed = true
	c.ws.Close()
	close(c.sendQueue)
	c.sendQueue = nil
	close(c.addToQueue)
	c.addToQueue = nil
	if c.readEvents != nil {
		close(c.readEvents)
		c.readEvents = nil
	}
	c.writer = nil
	c.reader = nil

	if c.Reconnect {
		for {
			if err := c.Dial(c.url); err == nil {
				break
			}
			time.Sleep(time.Second * 1)
		}
	}
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
	if c.closed || c.writer == nil {
		return errors.New("closed connection")
	}
	if msg.Callback != nil {
		c.addToQueue <- msgOperation{
			add: true,
			pos: -1,
			msg: &msg,
		}
	}
	c.sendQueue <- msg
	return nil
}

// RemoveFromQueue unregisters a callback from the queue in the event it has timed out
func (c *Conn) RemoveFromQueue(msg Msg) error {
	if c.closed || c.writer == nil {
		return errors.New("closed connection")
	}
	c.addToQueue <- msgOperation{
		add: false,
		pos: -1,
		msg: &msg,
	}
	return nil
}
