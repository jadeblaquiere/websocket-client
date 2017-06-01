// The MIT License (MIT)
//
// Copyright (c) 2017, Joseph deBlaquiere <jadeblaquiere@yahoo.com>
// Copyright (c) 2016-2017 Gerasimos Maropoulos
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package wsclient

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/gorilla/websocket"
	iwebsocket "github.com/kataras/iris/adapters/websocket"
)

type (
	// DisconnectFunc is the callback which fires when a client/connection closed
	DisconnectFunc func()
	// ErrorFunc is the callback which fires when an error happens
	ErrorFunc (func(string))
	// NativeMessageFunc is the callback for native websocket messages, receives one []byte parameter which is the raw client's message
	NativeMessageFunc func([]byte)
	// MessageFunc is the second argument to the Emitter's Emit functions.
	// A callback which should receives one parameter of type string, int, bool or any valid JSON/Go struct
	MessageFunc interface{}
)

//Client presents a subset of iris.websocket.Connection interface
type Client struct {
	conn                     *websocket.Connection
	config                   iwebsocket.Config
	wchan                    chan []byte
	pchan                    chan []byte
	onDisconnectListeners    []DisconnectFunc
	onErrorListeners         []ErrorFunc
	onNativeMessageListeners []NativeMessageFunc
	onEventListeners         map[string][]MessageFunc
}

func (c *Client) readPump() {
	defer c.conn.Close()
	c.conn.SetReadLimit(c.config.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
	c.conn.SetPongHandler(func(s string) error {
		fmt.Printf("received PONG from %s\n", c.conn.UnderlyingConn().RemoteAddr().String())
		c.conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
		return nil
	})
	c.conn.SetPingHandler(func(s string) error {
		fmt.Printf("received PING (%s) from %s\n", s, con.UnderlyingConn().RemoteAddr().String())
		c.conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
		c.pchan <- []byte(s)
		return nil
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			fmt.Println("panic @ ", time.Now().Format("2006-01-02 15:04:05.000000"))
			panic(err)
		}
		c.conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))

		fmt.Println("recv:", string(message), "@", time.Now().Format("2006-01-02 15:04:05.000000"))

		c.messageReceived(message)
	}
}

// messageReceived comes straight from iris/adapters/websocket/connection.go
// messageReceived checks the incoming message and fire the nativeMessage listeners or the event listeners (ws custom message)
func (c *Client) messageReceived(data []byte) {

	if bytes.HasPrefix(data, websocketMessagePrefixBytes) {
		customData := string(data)
		//it's a custom ws message
		receivedEvt := getWebsocketCustomEvent(customData)
		listeners := c.onEventListeners[receivedEvt]
		if listeners == nil { // if not listeners for this event exit from here
			return
		}
		customMessage, err := websocketMessageDeserialize(receivedEvt, customData)
		if customMessage == nil || err != nil {
			return
		}

		for i := range listeners {
			if fn, ok := listeners[i].(func()); ok { // its a simple func(){} callback
				fn()
			} else if fnString, ok := listeners[i].(func(string)); ok {

				if msgString, is := customMessage.(string); is {
					fnString(msgString)
				} else if msgInt, is := customMessage.(int); is {
					// here if server side waiting for string but client side sent an int, just convert this int to a string
					fnString(strconv.Itoa(msgInt))
				}

			} else if fnInt, ok := listeners[i].(func(int)); ok {
				fnInt(customMessage.(int))
			} else if fnBool, ok := listeners[i].(func(bool)); ok {
				fnBool(customMessage.(bool))
			} else if fnBytes, ok := listeners[i].(func([]byte)); ok {
				fnBytes(customMessage.([]byte))
			} else {
				listeners[i].(func(interface{}))(customMessage)
			}

		}
	} else {
		// it's native websocket message
		for i := range c.onNativeMessageListeners {
			c.onNativeMessageListeners[i](data)
		}
	}

}

