package main

import (
	"bytes"
	"fmt"

	"gopkg.in/kataras/iris.v6"
	"gopkg.in/kataras/iris.v6/adaptors/httprouter"
	"gopkg.in/kataras/iris.v6/adaptors/websocket"
)

type AlexaHeader struct {
	Namespace      string `json:"namespace"`
	Name           string `json:"name"`
	PayloadVersion string `json:"payloadVersion"`
	MessageId      string `json:"messageId"`
}
type AlexaPayload struct {
	AccessToken string `json:"accessToken"`
}

type AlexaRequest struct {
	Header  AlexaHeader  `json:"header"`
	Payload AlexaPayload `json:"payload"`
}

func main() {
	app := iris.New()
	app.Adapt(iris.DevLogger(), httprouter.New())

	ws := websocket.New(websocket.Config{
		Endpoint: "/connect",
	})
	app.Adapt(ws)

	clientConnections := make(map[string]websocket.Connection)

	ws.OnConnection(func(c websocket.Connection) {
		fmt.Println("New connection %s", c.ID())
		clientConnections[c.ID()] = c
		c.OnDisconnect(func() {
			delete(clientConnections, c.ID())
			fmt.Println("Connection with ID: %s has been disconnected!\n", c.ID())
		})
	})
	app.Post("/", func(c *iris.Context) {
		buf := new(bytes.Buffer)
		buf.ReadFrom(c.Request.Body)
		r := buf.String()

		fmt.Println(r)

		for connectionID, connection := range clientConnections {
			fmt.Println("Send message to connection ID: %s", connectionID)
			connection.Emit("AlexaRequest", r)
		}

		c.JSON(iris.StatusOK, nil)
	})

	app.Listen(":12345")
}
