package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"container/list"

	"strconv"

	"gopkg.in/kataras/iris.v6"
	"gopkg.in/kataras/iris.v6/adaptors/httprouter"
	"gopkg.in/kataras/iris.v6/adaptors/websocket"
)

type AlexaHeader struct {
	Namespace      string `json:"namespace"`
	Name           string `json:"name"`
	PayloadVersion string `json:"payloadVersion"`
	MessageID      string `json:"messageId"`
}
type AlexaPayload struct {
	AccessToken string `json:"accessToken"`
}

type AlexaRequest struct {
	Header  AlexaHeader  `json:"header"`
	Payload AlexaPayload `json:"payload"`
}

type AuthUserData struct {
	Active   bool   `json:"active"`
	Username string `json:"username"`
}

type IotVariable struct {
	Interface    string
	ResourceType string
	Href         string
	Name         string
}

type IotDevice struct {
	ID        string
	Name      string
	Variables []IotVariable
}

type ClientConnection struct {
	Username   string
	Connection websocket.Connection
	DeviceList []IotDevice

	Callbacks map[int]RequestCallback
	Mid       int
}

type RequestCallback func(string)

type IotPayload struct {
	Request string `json:"request"`
}
type IotMessage struct {
	Mid     int        `json:"mid"`
	Payload IotPayload `json:"payload"`
	Event   string     `json:"event"`
}

func getUserInfo(token string) (user AuthUserData, e error) {
	clientID := "test_client_1"
	clientSecret := "test_secret"

	form := url.Values{
		"token":           {token},
		"token_type_hint": {"access_token"},
	}
	body := bytes.NewBufferString(form.Encode())
	resp, err := http.Post("https://"+clientID+":"+clientSecret+"@auth.wiklosoft.com/v1/oauth/introspect", "application/x-www-form-urlencoded", body)
	if err != nil {
		log.Println(err)
		return AuthUserData{}, err
	}
	defer resp.Body.Close()

	userData := AuthUserData{}

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	r := buf.String()

	if err := json.Unmarshal([]byte(r), &userData); err != nil {
		log.Fatal(err)
	}
	return userData, nil
}

func sendRequest(conn *ClientConnection, request string, callback RequestCallback) {
	conn.Callbacks[conn.Mid] = callback
	conn.Connection.EmitMessage([]byte(`{ "mid":` + strconv.Itoa(conn.Mid) + `, "payload":{"request":"` + request + `"}}`))
	conn.Mid++
}

func main() {
	app := iris.New()
	app.Adapt(iris.DevLogger(), httprouter.New())

	ws := websocket.New(websocket.Config{
		Endpoint: "/connect",
	})
	app.Adapt(ws)

	clientConnections := list.New()

	ws.OnConnection(func(c websocket.Connection) {
		log.Println("New connection", c.ID())
		newConnection := &ClientConnection{Username: "",
			Connection: c,
			Mid:        1,
			Callbacks:  make(map[int]RequestCallback)}
		clientConnections.PushBack(newConnection)

		sendRequest(newConnection, "RequestGetDevices", func(response string) {
			log.Println("Callback response" + response)
		})

		c.OnMessage(func(messageBytes []byte) {
			message := &IotMessage{}

			if err := json.Unmarshal(messageBytes, &message); err != nil {
				fmt.Println(err)
			}
			log.Println(message)

			callback := newConnection.Callbacks[message.Mid]
			if callback != nil {
				callback("message")
				delete(newConnection.Callbacks, message.Mid)
			}
		})

		c.OnDisconnect(func() {
			for e := clientConnections.Front(); e != nil; e = e.Next() {
				con := e.Value.(*ClientConnection)
				if con.Connection.ID() == c.ID() {
					clientConnections.Remove(e)
					break
				}
			}
			log.Println("Connection with ID: " + c.ID() + " has been disconnected!")
		})
	})
	app.Post("/", func(c *iris.Context) {
		buf := new(bytes.Buffer)
		buf.ReadFrom(c.Request.Body)
		r := buf.String()

		fmt.Println(r)

		alexaRequest := &AlexaRequest{}

		if err := json.Unmarshal([]byte(r), &alexaRequest); err != nil {
			fmt.Println(err)
		}

		userInfo, err := getUserInfo(alexaRequest.Payload.AccessToken)
		if err != nil {
			fmt.Println(err)
			return
		}

		for e := clientConnections.Front(); e != nil; e = e.Next() {
			con := e.Value.(*ClientConnection)

			if userInfo.Username != "" && con.Username == userInfo.Username {
				log.Println("Send message to connection ID", con.Connection.ID())
				con.Connection.EmitMessage([]byte("AlexaRequest:" + r))
			}
		}
		c.JSON(iris.StatusOK, nil)
	})

	app.Listen(":12345")
}
