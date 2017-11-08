# evtWebsocketClient
An Event driven golang websocket client, meant for websocket events that need callbacks

Built very quickly in a few hours because I needed some specific functionality
Heavily inspired by [rgamba/evtwebsocket](https://github.com/rgamba/evtwebsocket)

## Remaining TODO
 - improve documentation
 - add tests
 - more rigorous load and rate tests to find any remaining issues, or concurrency issues

## Usage

First create an object and provide all the configuration needed
```
```

Then call dial, passing the url to dial
```
```

Send messages with Send
```
```

## Using callbacks
### Basic
ensure you have MatchMsg declared on your connection object during config (before calling dial)
```
```

Note: declaring MsgPrep can be useful if you need to do any decoding on the message, allowing you to do so once, instead of repeatedly in each match call (use params to persist the data)
```
```

declare your message with the callback referenced in the message
```
```

send the message and the response will be sent to MsgPrep (if declared), it will check MatchMsg for each callback logged with the system, calling the matching message

### Using Timeouts
If you want messages to have a timeout waiting for a response, handle this externally with something like `time.After(time.Duration)`
But when you do this, make sure to remove the message from the callback waiting queue (very important if your method for testing message matches is in any way cyclical)
```
```

## API

### Types
**Conn**

**Msg**

### Methods
**(Conn)Dial**

**(Conn)PongReceived**

**(Conn)IsConnected**

**(Conn)Send**

**(Conn)RemoveFromQueue**
