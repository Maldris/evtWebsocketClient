package evtWebsocketClient

import (
	"errors"
	"fmt"
	"time"

	"github.com/gobwas/ws"
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
		queue := make([]Msg, len(c.msgQueue))
		copy(queue, c.msgQueue)
		for _, m := range queue {
			if m.Callback != nil && c.MatchMsg(msg, m) {
				go m.Callback(msg, c)
				c.addToQueue <- msgOperation{
					add: false,
					pos: -1, // i,
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
				if c.closed {
					return
				}
				var msg []byte
				if c.ComposePingMessage != nil {
					msg = c.ComposePingMessage()
				} else {
					msg = c.PingMsg
				}
				if len(msg) > 0 {
					c.write(ws.OpText, msg)
				}
				c.write(ws.OpPing, []byte(``))
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
	} else {
		c.write(ws.OpText, msg.Body)
	}
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
	m := []wsutil.Message{}
	m, err := wsutil.ReadServerMessage(c.ws, m)
	if err != nil {
		c.onError(err)
		return false
	}
	for _, msg := range m {
		switch msg.OpCode {
		case ws.OpText:
			go c.onMsg(msg.Payload)
		case ws.OpBinary:
			go c.onMsg(msg.Payload)
		case ws.OpPing:
			go c.write(ws.OpPong, nil)
		case ws.OpPong:
			go c.PongReceived()
		case ws.OpClose:
			c.close()
			return true
		}

	}
	c.readerAvailable <- struct{}{}

	return true
}

func (c *Conn) write(opcode ws.OpCode, pkt []byte) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%v Recovered from write error %v", time.Now(), r)
		}
	}()
	_, ok := <-c.writerAvailable
	if !ok {
		return
	}
	err := wsutil.WriteClientMessage(c.ws, opcode, pkt)
	if err != nil {
		c.onError(err)
		return
	}
	c.writerAvailable <- struct{}{}
}

func (c *Conn) sendCloseFrame() {
	c.write(ws.OpClose, []byte(``))
}
