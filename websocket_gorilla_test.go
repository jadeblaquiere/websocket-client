// Copyright 2017 Joseph deBlaquiere. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package websocket_client

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"

	iwebsocket "github.com/kataras/iris/websocket"
)

type gwsServer struct {
	clients   []*wsClient
	listMutex sync.Mutex
	router    *mux.Router
	srv       *http.Server
}

func (gwss *gwsServer) connect(con ClientConnection) {
	gwss.listMutex.Lock()
	defer gwss.listMutex.Unlock()

	// fail compile here if server connection doesn't satisfy interface
	validateInterface(con)
	// c := &wsClient{con: con, wss: wss}
	c := &wsClient{con: con}
	gwss.clients = append(gwss.clients, c)

	// fmt.Printf("Connect # active clients : %d\n", len(wss.clients))

	con.OnMessage(c.echoRawMessage)
	con.On("echo", c.echoString)
	con.On("len", c.lenString)
	con.On("reverse", c.reverseString)
	con.OnDisconnect(c.disconnect)
}

func (gwss *gwsServer) disconnect(wsc *wsClient) {
	gwss.listMutex.Lock()
	defer gwss.listMutex.Unlock()

	l := len(gwss.clients)

	if l == 0 {
		panic("WSS:trying to delete client from empty list")
	}

	for p, v := range gwss.clients {
		if v == wsc {
			gwss.clients[p] = gwss.clients[l-1]
			gwss.clients = gwss.clients[:l-1]
			// fmt.Printf("Disconnect # active clients : %d\n", len(wss.clients))
			return
		}
	}
	panic("WSS:trying to delete client not in list")
}

func (gwss *gwsServer) index(w http.ResponseWriter, r *http.Request) {
	t := time.Now().Unix()
	w.Header().Set("Content-Type", "application/json")

	response := indexResponse{
		RequestIP: r.RemoteAddr,
		Time:      int64(t),
	}

	j, _ := json.Marshal(response)
	w.Write(j)
}

