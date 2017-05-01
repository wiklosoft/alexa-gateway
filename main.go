package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"container/list"

	"strings"

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

type ClientConnection struct {
	Username   string
	Connection websocket.Connection
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

	fmt.Println(r)

	if err := json.Unmarshal([]byte(r), &userData); err != nil {
		log.Fatal(err)
	}

	fmt.Println(userData)
	return userData, nil
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
		newConnection := &ClientConnection{"", c}
		clientConnections.PushBack(newConnection)
		c.OnMessage(func(messageBytes []byte) {
			message := string(messageBytes)

			messageType := strings.Split(message, ":")[0]
			messagePayload := strings.Split(message, ":")[1]

			if messageType == "auth" {
				log.Println("Auth request on connection ", c.ID(), " - ", messagePayload)
				userInfo, err := getUserInfo(messagePayload)
				if err != nil {
					fmt.Println(err)
					return
				}
				newConnection.Username = userInfo.Username
				log.Println("Saved username: " + newConnection.Username)
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
