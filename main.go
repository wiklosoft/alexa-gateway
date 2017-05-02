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

	"strings"

	"gopkg.in/kataras/iris.v6"
	"gopkg.in/kataras/iris.v6/adaptors/httprouter"
	"gopkg.in/kataras/iris.v6/adaptors/websocket"
)

const (
	NAMESPACE_CONTROL   = "Alexa.ConnectedHome.Control"
	NAMESPACE_DISCOVERY = "Alexa.ConnectedHome.Discovery"

	DISCOVER_APPLIANCES_REQUEST  = "DiscoverAppliancesRequest"
	DISCOVER_APPLIANCES_RESPONSE = "DiscoverAppliancesResponse"

	TURN_ON_REQUEST       = "TurnOnRequest"
	TURN_OFF_REQUEST      = "TurnOffRequest"
	TURN_ON_CONFIRMATION  = "TurnOnConfirmation"
	TURN_OFF_CONFIRMATION = "TurnOffConfirmation"

	SET_PERCENTAGE_REQUEST            = "SetPercentageRequest"
	SET_PERCENTAGE_CONFIRMATION       = "SetPercentageConfirmation"
	INCREMENT_PERCENTAGE_REQUEST      = "IncrementPercentageRequest"
	INCREMENT_PERCENTAGE_CONFIRMATION = "IncrementPercentageConfirmation"
	DECREMENT_PERCENTAGE_REQUEST      = "DecrementPercentageRequest"
	DECREMENT_PERCENTAGE_CONFIRMATION = "DecrementPercentageConfirmation"

	MANUFACTURER_NAME = "Wiklosoft"
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

type AlexaMessage struct {
	Header  AlexaHeader  `json:"header"`
	Payload AlexaPayload `json:"payload"`
}
type AlexaDevice struct {
	ApplicanceID        string `json:"applianceId"`
	ManufacturerName    string `json:"manufacturerName"`
	ModelName           string `json:"modelName"`
	FriendlyName        string `json:"friendlyName"`
	FriendlyDescription string `json:"friendlyDescription"`
	IsReachable         bool   `json:"isReachable"`
	Version             string `json:"version"`

	Actions                    []string `json:"actions"`
	AdditionalApplianceDetails struct {
	} `json:"additionalApplianceDetails"`
}

type AlexaDiscoveryResponse struct {
	Header  AlexaHeader `json:"header"`
	Payload struct {
		DiscoveredAppliances []AlexaDevice `json:"discoveredAppliances"`
	} `json:"payload"`
}

type AuthUserData struct {
	Active   bool   `json:"active"`
	Username string `json:"username"`
}

type IotVariable struct {
	Interface    string `json:"if"`
	ResourceType string `json:"rt"`
	Href         string `json:"href"`
	Name         string `json:"n"`
}

type IotDevice struct {
	ID        string
	Name      string
	Variables []IotVariable `json:"variables"`
}

func (device *IotDevice) getVariable(href string) *IotVariable {
	for _, variable := range device.Variables {
		if variable.Href == href {
			return &variable
		}
	}
	return nil
}

type ClientConnection struct {
	Username   string
	Connection websocket.Connection
	DeviceList []IotDevice

	Callbacks map[int]RequestCallback
	Mid       int
}

type RequestCallback func([]byte)

type IotPayload struct {
	Request string `json:"request"`
}
type IotMessage struct {
	Mid     int        `json:"mid"`
	Payload IotPayload `json:"payload"`
	Event   string     `json:"event"`
}

type IotDevices struct {
	Devices []IotDevice `json:"devices"`
}

type EventDeviceListMessage struct {
	Mid     int        `json:"mid"`
	Payload IotDevices `json:"payload"`
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
	log.Println("sendRequest " + request)
	conn.Callbacks[conn.Mid] = callback
	conn.Connection.EmitMessage([]byte(`{ "mid":` + strconv.Itoa(conn.Mid) + `, "payload":{"request":"` + request + `"}}`))
	conn.Mid++
}

func parseDeviceList(conn *ClientConnection, messageBytes []byte) {
	message := &EventDeviceListMessage{}
	if err := json.Unmarshal(messageBytes, &message); err != nil {
		fmt.Println(err)
	}
	conn.DeviceList = message.Payload.Devices
	log.Println(message)
}

func handleAlexaMessage(request *AlexaMessage, clientConnections *list.List, userInfo *AuthUserData, c *iris.Context) {

	if request.Header.Namespace == NAMESPACE_DISCOVERY {
		response := &AlexaDiscoveryResponse{}
		response.Header.Name = DISCOVER_APPLIANCES_RESPONSE
		response.Header.Namespace = NAMESPACE_DISCOVERY
		response.Header.PayloadVersion = "2"
		response.Header.MessageID = "746d98-ab02-4c9e-9d0d-b44711658414" //TODO: add random value

		for e := clientConnections.Front(); e != nil; e = e.Next() {
			con := e.Value.(*ClientConnection)
			if userInfo.Username != "" && con.Username == userInfo.Username {
				for _, device := range con.DeviceList {
					log.Println("Adding device " + device.ID)

					if device.getVariable("/master") != nil {
						dev := AlexaDevice{
							ApplicanceID:        device.ID,
							ManufacturerName:    MANUFACTURER_NAME,
							ModelName:           "The Best Model",
							FriendlyName:        device.Name,
							FriendlyDescription: "OCF Device by Wiklosoft",
							IsReachable:         true,
							Version:             "0.1",
						}

						dev.Actions = append(dev.Actions, "turnOn")
						dev.Actions = append(dev.Actions, "turnOff")
						response.Payload.DiscoveredAppliances = append(response.Payload.DiscoveredAppliances, dev)
					}

					for _, variable := range device.Variables {
						if variable.ResourceType == "oic.r.light.dimming" {
							dev := AlexaDevice{
								ApplicanceID:        device.ID + ":" + strings.Replace(variable.Href, "/", "_", -1),
								ManufacturerName:    MANUFACTURER_NAME,
								ModelName:           "The Best Model",
								FriendlyName:        variable.Name,
								FriendlyDescription: "OCF Resource by Wiklosoft",
								IsReachable:         true,
								Version:             "0.1",
							}

							dev.Actions = append(dev.Actions, "setPercentage")
							dev.Actions = append(dev.Actions, "incrementPercentage")
							dev.Actions = append(dev.Actions, "decrementPercentage")
							response.Payload.DiscoveredAppliances = append(response.Payload.DiscoveredAppliances, dev)
						}
					}
				}
			}
		}
		log.Println(response)
		c.JSON(iris.StatusOK, response)
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

	ws.OnConnection(func(c websocket.Connection) {
		log.Println("New connection", c.ID())
		newConnection := &ClientConnection{Username: "",
			Connection: c,
			Mid:        1,
			Callbacks:  make(map[int]RequestCallback)}
		clientConnections.PushBack(newConnection)

		sendRequest(newConnection, "RequestGetDevices", func(response []byte) {
			parseDeviceList(newConnection, response)
		})

		c.OnMessage(func(messageBytes []byte) {
			message := &IotMessage{}

			if err := json.Unmarshal(messageBytes, &message); err != nil {
				fmt.Println(err)
			}
			log.Println(message)

			callback := newConnection.Callbacks[message.Mid]
			if callback != nil {
				callback(messageBytes)
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

		AlexaMessage := &AlexaMessage{}

		if err := json.Unmarshal([]byte(r), &AlexaMessage); err != nil {
			fmt.Println(err)
		}

		userInfo, err := getUserInfo(AlexaMessage.Payload.AccessToken)
		if err != nil {
			fmt.Println(err)
			return
		}

		handleAlexaMessage(AlexaMessage, clientConnections, &userInfo, c)
	})

	app.Listen(":12345")
}
