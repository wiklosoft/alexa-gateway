package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/tidwall/gjson"
	"github.com/twinj/uuid"

	"container/list"

	"strconv"

	"strings"

	"io/ioutil"

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
type AlexaControlResponse struct {
	Header  AlexaHeader `json:"header"`
	Payload struct {
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

func sendRequest(conn *ClientConnection, payload string, callback RequestCallback) {
	log.Println("sendRequest " + payload)
	conn.Callbacks[conn.Mid] = callback
	conn.Connection.EmitMessage([]byte(`{ "mid":` + strconv.Itoa(conn.Mid) + `, "payload":` + payload + `}`))
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

func generateMessageUUID() string {
	return uuid.NewV4().String()
}
func setDeviceValue(clientConnection *ClientConnection, deviceID string, resourceID string, valueObject string) {
	sendRequest(clientConnection, `{"di":"`+deviceID+`","request":"RequestSetValue", "resource":"`+resourceID+`", "value":`+valueObject+`}`, nil)
}

func onTurnOnOffRequest(clientConnection *ClientConnection, deviceID string, value bool) {
	setDeviceValue(clientConnection, deviceID, "/master", `{"value":`+strconv.FormatBool(value)+`}`)
}
func onSetPercentRequest(deviceID string, resource string, value int64) {

}
func onChangePercentRequest(deviceID string, resource string, value int64) {

}

func handleAlexaMessage(message string, clientConnections *list.List, userInfo *AuthUserData, c *iris.Context) {
	namespace := gjson.Get(message, "header.namespace").String()

	if namespace == NAMESPACE_DISCOVERY {
		response := &AlexaDiscoveryResponse{}
		response.Header.Name = DISCOVER_APPLIANCES_RESPONSE
		response.Header.Namespace = NAMESPACE_DISCOVERY
		response.Header.PayloadVersion = "2"
		response.Header.MessageID = generateMessageUUID()

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
	} else if namespace == NAMESPACE_CONTROL {
		name := gjson.Get(message, "header.name").String()
		response := &AlexaControlResponse{}

		response.Header.Namespace = NAMESPACE_CONTROL
		response.Header.PayloadVersion = "2"
		response.Header.MessageID = generateMessageUUID()

		applianceID := strings.Split(gjson.Get(message, "payload.appliance.applianceId").String(), ":")
		clientConnection := clientConnections.Front().Value.(*ClientConnection) //TODO: get client connection for this device - include hubID in url or smth
		deviceID := applianceID[0]
		resource := ""
		if len(applianceID) == 2 {
			resource = applianceID[1]
		}

		if name == TURN_ON_REQUEST {
			response.Header.Name = TURN_ON_CONFIRMATION
			onTurnOnOffRequest(clientConnection, deviceID, true)
		} else if name == TURN_OFF_REQUEST {
			response.Header.Name = TURN_OFF_CONFIRMATION
			onTurnOnOffRequest(clientConnection, deviceID, false)
		} else if name == SET_PERCENTAGE_REQUEST {
			response.Header.Name = SET_PERCENTAGE_REQUEST
			percent := gjson.Get(message, "payload.percentageState.value").Int()
			onSetPercentRequest(deviceID, resource, percent)
		} else if name == INCREMENT_PERCENTAGE_REQUEST {
			response.Header.Name = INCREMENT_PERCENTAGE_CONFIRMATION
			percent := gjson.Get(message, "payload.deltaPercentage.value").Int()
			onChangePercentRequest(deviceID, resource, percent)
		} else if name == DECREMENT_PERCENTAGE_REQUEST {
			response.Header.Name = DECREMENT_PERCENTAGE_CONFIRMATION
			percent := gjson.Get(message, "payload.deltaPercentage.value").Int()
			onChangePercentRequest(deviceID, resource, -percent)
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

		sendRequest(newConnection, `{"request":"RequestGetDevices"}`, func(response []byte) {
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
		bodyBytes, err := ioutil.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(iris.StatusInternalServerError, nil)
			return
		}
		body := string(bodyBytes)

		token := gjson.Get(body, "header.payload.accessToken").String()

		userInfo, err := getUserInfo(token)
		if err != nil {
			log.Println(err)
			return
		}

		handleAlexaMessage(body, clientConnections, &userInfo, c)
	})

	app.Listen(":12345")
}
