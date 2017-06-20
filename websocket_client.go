// Copyright (c) 2016-2017 Gerasimos Maropoulos
// Copyright (c) 2017, Joseph deBlaquiere <jadeblaquiere@yahoo.com>
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
// * Redistributions of source code must retain the above copyright notice, this
//   list of conditions and the following disclaimer.
//
// * Redistributions in binary form must reproduce the above copyright notice,
//   this list of conditions and the following disclaimer in the documentation
//   and/or other materials provided with the distribution.
//
// * Neither the name of ciphrtxt nor the names of its
//   contributors may be used to endorse or promote products derived from
//   this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
// SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
// CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
// OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package websocket_client

import (
	"bytes"
	"crypto/tls"
	// "fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	gwebsocket "github.com/gorilla/websocket"
	//iwebsocket "gopkg.in/kataras/iris.v7/websocket"
	iwebsocket "github.com/kataras/iris/websocket"
)

// Client presents a subset of iris.websocket.Connection interface to support
// client-initiated connections using the Iris websocket message protocol
type client struct {
	conn                     *gwebsocket.Conn
	config                   iwebsocket.Config
	wAbort                   chan bool
	wchan                    chan []byte
	pchan                    chan []byte
	onDisconnectListeners    []iwebsocket.DisconnectFunc
	onErrorListeners         []iwebsocket.ErrorFunc
	onNativeMessageListeners []iwebsocket.NativeMessageFunc
	onEventListeners         map[string][]iwebsocket.MessageFunc
	connected                bool
	dMutex                   sync.Mutex
}

// ClientConnection defines proper subset of Connection interface which is
// satisfied by Client
type ClientConnection interface {
	// EmitMessage sends a native websocket message
	EmitMessage([]byte) error
	// Emit sends a message on a particular event
	Emit(string, interface{}) error

	// OnDisconnect registers a callback which fires when this connection is closed by an error or manual
	OnDisconnect(iwebsocket.DisconnectFunc)

	// OnMessage registers a callback which fires when native websocket message received
	OnMessage(iwebsocket.NativeMessageFunc)
	// On registers a callback to a particular event which fires when a message to this event received
	On(string, iwebsocket.MessageFunc)
	// Disconnect disconnects the client, close the underline websocket conn and removes it from the conn list
	// returns the error, if any, from the underline connection
	Disconnect() error
}

// in order to ensure all read operations are within a single goroutine
// readPump processes incoming messages and dispatches them to messageReceived
func (c *client) readPump() {
	defer c.conn.Close()
	c.conn.SetReadLimit(c.config.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
	c.conn.SetPongHandler(func(s string) error {
		// fmt.Printf("received PONG from %s\n", c.conn.UnderlyingConn().RemoteAddr().String())
		c.conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
		return nil
	})
	c.conn.SetPingHandler(func(s string) error {
		// fmt.Printf("received PING (%s) from %s\n", s, c.conn.UnderlyingConn().RemoteAddr().String())
		c.conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
		c.pchan <- []byte(s)
		return nil
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			// fmt.Println("disconnect @ ", time.Now().Format("2006-01-02 15:04:05.000000"))
			c.wAbort <- false
			return
		}
		c.conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))

		// fmt.Println("recv:", string(message), "@", time.Now().Format("2006-01-02 15:04:05.000000"))

		go c.messageReceived(message)
	}
}

func (c *client) fireDisconnect() {
	c.dMutex.Lock()
	defer c.dMutex.Unlock()
	if c.connected == false {
		return
	}
	// fmt.Println("fireDisconnect unique")
	for i := range c.onDisconnectListeners {
		c.onDisconnectListeners[i]()
	}
	c.connected = false
}

// messageReceived comes straight from iris/adapters/websocket/connection.go
// messageReceived checks the incoming message and fire the nativeMessage listeners or the event listeners (ws custom message)
func (c *client) messageReceived(data []byte) {

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

// In order to ensure all write operations are within a single goroutine
// writePump handles write operations to the socket serially from channels
func (c *client) writePump() {
	pingtimer := time.NewTicker(c.config.PingPeriod)
	defer c.conn.Close()
	c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
	for {
		select {
		case wmsg := <-c.wchan:
			// fmt.Printf("WP: writing %s\n", string(wmsg))
			w, err := c.conn.NextWriter(gwebsocket.TextMessage)
			if err != nil {
				// fmt.Println("error getting NextWriter")
				c.fireDisconnect()
				return
			}
			w.Write(wmsg)

			if err := w.Close(); err != nil {
				// fmt.Println("error closing NextWriter")
				c.fireDisconnect()
				return
			}

		case pmsg := <-c.pchan:
			c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
			// fmt.Printf("sending PONG to %s\n", c.conn.UnderlyingConn().RemoteAddr().String())
			if err := c.conn.WriteControl(gwebsocket.PongMessage, pmsg, time.Now().Add(c.config.WriteTimeout)); err != nil {
				// fmt.Println("error sending PONG")
				c.fireDisconnect()
				return
			}

		// any write to wAbort aborts writePump
		case sendClose := <-c.wAbort:
			// fmt.Println("wAbort received")
			if sendClose {
				c.conn.WriteControl(gwebsocket.CloseMessage, gwebsocket.FormatCloseMessage(gwebsocket.CloseNormalClosure, ""),
					time.Now().Add(c.config.WriteTimeout))
			}
			c.fireDisconnect()
			return

		case <-pingtimer.C:
			c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
			// fmt.Printf("sending PING to %s\n", c.conn.UnderlyingConn().RemoteAddr().String())
			if err := c.conn.WriteControl(gwebsocket.PingMessage, []byte{}, time.Now().Add(c.config.WriteTimeout)); err != nil {
				// fmt.Println("error sending PING")
				c.fireDisconnect()
				return
			}
		}
	}
}

// EmitMessage sends a native (raw) message to the socket. The server will
// receive this message using the handler specified with OnMessage()
func (c *client) EmitMessage(nativeMessage []byte) error {
	c.wchan <- nativeMessage
	return nil
}

// Emit sends a message to a particular event queue. The server will receive
// these messages via the handler specified using On()
func (c *client) Emit(event string, data interface{}) error {
	message, err := websocketMessageSerialize(event, data)
	if err != nil {
		return err
	}
	// fmt.Printf("Message %s\n", string(message))
	c.wchan <- []byte(message)
	return nil
}

func (c *client) OnDisconnect(f iwebsocket.DisconnectFunc) {
	c.onDisconnectListeners = append(c.onDisconnectListeners, f)
}

//func (c *client) OnError(f ErrorFunc) {
//
//}

// OnMessage designates a listener callback function for raw messages. If
// multiple callback functions are specified, all will be called for each
// message
func (c *client) OnMessage(f iwebsocket.NativeMessageFunc) {
	c.onNativeMessageListeners = append(c.onNativeMessageListeners, f)
}

// On designates a listener callback for a specific event tag.  If multiple
// callback functions are specified, all will be called for each message
func (c *client) On(event string, f iwebsocket.MessageFunc) {
	if c.onEventListeners[event] == nil {
		c.onEventListeners[event] = make([]iwebsocket.MessageFunc, 0)
	}

	c.onEventListeners[event] = append(c.onEventListeners[event], f)
}

func (c *client) Disconnect() error {
	c.wAbort <- true
	return nil
}

// WSDialer here is a wrapper around the gorilla.websocket.Dialer
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

	dialer *gwebsocket.Dialer
}

