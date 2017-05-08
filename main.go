package main

import (
	"bytes"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"github.com/tidwall/gjson"
	"github.com/twinj/uuid"

	"container/list"

	"io/ioutil"

	"os"

	"gopkg.in/kataras/iris.v6"
	"gopkg.in/kataras/iris.v6/adaptors/httprouter"
	"gopkg.in/kataras/iris.v6/adaptors/websocket"
)

type ClientConnection struct {
	Username   string
	Connection websocket.Connection
	Callbacks  map[int64]RequestCallback
	Mid        int64
}

type RequestCallback func(gjson.Result)

type OAuthData struct {
	Client string
	Secret string
}
type AuthUserData struct {
	Active   bool   `json:"active"`
	Username string `json:"username"`
}

func getUserInfo(token string, auth *OAuthData) (user *AuthUserData, e error) {

	form := url.Values{
		"token":           {token},
		"token_type_hint": {"access_token"},
	}
	body := bytes.NewBufferString(form.Encode())
	resp, err := http.Post("https://"+auth.Client+":"+auth.Secret+"@auth.wiklosoft.com/v1/oauth/introspect", "application/x-www-form-urlencoded", body)
	if err != nil {
		log.Println(err)
		return &AuthUserData{}, err
	}
	defer resp.Body.Close()

	userData := &AuthUserData{}

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return &AuthUserData{}, err
	}
	r := gjson.ParseBytes(bodyBytes)

	userData.Username = r.Get("username").String()
	userData.Active = r.Get("active").Bool()

	return userData, nil
}
func sendRequest(conn *ClientConnection, name string, payload string, callback RequestCallback) {
	log.Println("sendRequest " + payload)
	if callback != nil {
		conn.Callbacks[conn.Mid] = callback
	}
	conn.Connection.EmitMessage([]byte(`{ "mid":` + strconv.FormatInt(conn.Mid, 10) + `, "name":"` + name + `", "payload":` + payload + `}`))
	conn.Mid++
}
func generateMessageUUID() string {
	return uuid.NewV4().String()
}

func handleAlexaMessage(message string, clientConnections *list.List, userInfo *AuthUserData, c *iris.Context) {
	log.Println("handleAlexaMessage: " + message)

	ch := make(chan string)

	if clientConnections.Len() > 0 {
		for e := clientConnections.Front(); e != nil; e = e.Next() {
			con := e.Value.(*ClientConnection)
			if userInfo.Username != "" && con.Username == userInfo.Username {
				sendRequest(con, "AlexaMessage", message, func(response gjson.Result) {
					log.Println("Response from backend")
					ch <- response.String()
				})
				break
			}
		}
		c.JSON(iris.StatusOK, <-ch)
	} else {
		c.JSON(iris.StatusBadGateway, nil)
	}

}

func main() {
	app := iris.New()
	app.Adapt(iris.DevLogger(), httprouter.New())

	ws := websocket.New(websocket.Config{
		Endpoint:       "/connect",
		MaxMessageSize: 102400,
	})
	app.Adapt(ws)

	clientConnections := list.New()

	alexaAuth := &OAuthData{
		Client: os.Getenv("AUTH_ALEXA_CLIENT"),
		Secret: os.Getenv("AUTH_ALEXA_CLIENT_SECRET"),
	}

	ws.OnConnection(func(c websocket.Connection) {
		log.Println("New connection", c.ID())
		newConnection := &ClientConnection{
			Connection: c,
			Mid:        1,
			Callbacks:  make(map[int64]RequestCallback)}
		clientConnections.PushBack(newConnection)

		c.OnMessage(func(messageBytes []byte) {
			messageJSON := gjson.ParseBytes(messageBytes)

			mid := messageJSON.Get("mid").Int()

			callback := newConnection.Callbacks[mid]
			if callback != nil {
				callback(messageJSON)
				delete(newConnection.Callbacks, mid)
			}
			eventName := messageJSON.Get("name").String()
			log.Println("Event: " + eventName)
			if eventName == "RequestAuthorize" {
				token := messageJSON.Get("payload.token").String()
				userInfo, err := getUserInfo(token, alexaAuth)
				if err != nil {
					log.Println(err)
					return
				}
				if userInfo.Username == "" {
					log.Println("Connection not authorized")
					return
				}
				log.Println("New connection authorized for " + userInfo.Username)
				newConnection.Username = userInfo.Username
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
	app.Get("/", func(c *iris.Context) {
		c.Text(iris.StatusOK, "ok")
	})

	app.Post("/", func(c *iris.Context) {
		bodyBytes, err := ioutil.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(iris.StatusInternalServerError, nil)
			return
		}
		body := string(bodyBytes)

		token := gjson.Get(body, "payload.accessToken").String()

		userInfo, err := getUserInfo(token, alexaAuth)
		if err != nil {
			c.JSON(iris.StatusUnauthorized, nil)
			log.Println(err)
			return
		}

		handleAlexaMessage(body, clientConnections, userInfo, c)
	})

	app.Listen(":10001")
}
