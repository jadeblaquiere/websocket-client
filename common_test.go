// Copyright 2017 Joseph deBlaquiere. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package websocket_client

func validateInterface(con ClientConnection) bool {
	return true
}

// test server model used to test client code

type indexResponse struct {
	RequestIP string `json:"request_ip"`
	Time      int64  `json:"unix_time"`
}

type wsClient struct {
	con ClientConnection
	// wss *wsServer
}

func (wsc *wsClient) echoRawMessage(message []byte) {
	// fmt.Println("recv :", string(message))
	wsc.con.EmitMessage(message)
}

func (wsc *wsClient) echoString(message string) {
	// fmt.Println("recv :", message)
	wsc.con.Emit("echo_reply", message)
}

func (wsc *wsClient) reverseString(message string) {
	// fmt.Println("recv :", message)
	chars := []rune(message)
	for i, j := 0, len(chars)-1; i < j; i, j = i+1, j-1 {
		chars[i], chars[j] = chars[j], chars[i]
	}
	wsc.con.Emit("reverse_reply", string(chars))
}

func (wsc *wsClient) lenString(message string) {
	// fmt.Println("recv :", message)
	wsc.con.Emit("len_reply", len(message))
}

func (wsc *wsClient) disconnect() {
	// fmt.Println("client disconnect @ ", time.Now().Format("2006-01-02 15:04:05.000000"))
}