// Dial initiates a connection to a remote Iris server websocket listener
// using the gorilla websocket Dialer and returns a Client connection
// which can be used to emit and handle messages
func (wsd *WSDialer) Dial(urlStr string, requestHeader http.Header, config iwebsocket.Config) (ClientConnection, *http.Response, error) {
	if wsd.dialer == nil {
		wsd.dialer = new(gwebsocket.Dialer)
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
	conn, response, err := wsd.dialer.Dial(urlStr, requestHeader)
	if err != nil {
		return nil, response, err
	}
	c := new(client)
	c.conn = conn
	c.config = config
	c.wAbort = make(chan bool)
	c.wchan = make(chan []byte)
	c.pchan = make(chan []byte)
	c.onEventListeners = make(map[string][]iwebsocket.MessageFunc)
	c.config.Validate()
	c.connected = true

	go c.writePump()
	go c.readPump()

	return c, response, nil
}

type ConnectCallback func(cc ClientConnection)

type WSHandler struct {
	// HandshakeTimeout specifies the duration for the handshake to complete.
	HandshakeTimeout time.Duration

	// ReadBufferSize and WriteBufferSize specify I/O buffer sizes. If a buffer
	// size is zero, then buffers allocated by the HTTP server are used. The
	// I/O buffer sizes do not limit the size of the messages that can be sent
	// or received.
	ReadBufferSize, WriteBufferSize int

	// Subprotocols specifies the server's supported protocols in order of
	// preference. If this field is set, then the Upgrade method negotiates a
	// subprotocol by selecting the first match in this list with a protocol
	// requested by the client.
	Subprotocols []string

	// Error specifies the function for generating HTTP error responses. If Error
	// is nil, then http.Error is used to generate the HTTP response.
	Error func(w http.ResponseWriter, r *http.Request, status int, reason error)

	// CheckOrigin returns true if the request Origin header is acceptable. If
	// CheckOrigin is nil, the host in the Origin header must not be set or
	// must match the host of the request.
	CheckOrigin func(r *http.Request) bool

	// EnableCompression specify if the server should attempt to negotiate per
	// message compression (RFC 7692). Setting this value to true does not
	// guarantee that compression will be supported. Currently only "no context
	// takeover" modes are supported.
	EnableCompression bool

	Config iwebsocket.Config

	connectCallback ConnectCallback
}

// OnConnect registers a callback function which is called after an incoming
// HTTP(S) request is upgraded to enable Websocket traffic and before message
// traffic handling (send/receive) starts. The
func (wsh *WSHandler) OnConnect(cb ConnectCallback) {
	wsh.connectCallback = cb
}

func (wsh *WSHandler) HandleRequest(w http.ResponseWriter, r *http.Request) {
	u := gwebsocket.Upgrader{
		HandshakeTimeout:  wsh.HandshakeTimeout,
		ReadBufferSize:    wsh.ReadBufferSize,
		WriteBufferSize:   wsh.WriteBufferSize,
		Subprotocols:      wsh.Subprotocols,
		Error:             wsh.Error,
		CheckOrigin:       wsh.CheckOrigin,
		EnableCompression: wsh.EnableCompression,
	}

	conn, err := u.Upgrade(w, r, nil)

	if err != nil {
		return
	}

	c := new(client)
	c.conn = conn
	c.config = wsh.Config
	c.wAbort = make(chan bool)
	c.wchan = make(chan []byte)
	c.pchan = make(chan []byte)
	c.onEventListeners = make(map[string][]iwebsocket.MessageFunc)
	c.config.Validate()
	c.connected = true

	if wsh.connectCallback == nil {
		conn.Close()
		return
	}

	wsh.connectCallback(c)

	go c.readPump()

	c.writePump()
}