func (c *Client) writePump() {
	pingtimer := time.NewTicker(c.config.PingPeriod)
	defer c.conn.Close()
	c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
	for {
		select {
		case wmsg := <-c.wchan:
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(wmsg)

			if err := w.Close(); err != nil {
				return
			}

		case pmsg := <-c.pchan:
			c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
			fmt.Printf("sending PONG to %s\n", c.conn.UnderlyingConn().RemoteAddr().String())
			if err := c.conn.WriteControl(websocket.PongMessage, pmsg, time.Now().Add(c.config.WriteTimeout)); err != nil {
				return
			}

		case <-pingtimer.C:
			c.con.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
			fmt.Printf("sending PING to %s\n", c.conn.UnderlyingConn().RemoteAddr().String())
			if err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(c.config.WriteTimeout)); err != nil {
				return
			}
		}
	}
}

func (c *Client) EmitMessage(nativeMessage []byte) error {
	c.wchan <- nativeMessage
	return nil
}

func (c *Client) Emit(event string, data interface{}) error {
	message, err := websocketMessageSerialize(event, data)
	if err != nil {
		return err
	}
	e.EmitMessage([]byte(message))
	return nil
}

//func (c *Client) OnDisconnect(f DisconnectFunc) {
//
//}

//func (c *Client) OnError(f ErrorFunc) {
//
//}

func (c *Client) OnMessage(f NativeMessageFunc) {
	c.onNativeMessageListeners = append(c.onNativeMessageListeners, f)
}

func (c *Client) On(event string, f MessageFunc) {
	if c.onEventListeners[event] == nil {
		c.onEventListeners[event] = make([]MessageFunc, 0)
	}

	c.onEventListeners[event] = append(c.onEventListeners[event], cb)
}

func (c *Client) Disconnect() error {
	return nil
}

// WSDialer here is a shameless wrapper around gorilla.websocket.Dialer
// which returns a wsclient.Client instead of the gorilla Connection on Dial()
type WSDialer struct {
	// NetDial specifies the dial function for creating TCP connections. If
	// NetDial is nil, net.Dial is used.
	NetDial func(network, addr string) (net.Conn, error)

	// Proxy specifies a function to return a proxy for a given
	// Request. If the function returns a non-nil error, the
	// request is aborted with the provided error.
	// If Proxy is nil or returns a nil *URL, no proxy is used.
	Proxy func(*http.Request) (*url.URL, error)

	// TLSClientConfig specifies the TLS configuration to use with tls.Client.
	// If nil, the default configuration is used.
	TLSClientConfig *tls.Config

	// HandshakeTimeout specifies the duration for the handshake to complete.
	HandshakeTimeout time.Duration

	// ReadBufferSize and WriteBufferSize specify I/O buffer sizes. If a buffer
	// size is zero, then a useful default size is used. The I/O buffer sizes
	// do not limit the size of the messages that can be sent or received.
	ReadBufferSize, WriteBufferSize int

	// Subprotocols specifies the client's requested subprotocols.
	Subprotocols []string

	// EnableCompression specifies if the client should attempt to negotiate
	// per message compression (RFC 7692). Setting this value to true does not
	// guarantee that compression will be supported. Currently only "no context
	// takeover" modes are supported.
	EnableCompression bool

	// Jar specifies the cookie jar.
	// If Jar is nil, cookies are not sent in requests and ignored
	// in responses.
	Jar http.CookieJar

	dialer *websocket.Dialer
}

func (wsd *WSDialer) Dial(urlStr string, requestHeader http.Header, config iwebsocket.Config) (*Client, *http.Response, error) {
	if wsd.dialer == nil {
		wsd.dialer = New(websocket.Dialer)
	}
	wsd.dialer.NetDial = wsd.NetDial
	wsd.dialer.Proxy = wsd.Proxy
	wsd.dialer.TLSClientConfig = wsd.TLSClientConfig
	wsd.dialer.HandshakeTimeout = wsd.HandshakeTimeout
	wsd.dialer.ReadBufferSize = wsd.ReadBufferSize
	wsd.dialer.WriteBufferSize = wsd.WriteBufferSize
	wsd.dialer.Subprotocols = wsd.Subprotocols
	wsd.dialer.EnableCompression = wsd.EnableCompression
	wsd.dialer.Jar = wsd.Jar
	conn, response, err = wsd.dialer.Dial(urlStr, requestHeader)
	if err != nil {
		return nil, response, err
	}
	c := New(Client)
	c.conn = conn
	c.config = config
	c.config.Validate()

	go c.writePump()
	go c.readPump()

	return c, response, nil
}
