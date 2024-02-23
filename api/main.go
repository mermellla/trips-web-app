package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/DIMO-Network/shared"
	"github.com/dimo-network/trips-web-app/api/internal/config"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/template/handlebars/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"
)

type Trip struct {
	ID    string    `json:"id"`
	Start TimeEntry `json:"start"`
	End   TimeEntry `json:"end"`
}

type TimeEntry struct {
	Time string `json:"time"`
}

type TripsResponse struct {
	Trips []Trip `json:"trips"`
}

type HistoryResponse struct {
	Hits struct {
		Hits []struct {
			Source struct {
				Data LocationData `json:"data"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

type LocationData struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

var cacheInstance = cache.New(cache.DefaultExpiration, 10*time.Minute)

type ChallengeResponse struct {
	State     string `json:"state"`
	Challenge string `json:"challenge"`
}

type GraphQLRequest struct {
	Query string `json:"query"`
}

type Vehicle struct {
	TokenID  int64 `json:"tokenId"`
	Earnings struct {
		TotalTokens string `json:"totalTokens"`
	} `json:"earnings"`
	Definition struct {
		Make  string `json:"make"`
		Model string `json:"model"`
		Year  int    `json:"year"`
	} `json:"definition"`
	AftermarketDevice struct {
		Address      string `json:"address"`
		Serial       string `json:"serial"`
		Manufacturer struct {
			Name string `json:"name"`
		} `json:"manufacturer"`
	} `json:"aftermarketDevice"`
	DeviceStatusEntries []DeviceDataEntry `json:"deviceStatusEntries"`
	Trips               []Trip            `json:"trips"`
}

type RawDeviceStatus struct {
	DTC                       map[string]interface{} `json:"dtc"`
	MAF                       map[string]interface{} `json:"maf"`
	VIN                       map[string]interface{} `json:"vin"`
	Cell                      map[string]interface{} `json:"cell"`
	HDOP                      map[string]interface{} `json:"hdop"`
	NSAT                      map[string]interface{} `json:"nsat"`
	WiFi                      map[string]interface{} `json:"wifi"`
	Speed                     map[string]interface{} `json:"speed"`
	Device                    map[string]interface{} `json:"device"`
	RunTime                   map[string]interface{} `json:"runTime"`
	Altitude                  map[string]interface{} `json:"altitude"`
	Timestamp                 map[string]interface{} `json:"timestamp"`
	EngineLoad                map[string]interface{} `json:"engineLoad"`
	IntakeTemp                map[string]interface{} `json:"intakeTemp"`
	CoolantTemp               map[string]interface{} `json:"coolantTemp"`
	EngineSpeed               map[string]interface{} `json:"engineSpeed"`
	ThrottlePosition          map[string]interface{} `json:"throttlePosition"`
	LongTermFuelTrim1         map[string]interface{} `json:"longTermFuelTrim1"`
	BarometricPressure        map[string]interface{} `json:"barometricPressure"`
	ShortTermFuelTrim1        map[string]interface{} `json:"shortTermFuelTrim1"`
	AcceleratorPedalPositionD map[string]interface{} `json:"acceleratorPedalPositionD"`
	AcceleratorPedalPositionE map[string]interface{} `json:"acceleratorPedalPositionE"`
}

type DeviceDataEntry struct {
	SignalName string
	Value      interface{}
	Timestamp  string
	Source     string
}

type DeviceStatusEntries []DeviceDataEntry

func extractLocationData(historyData HistoryResponse) []LocationData {
	var locations []LocationData
	for _, hit := range historyData.Hits.Hits {
		locData := LocationData{
			Latitude:  hit.Source.Data.Latitude,
			Longitude: hit.Source.Data.Longitude,
		}
		locations = append(locations, locData)
	}
	return locations
}

func queryDeviceDataHistory(tokenID int64, startTime string, endTime string, settings *config.Settings, c *fiber.Ctx) ([]LocationData, error) {
	var historyResponse HistoryResponse

	sessionCookie := c.Cookies("session_id")
	privilegeTokenKey := "privilegeToken_" + sessionCookie

	// Retrieve the privilege token from the cache
	token, found := cacheInstance.Get(privilegeTokenKey)
	if !found {
		return nil, errors.New("privilege token not found in cache")
	}

	ddUrl := fmt.Sprintf("%s/v1/vehicle/%d/history?start=%s&end=%s", settings.DeviceDataAPIBaseURL, tokenID, url.QueryEscape(startTime), url.QueryEscape(endTime))

	req, err := http.NewRequest("GET", ddUrl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&historyResponse); err != nil {
		return nil, err
	}

	locations := extractLocationData(historyResponse)
	return locations, nil
}

func convertToGeoJSON(locations []LocationData) GeoJSONFeatureCollection {
	var coordinates [][]float64
	for _, loc := range locations {
		coordinates = append(coordinates, []float64{loc.Longitude, loc.Latitude})
	}

	geoJSON := GeoJSONFeatureCollection{
		Type: "FeatureCollection",
		Features: []GeoJSONFeature{
			{
				Type: "Feature",
				Geometry: GeoJSONGeometry{
					Type:        "LineString",
					Coordinates: coordinates,
				},
			},
		},
	}

	return geoJSON
}

type GeoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []GeoJSONFeature `json:"features"`
}

type GeoJSONFeature struct {
	Type     string          `json:"type"`
	Geometry GeoJSONGeometry `json:"geometry"`
}

type GeoJSONGeometry struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"`
}

func queryTripsAPI(tokenID int64, settings *config.Settings, c *fiber.Ctx) ([]Trip, error) {
	var tripsResponse TripsResponse

	sessionCookie := c.Cookies("session_id")
	privilegeTokenKey := "privilegeToken_" + sessionCookie

	// Retrieve the privilege token from the cache
	token, found := cacheInstance.Get(privilegeTokenKey)
	if !found {
		return nil, errors.New("privilege token not found in cache")
	}

	url := fmt.Sprintf("%s/vehicle/%d/trips", settings.TripsAPIBaseURL, tokenID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&tripsResponse); err != nil {
		return nil, err
	}

	// Log each trip ID
	for _, trip := range tripsResponse.Trips {
		log.Info().Msgf("Trip ID: %s", trip.ID)
	}

	return tripsResponse.Trips, nil
}

func handleMapDataForTrip(c *fiber.Ctx, settings *config.Settings, tripID string) error {
	ethAddress := c.Locals("ethereum_address").(string)

	// Fetch vehicles associated with the Ethereum address
	vehicles, err := queryIdentityAPIForVehicles(ethAddress, settings)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if len(vehicles) == 0 {
		return c.Status(fiber.StatusNotFound).SendString("No vehicles found")
	}

	var tokenID int64
	var startTime, endTime string
	tripFound := false

	for _, vehicle := range vehicles {
		trips, err := queryTripsAPI(vehicle.TokenID, settings, c)
		if err != nil {
			continue
		}

		for _, trip := range trips {
			if trip.ID == tripID {
				tokenID = vehicle.TokenID
				startTime = trip.Start.Time
				endTime = trip.End.Time
				tripFound = true
				break
			}
		}

		if tripFound {
			break
		}
	}

	if !tripFound {
		return c.Status(fiber.StatusNotFound).SendString("Trip not found")
	}

	// Fetch historical data for the specific trip
	locations, err := queryDeviceDataHistory(tokenID, startTime, endTime, settings, c)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch historical data: " + err.Error()})
	}

	// Convert the historical data to GeoJSON
	geoJSON := convertToGeoJSON(locations)
	return c.JSON(geoJSON)
}

func processRawDeviceStatus(rawDeviceStatus RawDeviceStatus) DeviceStatusEntries {
	var entries DeviceStatusEntries

	v := reflect.ValueOf(rawDeviceStatus)
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		name := v.Type().Field(i).Name

		if data, ok := field.Interface().(map[string]interface{}); ok {
			if value, exists := data["value"]; exists {
				// Check if value is a nested map and process each entry
				switch valueTyped := value.(type) {
				case map[string]interface{}:
					for k, v := range valueTyped {
						entries = append(entries, DeviceDataEntry{
							SignalName: fmt.Sprintf("%s.%s", name, k),
							Value:      fmt.Sprintf("%v", v),
							Timestamp:  fmt.Sprintf("%v", data["timestamp"]),
							Source:     fmt.Sprintf("%v", data["source"]),
						})
					}
				default:
					entries = append(entries, DeviceDataEntry{
						SignalName: name,
						Value:      fmt.Sprintf("%v", value),
						Timestamp:  fmt.Sprintf("%v", data["timestamp"]),
						Source:     fmt.Sprintf("%v", data["source"]),
					})
				}
			} else {
				entries = append(entries, DeviceDataEntry{
					SignalName: name,
					Value:      "",
					Timestamp:  fmt.Sprintf("%v", data["timestamp"]),
					Source:     fmt.Sprintf("%v", data["source"]),
				})
			}
		}
	}
	return entries
}

func ExtractEthereumAddressFromToken(tokenString string) (string, error) {
	// Parsing the token without validating its signature
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		fmt.Println("Error parsing token:", err)
		return "", fmt.Errorf("error parsing token")
	}

	// Asserting the type of the claims to access the data
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid claims type")
	}

	ethAddress, ok := claims["ethereum_address"].(string)
	if !ok {
		return "", errors.New("ethereum address not found in JWT")
	}

	return ethAddress, nil
}

func AuthMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Retrieve the session_id from the request cookie
		sessionCookie := c.Cookies("session_id")

		// Check if the session_id is in the cache
		jwtToken, found := cacheInstance.Get(sessionCookie)
		if !found {
			return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
		}

		ethAddress, err := ExtractEthereumAddressFromToken(jwtToken.(string))
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).SendString("Invalid token: " + err.Error())
		}

		c.Locals("ethereum_address", ethAddress)

		return c.Next()
	}
}

func HandleGetVehicles(c *fiber.Ctx, settings *config.Settings) error {
	ethAddress := c.Locals("ethereum_address").(string)

	vehicles, err := queryIdentityAPIForVehicles(ethAddress, settings)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error querying identity API: " + err.Error())
	}

	for i := range vehicles {
		// fetch raw status
		rawStatus, err := queryDeviceDataAPI(vehicles[i].TokenID, settings, c)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get raw device status")
			return c.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("Failed to get raw device status for vehicle with TokenID: %d", vehicles[i].TokenID))
		}
		vehicles[i].DeviceStatusEntries = processRawDeviceStatus(rawStatus)

		// fetch trips for each vehicle
		trips, err := queryTripsAPI(vehicles[i].TokenID, settings, c)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get trips for vehicle")
			continue
		}
		vehicles[i].Trips = trips
	}

	return c.Render("vehicles", fiber.Map{
		"Title":    "My Vehicles",
		"Vehicles": vehicles,
	})
}

