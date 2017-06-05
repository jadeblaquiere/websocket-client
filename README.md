# wsclient

wsclient is a client library which implements a subset of the [Iris](https://github.com/kataras/iris) websocket
server interface. wsclient provides a wrapper of the (gorilla websocket Dialer)[https://godoc.org/github.com/gorilla/websocket#Dialer]
interface to initiate client connections. Once this connection is established,
the client interface follows the Iris server functions for On(), OnMessage(), 
Emit() and EmitMessage(). 

wsclient does not (yet) implement Joining/Leaving rooms.