func (gwss *gwsServer) startup() {

	r := mux.NewRouter()
	r.HandleFunc("/index.html", gwss.index).Methods("GET")

	wsh := WSHandler{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		Config: iwebsocket.Config{
			ReadTimeout:     60 * time.Second,
			WriteTimeout:    60 * time.Second,
			PingPeriod:      9 * 6 * time.Second,
			PongTimeout:     60 * time.Second,
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			BinaryMessages:  true,
		},
	}
	wsh.OnConnect(gwss.connect)
	r.HandleFunc("/echo", wsh.HandleRequest).Methods("GET")

	gwss.srv = &http.Server{
		Addr:    ":8080",
		Handler: r,
	}
	go gwss.srv.ListenAndServe()

	tries := 0
	for {
		_, err := http.Get("http://127.0.0.1:8080/")
		if err == nil {
			break
		}
		tries += 1
		if tries > 30 {
			fmt.Println("Server not responding")
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func (gwss *gwsServer) shutdown() {
	// for _, v := range gwss.clients {
	// 	v.con.Disconnect()
	// }
	gwss.srv.Close()
	time.Sleep(1 * time.Second)
}

func TestGorillaClientConnectDisconnect(t *testing.T) {
	var gwss gwsServer
	var client ClientConnection
	var err error
	connected := true
	tries_left := int(10)
	gwss.startup()
	d := new(WSDialer)
	client = nil
	for (client == nil) && (tries_left > 0) {
		client, _, err = d.Dial("ws://127.0.0.1:8080/echo", nil, iwebsocket.Config{
			ReadTimeout:     60 * time.Second,
			WriteTimeout:    60 * time.Second,
			PingPeriod:      9 * 6 * time.Second,
			PongTimeout:     60 * time.Second,
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			BinaryMessages:  true,
		})
		if err != nil {
			fmt.Println("Dialer error:", err)
			if tries_left > 0 {
				time.Sleep(1 * time.Second)
				tries_left -= 1
			} else {
				t.Fail()
			}
		}
	}
	if client == nil {
		fmt.Println("Dialer returned nil client")
		t.Fail()
	} else {
		got_reply := false
		client.On("echo_reply", func(s string) {
			// fmt.Println("client echo_reply", s)
			got_reply = true
		})
		client.OnDisconnect(func() {
			// fmt.Println("client echo_reply", s)
			connected = false
		})
		client.Emit("echo", "hello")
		// fmt.Println("Emit complete")
		time.Sleep(1 * time.Second)
		if !got_reply {
			fmt.Println("No echo response")
			t.Fail()
		}
	}
	c := gwss.clients[0]
	c.con.Disconnect()
	tries_left = 5
	for connected && (tries_left > 0) {
		time.Sleep(1 * time.Second)
		tries_left -= 1
	}
	if connected {
		fmt.Println("Disconnect not received by client")
		t.Fail()
	}
	gwss.shutdown()
}

func TestGorillaMixedMessagesConcurrency(t *testing.T) {
	var gwss gwsServer
	var client ClientConnection
	var err error
	tries_left := int(10)
	gwss.startup()
	d := new(WSDialer)
	client = nil
	for (client == nil) && (tries_left > 0) {
		client, _, err = d.Dial("ws://127.0.0.1:8080/echo", nil, iwebsocket.Config{
			ReadTimeout:     60 * time.Second,
			WriteTimeout:    60 * time.Second,
			PingPeriod:      9 * 6 * time.Second,
			PongTimeout:     60 * time.Second,
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			BinaryMessages:  true,
		})
		if err != nil {
			fmt.Println("Dialer error:", err)
			if tries_left > 0 {
				time.Sleep(1 * time.Second)
				tries_left -= 1
			} else {
				t.Fail()
			}
		}
	}
	if client == nil {
		fmt.Println("Dialer returned nil client")
		t.Fail()
	} else {
		cycles := int32(500)
		echo_count := int32(0)
		len_count := int32(0)
		reverse_count := int32(0)
		raw_count := int32(0)
		// fmt.Println("Dial complete")
		client.On("echo_reply", func(s string) {
			//fmt.Println("client echo_reply", s)
			atomic.AddInt32(&echo_count, 1)
		})
		client.On("len_reply", func(i int) {
			// fmt.Printf("client len_reply %d\n", i)
			atomic.AddInt32(&len_count, 1)
		})
		client.On("reverse_reply", func(s string) {
			// fmt.Println("client reverse_reply", s)
			atomic.AddInt32(&reverse_count, 1)
		})
		client.OnMessage(func(b []byte) {
			// fmt.Println("client raw_reply", hex.EncodeToString(b))
			atomic.AddInt32(&raw_count, 1)
		})
		// fmt.Println("ON complete")
		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			defer wg.Done()
			for i := 0; i < int(cycles); i++ {
				s := fmt.Sprintf("hello %d", i)
				if client.Emit("echo", s) != nil {
					fmt.Println("error serializing echo request:", s)
					t.Fail()
				}
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < int(cycles); i++ {
				s := fmt.Sprintf("hello %d", i)
				if client.Emit("reverse", s) != nil {
					fmt.Println("error serializing reverse request:", s)
					t.Fail()
				}
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < int(cycles); i++ {
				s := make([]byte, i, i)
				for j := 0; j < i; j++ {
					s[j] = byte('a')
				}
				if client.Emit("len", string(s)) != nil {
					fmt.Println("error serializing len request:", string(s))
					t.Fail()
				}
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < int(cycles); i++ {
				bb := make([]byte, 8)
				binary.BigEndian.PutUint64(bb, uint64(i))
				if client.EmitMessage(bb) != nil {
					fmt.Println("error serializing raw request:", hex.EncodeToString(bb))
					t.Fail()
				}
			}
		}()
		// ensure all messages sent
		wg.Wait()
		// fmt.Println("Emit complete")
		// wait until we complete or timeout after 1 minute
		for i := 0; i < 60; i++ {
			if (atomic.LoadInt32(&echo_count) == cycles) &&
				(atomic.LoadInt32(&len_count) == cycles) &&
				(atomic.LoadInt32(&reverse_count) == cycles) &&
				(atomic.LoadInt32(&raw_count) == cycles) {
				break
			}
			time.Sleep(1 * time.Second)
		}
		// fmt.Printf("echo, len, raw = %d, %d, %d\n", echo_count, len_count, raw_count)
		if echo_count != cycles {
			fmt.Printf("echo count mismatch, %d != %d\n", echo_count, cycles)
			t.Fail()
		}
		if len_count != cycles {
			fmt.Printf("len count mismatch, %d != %d\n", len_count, cycles)
			t.Fail()
		}
		if raw_count != cycles {
			fmt.Printf("echo count mismatch, %d != %d\n", raw_count, cycles)
			t.Fail()
		}
	}
	gwss.shutdown()
}

func TestGorillaConnectAndWait(t *testing.T) {
	var gwss gwsServer
	var client ClientConnection
	var err error
	tries_left := int(10)
	gwss.startup()
	d := new(WSDialer)
	client = nil
	for (client == nil) && (tries_left > 0) {
		client, _, err = d.Dial("ws://127.0.0.1:8080/echo", nil, iwebsocket.Config{
			ReadTimeout:     60 * time.Second,
			WriteTimeout:    60 * time.Second,
			PingPeriod:      9 * 6 * time.Second,
			PongTimeout:     60 * time.Second,
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			BinaryMessages:  true,
		})
		if err != nil {
			fmt.Println("Dialer error:", err)
			if tries_left > 0 {
				time.Sleep(1 * time.Second)
				tries_left -= 1
			} else {
				t.Fail()
			}
		}
	}
	// fail compile here if client doesn't satisfy common interface
	validateInterface(client)
	if client == nil {
		fmt.Println("Dialer returned nil client")
		t.Fail()
	} else {
		// the wait here is longer than the Timeout, so the server will disconnect if
		// ping/pong messages are not correctly triggered
		for i := 0; i < 65; i++ {
			// fmt.Printf("(sleeping) %s\n", time.Now().Format("2006-01-02 15:04:05.000000"))
			time.Sleep(1 * time.Second)
		}
		got_reply := false
		client.On("echo_reply", func(s string) {
			// fmt.Println("client echo_reply", s)
			got_reply = true
		})
		// fmt.Println("ON complete")
		client.Emit("echo", "hello")
		// fmt.Println("Emit complete")
		time.Sleep(1 * time.Second)
		if !got_reply {
			fmt.Println("No echo response")
			t.Fail()
		}
	}
	gwss.shutdown()
}
