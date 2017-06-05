// with the exclusion of the package name this file is a verbatim copy of
// iris/adapter/websocket/message.go
package websocket_client

// The MIT License (MIT)
//
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

import (
	"encoding/json"
	"github.com/kataras/go-errors"
	"github.com/valyala/bytebufferpool"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// -------------------------------------------------------------------------------------
// -------------------------------------------------------------------------------------
// -----------------websocket messages and de/serialization implementation--------------
// -------------------------------------------------------------------------------------
// -------------------------------------------------------------------------------------

/*
serializer, [de]websocketMessageSerialize the messages from the client to the websocketServer and from the websocketServer to the client
*/

// The same values are exists on client side also
const (
	websocketStringMessageType websocketMessageType = iota
	websocketIntMessageType
	websocketBoolMessageType
	websocketBytesMessageType
	websocketJSONMessageType
)

const (
	websocketMessagePrefix          = "iris-websocket-message:"
	websocketMessageSeparator       = ";"
	websocketMessagePrefixLen       = len(websocketMessagePrefix)
	websocketMessageSeparatorLen    = len(websocketMessageSeparator)
	websocketMessagePrefixAndSepIdx = websocketMessagePrefixLen + websocketMessageSeparatorLen - 1
	websocketMessagePrefixIdx       = websocketMessagePrefixLen - 1
	websocketMessageSeparatorIdx    = websocketMessageSeparatorLen - 1
)

var (
	websocketMessageSeparatorByte = websocketMessageSeparator[0]
	websocketMessageBuffer        = bytebufferpool.Pool{}
	websocketMessagePrefixBytes   = []byte(websocketMessagePrefix)
)

type (
	websocketMessageType uint8
)

func (m websocketMessageType) String() string {
	return strconv.Itoa(int(m))
}

func (m websocketMessageType) Name() string {
	if m == websocketStringMessageType {
		return "string"
	} else if m == websocketIntMessageType {
		return "int"
	} else if m == websocketBoolMessageType {
		return "bool"
	} else if m == websocketBytesMessageType {
		return "[]byte"
	} else if m == websocketJSONMessageType {
		return "json"
	}

	return "Invalid(" + m.String() + ")"

}

// websocketMessageSerialize serializes a custom websocket message from websocketServer to be delivered to the client
// returns the  string form of the message
// Supported data types are: string, int, bool, bytes and JSON.
func websocketMessageSerialize(event string, data interface{}) (string, error) {
	var msgType websocketMessageType
	var dataMessage string

	if s, ok := data.(string); ok {
		msgType = websocketStringMessageType
		dataMessage = s
	} else if i, ok := data.(int); ok {
		msgType = websocketIntMessageType
		dataMessage = strconv.Itoa(i)
	} else if b, ok := data.(bool); ok {
		msgType = websocketBoolMessageType
		dataMessage = strconv.FormatBool(b)
	} else if by, ok := data.([]byte); ok {
		msgType = websocketBytesMessageType
		dataMessage = string(by)
	} else {
		//we suppose is json
		res, err := json.Marshal(data)
		if err != nil {
			return "", err
		}
		msgType = websocketJSONMessageType
		dataMessage = string(res)
	}

	b := websocketMessageBuffer.Get()
	b.WriteString(websocketMessagePrefix)
	b.WriteString(event)
	b.WriteString(websocketMessageSeparator)
	b.WriteString(msgType.String())
	b.WriteString(websocketMessageSeparator)
	b.WriteString(dataMessage)
	dataMessage = b.String()
	websocketMessageBuffer.Put(b)

	return dataMessage, nil

}

var errInvalidTypeMessage = errors.New("Type %s is invalid for message: %s")

// websocketMessageDeserialize deserializes a custom websocket message from the client
// ex: iris-websocket-message;chat;4;themarshaledstringfromajsonstruct will return 'hello' as string
// Supported data types are: string, int, bool, bytes and JSON.
func websocketMessageDeserialize(event string, websocketMessage string) (message interface{}, err error) {
	t, formaterr := strconv.Atoi(websocketMessage[websocketMessagePrefixAndSepIdx+len(event)+1 : websocketMessagePrefixAndSepIdx+len(event)+2]) // in order to iris-websocket-message;user;-> 4
	if formaterr != nil {
		return nil, formaterr
	}
	_type := websocketMessageType(t)
	_message := websocketMessage[websocketMessagePrefixAndSepIdx+len(event)+3:] // in order to iris-websocket-message;user;4; -> themarshaledstringfromajsonstruct

	if _type == websocketStringMessageType {
		message = string(_message)
	} else if _type == websocketIntMessageType {
		message, err = strconv.Atoi(_message)
	} else if _type == websocketBoolMessageType {
		message, err = strconv.ParseBool(_message)
	} else if _type == websocketBytesMessageType {
		message = []byte(_message)
	} else if _type == websocketJSONMessageType {
		err = json.Unmarshal([]byte(_message), &message)
	} else {
		return nil, errInvalidTypeMessage.Format(_type.Name(), websocketMessage)
	}

	return
}

// getWebsocketCustomEvent return empty string when the websocketMessage is native message
func getWebsocketCustomEvent(websocketMessage string) string {
	if len(websocketMessage) < websocketMessagePrefixAndSepIdx {
		return ""
	}
	s := websocketMessage[websocketMessagePrefixAndSepIdx:]
	evt := s[:strings.IndexByte(s, websocketMessageSeparatorByte)]
	return evt
}

const (
	letterBytes   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

var src = rand.NewSource(time.Now().UnixNano())

// random takes a parameter (int) and returns random slice of byte
// ex: var randomstrbytes []byte; randomstrbytes = utils.Random(32)
func random(n int) []byte {
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return b
}

// randomString accepts a number(10 for example) and returns a random string using simple but fairly safe random algorithm
func randomString(n int) string {
	return string(random(n))
}
