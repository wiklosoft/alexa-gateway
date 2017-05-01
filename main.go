package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"container/list"

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
	client_id := "test_client_1"
	client_secret := "test_secret"

	form := url.Values{
		"token":           {token},
		"token_type_hint": {"access_token"},
	}
	body := bytes.NewBufferString(form.Encode())
	resp, err := http.Post("https://"+client_id+":"+client_secret+"@auth.wiklosoft.com/v1/oauth/introspect", "application/x-www-form-urlencoded", body)
	if err != nil {
		fmt.Println(err)
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
		fmt.Println("New connection %s", c.ID())
		newConnection := &ClientConnection{"", c}
		clientConnections.PushBack(newConnection)
		c.OnDisconnect(func() {
			for e := clientConnections.Front(); e != nil; e = e.Next() {
				con := e.Value.(ClientConnection)
				if con.Connection.ID() == c.ID() {
					clientConnections.Remove(e)
					break
				}
			}
			fmt.Println("Connection with ID: %s has been disconnected!\n", c.ID())
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
			con := e.Value.(ClientConnection)

			if con.Username == userInfo.Username {
				fmt.Println("Send message to connection ID: %s", con.Connection.ID())
				con.Connection.Emit("AlexaRequest", r)
			}
		}
		c.JSON(iris.StatusOK, nil)
	})

	app.Listen(":12345")
}
