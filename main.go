package main

import (
	"bytes"
	"log"
	"net/http"
	"net/url"

	"github.com/tidwall/gjson"
	"github.com/twinj/uuid"

	"container/list"

	"strconv"

	"strings"

	"io/ioutil"

	"os"

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
	Value        string
}

type IotDevice struct {
	ID        string
	Name      string
	Variables []*IotVariable `json:"variables"`
}

func (device *IotDevice) getVariable(href string) *IotVariable {
	for _, variable := range device.Variables {
		if variable.Href == href {
			return variable
		}
	}
	return nil
}

func (connection *ClientConnection) getDevice(uuid string) *IotDevice {
	for device := connection.DeviceList.Front(); device != nil; device = device.Next() {
		if device.Value.(*IotDevice).ID == uuid {
			return device.Value.(*IotDevice)
		}
	}
	return nil
}

type ClientConnection struct {
	Username   string
	Connection websocket.Connection
	DeviceList list.List

	Callbacks map[int64]RequestCallback
	Mid       int64
	Uuid      string
	Name      string
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

type IotDevices struct {
	Devices []IotDevice `json:"devices"`
}

type EventDeviceListMessage struct {
	Mid     int        `json:"mid"`
	Payload IotDevices `json:"payload"`
	Event   string     `json:"event"`
}

type OAuthData struct {
	Client string
	Secret string
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

func sendRequest(conn *ClientConnection, payload string, callback RequestCallback) {
	log.Println("sendRequest " + payload)
	if callback != nil {
		conn.Callbacks[conn.Mid] = callback
	}
	conn.Connection.EmitMessage([]byte(`{ "mid":` + strconv.FormatInt(conn.Mid, 10) + `, "payload":` + payload + `}`))
	conn.Mid++
}
func handleValueUpdate(conn *ClientConnection, message gjson.Result) {
	log.Println("handleValueUpdate " + message.String())

	deviceID := message.Get("payload.di").String()
	resourceID := message.Get("payload.resource").String()
	value := message.Get("payload.value").String()

	device := conn.getDevice(deviceID)
	if device == nil {
		log.Println("Unable to find device with ID" + deviceID)
		return
	}
	device.getVariable(resourceID).Value = value
}
func parseDeviceList(conn *ClientConnection, message string) {
	devices := gjson.Get(message, "payload.devices").Array()
	//Add new devices
	for _, deviceData := range devices {
		deviceID := deviceData.Get("id").String()

		if conn.getDevice(deviceID) != nil {
			continue
		}
		d := &IotDevice{
			ID:   deviceID,
			Name: deviceData.Get("name").String(),
		}

		sendRequest(conn, `{"name":"RequestSubscribeDevice", "uuid":"`+d.ID+`"}`, nil)

		for _, variableData := range deviceData.Get("variables").Array() {
			v := &IotVariable{
				Href:         variableData.Get("href").String(),
				Name:         variableData.Get("n").String(),
				Interface:    variableData.Get("if").String(),
				ResourceType: variableData.Get("rt").String(),
				Value:        variableData.Get("values").String(),
			}
			d.Variables = append(d.Variables, v)
		}
		conn.DeviceList.PushBack(d)
	}
	deviceIDs := gjson.Get(message, "payload.devices.#.id").Array()

	for device := conn.DeviceList.Front(); device != nil; device = device.Next() {
		found := false
		for _, deviceID := range deviceIDs {
			if device.Value.(*IotDevice).ID == deviceID.String() {
				found = true
			}
		}
		if !found {
			sendRequest(conn, `{"name":"RequestUnsubscribeDevice", "uuid":"`+device.Value.(*IotDevice).ID+`"}`, nil)
			conn.DeviceList.Remove(device)
		}
	}
}

func generateMessageUUID() string {
	return uuid.NewV4().String()
}
func setDeviceValue(clientConnection *ClientConnection, deviceID string, resourceID string, valueObject string) {
	sendRequest(clientConnection, `{"di":"`+deviceID+`","name":"RequestSetValue", "resource":"`+resourceID+`", "value":`+valueObject+`}`, nil)
}

func onTurnOnOffRequest(clientConnection *ClientConnection, device *IotDevice, value bool) {
	setDeviceValue(clientConnection, device.ID, "/master", `{"value":`+strconv.FormatBool(value)+`}`)
}
func onSetPercentRequest(clientConnection *ClientConnection, device *IotDevice, resource string, value int64) {
	resourceType := device.getVariable(resource).ResourceType
	variable := gjson.Parse(device.getVariable(resource).Value)
	log.Println("onSetPercentRequest " + resource + " variable:" + device.getVariable(resource).Value)
	if resourceType == "oic.r.light.dimming" {
		var max int64
		if !variable.Get("range").Exists() {
			max = 100
		} else {
			var err error
			max, err = strconv.ParseInt(strings.Split(variable.Get("range").String(), ",")[1], 10, 0)
			if err != nil {
				log.Println(err)
				max = 100
			}
		}

		newValue := value * max / 100

		setDeviceValue(clientConnection, device.ID, resource, `{"dimmingSetting":`+strconv.FormatInt(newValue, 10)+`}`)
	}
}
func onChangePercentRequest(conn *ClientConnection, device *IotDevice, resource string, value int64) {
	resourceType := device.getVariable(resource).ResourceType
	variable := gjson.Parse(device.getVariable(resource).Value)

	if resourceType == "oic.r.light.dimming" {
		var max int64
		if !variable.Get("range").Exists() {
			max = 100
		} else {
			var err error
			max, err = strconv.ParseInt(strings.Split(variable.Get("range").String(), ",")[1], 10, 0)
			if err != nil {
				log.Println(err)
				max = 100
			}
		}

		diffValue := value * max / 100
		prevValue := variable.Get("dimmingSetting").Int()

		newValue := prevValue + diffValue
		log.Println("onChangePercentRequest oldValue:", prevValue, "newValue: ", newValue, " diff:", diffValue)

		if newValue > max {
			newValue = max
		}

		if newValue < 0 {
			newValue = 0
		}
		setDeviceValue(conn, device.ID, resource, `{"dimmingSetting":`+strconv.FormatInt(newValue, 10)+`}`)
	}
}

func handleAlexaMessage(message string, clientConnections *list.List, userInfo *AuthUserData, c *iris.Context) {
	namespace := gjson.Get(message, "header.namespace").String()

	log.Println("handleAlexaMessage: " + message)
	if namespace == NAMESPACE_DISCOVERY {
		response := &AlexaDiscoveryResponse{}
		response.Header.Name = DISCOVER_APPLIANCES_RESPONSE
		response.Header.Namespace = NAMESPACE_DISCOVERY
		response.Header.PayloadVersion = "2"
		response.Header.MessageID = generateMessageUUID()

		for e := clientConnections.Front(); e != nil; e = e.Next() {
			con := e.Value.(*ClientConnection)
			if userInfo.Username != "" && con.Username == userInfo.Username {
				for d := con.DeviceList.Front(); d != nil; d = d.Next() {
					device := d.Value.(*IotDevice)
					log.Println("Adding device " + device.ID)

					if device.getVariable("/master") != nil {
						dev := AlexaDevice{
							ApplicanceID:        con.Uuid + ":" + device.ID,
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
								ApplicanceID:        con.Uuid + ":" + device.ID + ":" + strings.Replace(variable.Href, "/", "_", -1),
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

		connectionID := applianceID[0]
		deviceID := applianceID[1]
		resource := ""
		if len(applianceID) == 2 {
			resource = strings.Replace(applianceID[1], "_", "/", -1)
		}
		var clientConnection *ClientConnection
		for e := clientConnections.Front(); e != nil; e = e.Next() {
			if e.Value.(*ClientConnection).Uuid == connectionID {
				clientConnection = e.Value.(*ClientConnection)
			}
		}
		if clientConnection == nil {
			//TODO: notify amazon that device does not exist
			return
		}

		device := clientConnection.getDevice(deviceID)
		if device == nil {
			//TODO: notify amazon that device does not exist
			return
		}

		if name == TURN_ON_REQUEST {
			response.Header.Name = TURN_ON_CONFIRMATION
			onTurnOnOffRequest(clientConnection, device, true)
		} else if name == TURN_OFF_REQUEST {
			response.Header.Name = TURN_OFF_CONFIRMATION
			onTurnOnOffRequest(clientConnection, device, false)
		} else if name == SET_PERCENTAGE_REQUEST {
			response.Header.Name = SET_PERCENTAGE_CONFIRMATION
			percent := gjson.Get(message, "payload.percentageState.value").Int()
			onSetPercentRequest(clientConnection, device, resource, percent)
		} else if name == INCREMENT_PERCENTAGE_REQUEST {
			response.Header.Name = INCREMENT_PERCENTAGE_CONFIRMATION
			percent := gjson.Get(message, "payload.deltaPercentage.value").Int()
			onChangePercentRequest(clientConnection, device, resource, percent)
		} else if name == DECREMENT_PERCENTAGE_REQUEST {
			response.Header.Name = DECREMENT_PERCENTAGE_CONFIRMATION
			percent := gjson.Get(message, "payload.deltaPercentage.value").Int()
			onChangePercentRequest(clientConnection, device, resource, -percent)
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

	hubAuth := &OAuthData{
		Client: os.Getenv("AUTH_HUB_CLIENT"),
		Secret: os.Getenv("AUTH_HUB_CLIENT_SECRET"),
	}
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

		sendRequest(newConnection, `{"name":"RequestGetDevices"}`, func(response string) {
			parseDeviceList(newConnection, response)
		})

		c.OnMessage(func(messageBytes []byte) {
			message := string(messageBytes)
			messageJson := gjson.Parse(message)

			mid := gjson.Get(message, "mid").Int()

			callback := newConnection.Callbacks[mid]
			if callback != nil {
				callback(message)
				delete(newConnection.Callbacks, mid)
			}

			eventName := messageJson.Get("name").String()

			log.Println("Event: " + eventName)
			if eventName == "RequestAuthorize" {
				token := messageJson.Get("payload.token").String()
				userInfo, err := getUserInfo(token, hubAuth)
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
				newConnection.Uuid = messageJson.Get("payload.uuid").String()
				newConnection.Name = messageJson.Get("payload.name").String()
			} else if eventName == "EventDeviceListUpdate" {
				parseDeviceList(newConnection, message)
			} else if eventName == "EventValueUpdate" {
				handleValueUpdate(newConnection, messageJson)
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

		token := gjson.Get(body, "payload.accessToken").String()

		userInfo, err := getUserInfo(token, alexaAuth)
		if err != nil {
			log.Println(err)
			return
		}

		handleAlexaMessage(body, clientConnections, userInfo, c)
	})

	app.Listen(":12345")
}
