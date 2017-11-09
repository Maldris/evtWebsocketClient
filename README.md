# evtWebsocketClient
An Event driven golang websocket client, meant for websocket events that need callbacks

Built very quickly in a few hours because I needed some specific functionality
Heavily inspired by [rgamba/evtwebsocket](https://github.com/rgamba/evtwebsocket)

## Remaining TODO
 - add tests
 - more rigorous load and rate tests to find any remaining issues, or concurrency issues

## Usage

First create an object and provide all the configuration needed
```
  conn := evtWebsocketClient.Conn{
		// Fires when the connection is established
		OnConnected: func(w *evtWebsocketClient.Conn) {
			log.Println("Connected to server")
		},
		// Fires when a new message arrives from the server
		OnMessage: func(msg evtWebsocketClient.Msg, w *evtWebsocketClient.Conn) {
			log.Println("Message Received: '", string(msg.Body), "'")
		},
		// Fires when an error occurs and connection is closed
		OnError: func(err error) {
			log.Println("Websocket Error: ", err)
		},
		// Reconnect true will make the client auto reconnect on disconnect
		Reconnect: true,
	}
```

Then call dial, passing the url to dial
```
  err := conn.Dial(url)
  check(err)
```

Send messages with Send
```
  conn.Send(evtWebsocketClient.Msg{
    Body: []byte("{"ID":1, "Body":"test message"}"),
  })
```

## Using callbacks
### Basic
ensure you have MatchMsg declared on your connection object during config (before calling dial)
```
conn := evtWebsocketClient.Conn{
	...
	MatchMsg: func(req, resp evtWebsocketClient.Msg) bool {
		return req.Params["MsgID"] == resp.Params["MsgID"] && req.Params["Type"] == resp.Params["Type"]
	},
}
```

Note: declaring MsgPrep can be useful if you need to do any decoding on the message, allowing you to do so once, instead of repeatedly in each match call (use params to persist the data)
```
conn := evtWebsocketClient.Conn{
	...
	MsgPrep: func(msg *evtWebsocketClient.Msg) {
		err := json.Unmarshal(msg.Body, &msg.Params)
		check(err)
	},
}
```

declare your message with the callback referenced in the message (storing details in params can help simplify your MatchMsg logic)
```
conn.Send(evtWebsocketClient.Msg{
	Body: []byte("{"MsgID":1, "Type":"test", "Body":"test message"}"),
	Params: map[string]interface{}{"MsgID": 1, "Type": "test"},
	Callback: func(resp evtWebsocketClient.Msg, conn *evtWebsocketClient.Conn) {
		log.Println("YAY Callback to test")
	},
})
```

send the message and the response will be sent to MsgPrep (if declared), it will check MatchMsg for each callback logged with the system, calling the matching message

### Using Timeouts
If you want messages to have a timeout waiting for a response, handle this externally with something like `time.After(time.Duration)`
But when you do this, make sure to remove the message from the callback waiting queue (very important if your method for testing message matches is in any way cyclical)
```
ch := make(chan string, 1)
msg := evtWebsocketClient.Msg{
	Body: []byte("{"MsgID":1, "Type":"test", "Body":"test message"}"),
	Params: map[string]interface{}{"MsgID": 1, "Type": "test"},
	Callback: func(resp evtWebsocketClient.Msg, conn *evtWebsocketClient.Conn) {
		log.Println("YAY Callback to test")
		ch <- string(resp.Body)
	},
}
conn.Send(msg)

var res string
select {
	case res = <- ch:
	case <-time.After(time.Second * 10):
		err = connection.RemoveFromQueue(msg)
		if err != nil {
			log.Println("Error removing test from queue: ", err)
		} else {
			close(ch)
		}
		return res, errors.New("timeout waiting for test")
}
return res, nil
```

## API

### Types
**Conn**

  OnMessage   func(Msg, *Conn)

    Callback that will receive any messages not handled by callbacks

  OnError     func(error)

    Callback that will receive any errors that happen internal to the client

  OnConnected func(*Conn)

    Callback to be notified when the connection to the server is established or reestablished

  MatchMsg    func(request Msg, response Msg) bool

    Custom test to determine if a response matches a logged message with callback

  Reconnect   bool

    If true, the client will automatically attempt to reconnect to the server after an error

  MsgPrep     func(*Msg)

    Callback that can be used to pre-parse a response and store details about it to avoid repeatedly parsing the same response in MatchMsg

  PingMsg                 []byte

    Message to send in a ping frame (fallback, not used if ComposePingMessage defined)

  ComposePingMessage      func() []byte

    Callback function that can be used to build a custom ping frame payload if it needs to be dynamic

  PingIntervalSecs        int

    Frequency (in seconds) to send ping frames (a positive non zero interval and either PingMsg or ComposePingMessage must be defined for pings to be sent)

  CountPongs              bool

    Whether to check for un-responded ping messages

  UnreceivedPingThreshold int

    If checking for un-responded ping's, the number after which an error will be thrown and the connection closed (and reconnect attempted if Reconnect is true)

**Msg**

  Body     []byte

    Message body that will be transmitted to the server

  Callback func(Msg, *Conn)

    Function to call back to when a response is received (subject to MatchMsg)

  Params   map[string]interface{}

    Parameter storage, stores arbitrary data on the message that is not transmitted, useful for MatchMsg, or to persist information into the callback

### Methods
**(Conn)Dia(url string) error**

Dial takes a URL and will attempt to connect at the provided URL, all connection config must be done before calling this method

**(Conn)PongReceived()**

PongReceived is used to log when a pong is received, used with the ping fields declared on Conn, this is left to your implementation as you may need additional processing or logging

**(Conn)IsConnected() bool**

IsConnected will tell you if the socket is currently connected, its largely intended for debug and logging, as it is not fully channel safe if the connection closes while calling this

**(Conn)Send(msg Msg) error**

Send attempts to write the the provided message's body to the connection, if a callback is defined it will also add it to the message queue to test against future messages received

**(Conn)RemoveFromQueue(msg Msg) error**

RemoveFromQueue will request a message be removed from the message queue awaiting callbacks (used if implementing some type of request timeout)