func queryIdentityAPIForVehicles(ethAddress string, settings *config.Settings) ([]Vehicle, error) {
	// GraphQL query
	graphqlQuery := `{
        vehicles(first: 10, filterBy: { owner: "` + ethAddress + `" }) {
            nodes {
                tokenId,
                earnings {
                    totalTokens
                },
                definition {
                    make,
                    model,
                    year
                },
                aftermarketDevice {
                    address,
                    serial,
                    manufacturer {
                        name
                    }
                }
            }
        }
    }`

	// GraphQL request
	requestPayload := GraphQLRequest{Query: graphqlQuery}
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, err
	}

	// POST request
	req, err := http.NewRequest("POST", settings.IdentityAPIURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var vehicleResponse struct {
		Data struct {
			Vehicles struct {
				Nodes []Vehicle `json:"nodes"`
			} `json:"vehicles"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &vehicleResponse); err != nil {
		return nil, err
	}

	vehicles := make([]Vehicle, 0, len(vehicleResponse.Data.Vehicles.Nodes))
	for _, v := range vehicleResponse.Data.Vehicles.Nodes {
		vehicles = append(vehicles, Vehicle{
			TokenID:           v.TokenID,
			Earnings:          v.Earnings,
			Definition:        v.Definition,
			AftermarketDevice: v.AftermarketDevice,
		})
	}

	return vehicles, nil
}

func queryDeviceDataAPI(tokenID int64, settings *config.Settings, c *fiber.Ctx) (RawDeviceStatus, error) {
	var rawDeviceStatus RawDeviceStatus

	sessionCookie := c.Cookies("session_id")
	privilegeTokenKey := "privilegeToken_" + sessionCookie

	// Retrieve the privilege token from the cache
	token, found := cacheInstance.Get(privilegeTokenKey)
	if !found {
		return rawDeviceStatus, errors.New("privilege token not found in cache")
	}

	url := fmt.Sprintf("%s/vehicle/%d/status-raw", settings.DeviceDataAPIBaseURL, tokenID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return rawDeviceStatus, err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return rawDeviceStatus, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&rawDeviceStatus); err != nil {
		return rawDeviceStatus, err
	}

	return rawDeviceStatus, nil
}

func HandleGenerateChallenge(c *fiber.Ctx, settings *config.Settings) error {
	address := c.FormValue("address")

	formData := url.Values{}
	formData.Add("client_id", settings.ClientID)
	formData.Add("domain", settings.Domain)
	formData.Add("scope", settings.Scope)
	formData.Add("response_type", settings.ResponseType)
	formData.Add("address", address)

	encodedFormData := formData.Encode()
	reqURL := settings.AuthURL

	resp, err := http.Post(reqURL, "application/x-www-form-urlencoded", strings.NewReader(encodedFormData))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Failed to make request to external service")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error reading external response")
	}

	var apiResp ChallengeResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error processing response from external service")
	}

	if apiResp.State == "" || apiResp.Challenge == "" {
		return c.Status(fiber.StatusInternalServerError).SendString("State or Challenge incomplete from external service")
	}

	return c.JSON(apiResp)
}

func HandleSubmitChallenge(c *fiber.Ctx, settings *config.Settings) error {
	state := c.FormValue("state")
	signature := c.FormValue("signature")

	log.Info().Msgf("State: %s, Signature: %s", state, signature)

	formData := url.Values{}
	formData.Add("client_id", settings.ClientID)
	formData.Add("domain", settings.Domain)
	formData.Add("grant_type", settings.GrantType)
	formData.Add("state", state)
	formData.Add("signature", signature)

	encodedFormData := formData.Encode()
	reqURL := settings.SubmitChallengeURL

	resp, err := http.Post(reqURL, "application/x-www-form-urlencoded", strings.NewReader(encodedFormData))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Failed to make request to external service")
	}
	defer resp.Body.Close()

	// Check the HTTP status code here
	if resp.StatusCode >= 300 {
		return c.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("Received non-success status code: %d", resp.StatusCode))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Failed to read response from external service")
	}

	var responseMap map[string]interface{}
	if err := json.Unmarshal(respBody, &responseMap); err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error processing response")
	}

	log.Info().Msgf("Response from submit challenge: %+v", responseMap) //debugging

	token, exists := responseMap["id_token"]
	if !exists {
		return c.Status(fiber.StatusInternalServerError).SendString("Token not found in response")
	}

	//jwt token storage
	sessionID := uuid.New().String()
	cacheInstance.Set(sessionID, token, 2*time.Hour)

	cookie := new(fiber.Cookie)
	cookie.Name = "session_id"
	cookie.Value = sessionID
	cookie.Expires = time.Now().Add(2 * time.Hour)
	cookie.HTTPOnly = true
	cookie.Domain = "localhost"

	c.Cookie(cookie)

	return c.JSON(fiber.Map{"message": "Challenge accepted and session started!", "id_token": token})
}

func HandleTokenExchange(c *fiber.Ctx, settings *config.Settings) error {

	ethAddress := c.Locals("ethereum_address").(string)
	vehicles, err := queryIdentityAPIForVehicles(ethAddress, settings)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Failed to query vehicles")
	}
	if len(vehicles) == 0 {
		return c.Status(fiber.StatusInternalServerError).SendString("No vehicles found")
	}
	tokenId := vehicles[0].TokenID

	log.Info().Msg("HandleTokenExchange called")

	sessionCookie := c.Cookies("session_id")

	jwtToken, found := cacheInstance.Get(sessionCookie)
	if !found {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized: No session found")
	}

	idToken, ok := jwtToken.(string)
	if !ok {
		return c.Status(fiber.StatusInternalServerError).SendString("Internal Error: Token format is invalid")
	}

	log.Info().Msgf("JWT being sent: %s", idToken)

	nftContractAddress := "0xbA5738a18d83D41847dfFbDC6101d37C69c9B0cF"
	privileges := []int{4}
	requestBody := map[string]interface{}{
		"nftContractAddress": nftContractAddress,
		"privileges":         privileges,
		"tokenId":            tokenId,
	}

	requestBodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error marshaling request body")
	}

	log.Info().Msgf("Request body being sent: %s", string(requestBodyBytes))

	req, err := http.NewRequest("POST", settings.TokenExchangeAPIURL, bytes.NewBuffer(requestBodyBytes))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error creating new request")
	}

	req.Header.Set("Authorization", "Bearer "+idToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error sending request to token exchange API")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Error reading response from token exchange API")
	}

	var responseMap map[string]interface{}
	if err := json.Unmarshal(respBody, &responseMap); err != nil {
		log.Error().Err(err).Msg("Error processing response")
		return c.Status(fiber.StatusInternalServerError).SendString("Error processing response")
	}

	token, exists := responseMap["token"]
	if !exists {
		return c.Status(fiber.StatusInternalServerError).SendString("Token not found in response from token exchange API")
	}

	// privilege token storage
	privilegeTokenKey := "privilegeToken_" + sessionCookie
	cacheInstance.Set(privilegeTokenKey, token, cache.DefaultExpiration)

	log.Info().Msgf("Token exchange successful: %s", token)
	return c.JSON(fiber.Map{"token": token})
}

func ErrorHandler(ctx *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	message := "Internal Server Error"

	var e *fiber.Error
	if errors.As(err, &e) {
		code = e.Code
		message = e.Message
	}

	log.Error().Err(err).Int("code", code).Str("path", ctx.Path()).Msg("Error occurred")

	return ctx.Status(code).JSON(fiber.Map{
		"error":   true,
		"message": message,
	})
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	fmt.Print("Server is starting...")

	settings, err := shared.LoadConfig[config.Settings]("settings.yaml")
	if err != nil {
		log.Fatal().Err(err).Msg("could not load settings")
	}

	level, err := zerolog.ParseLevel(settings.LogLevel)
	if err != nil {
		log.Fatal().Err(err).Msgf("could not parse LOG_LEVEL: %s", settings.LogLevel)
	}
	zerolog.SetGlobalLevel(level)

	engine := handlebars.New("../views", ".hbs")

	app := fiber.New(fiber.Config{
		ErrorHandler: ErrorHandler,
		Views:        engine,
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins:     "http://localhost:3000",
		AllowMethods:     "GET,POST,HEAD,PUT,DELETE,PATCH",
		AllowHeaders:     "Accept, Content-Type, Content-Length, Authorization",
		AllowCredentials: true,
	}))

	// Protected route
	app.Get("/api/vehicles/me", AuthMiddleware(), func(c *fiber.Ctx) error {
		return HandleGetVehicles(c, &settings)
	})

	// Public Routes
	app.Post("/auth/web3/generate_challenge", func(c *fiber.Ctx) error {
		return HandleGenerateChallenge(c, &settings)
	})
	app.Post("/auth/web3/submit_challenge", func(c *fiber.Ctx) error {
		return HandleSubmitChallenge(c, &settings)
	})

	app.Post("/api/token_exchange", AuthMiddleware(), func(c *fiber.Ctx) error {
		return HandleTokenExchange(c, &settings)
	})

	app.Get("/api/trip/:tripID", func(c *fiber.Ctx) error {
		tripID := c.Params("tripID")
		return handleMapDataForTrip(c, &settings, tripID)
	})

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("can you see this")
	})

	log.Info().Msgf("Starting server on port %s", settings.Port)
	if err := app.Listen(":" + settings.Port); err != nil {
		log.Fatal().Err(err).Msg("Server failed to start")
	}
}
