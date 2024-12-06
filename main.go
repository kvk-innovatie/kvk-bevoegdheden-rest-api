package main

import (
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
	kvkBevoegdheden "github.com/kvk-innovatie/kvk-bevoegdheden"
	"github.com/kvk-innovatie/kvk-bevoegdheden/models"
	"github.com/unrolled/render"
)

var (
	bevoegdheidResponseCache map[string]*models.BevoegdheidResponse
	requestsPerUser          map[string][]time.Time
	clientID                 string
	clientSecret             string
	authServerURL            string
	enableCaching            bool
	env                      string
)

const (
	maxRequestsPerUser          = 5000
	maxRequestsPerUserLongTerm  = 15000
	maxRequestsInterval         = time.Duration(time.Hour) * 24
	maxRequestsIntervalLongTerm = time.Duration(time.Hour) * 24 * 30
)

func init() {
	bevoegdheidResponseCache = make(map[string]*models.BevoegdheidResponse)
	requestsPerUser = make(map[string][]time.Time)
	godotenv.Load()
	var err error
	enableCachingStr := os.Getenv("ENABLE_CACHING")
	enableCaching, err = strconv.ParseBool(enableCachingStr)
	if err != nil {
		log.Printf("Invalid boolean value for ENABLE_CACHING: %s. Defaulting to false.", enableCachingStr)
		enableCaching = false // Default to false if parsing fails
	}
	clientID = os.Getenv("SIGNICAT_CLIENTID")
	clientSecret = os.Getenv("SIGNICAT_CLIENTSECRET")
	authServerURL = os.Getenv("SIGNICAT_AUTHSERVER_URL")
	env = "prod"
}

func renderJSON(w http.ResponseWriter, status int, data interface{}) {
	jsonData, err := json.Marshal(data)

	if err != nil {
		panic(err)
	}

	w.WriteHeader(status)
	w.Write(jsonData)
}

func sendErrorResponse(w http.ResponseWriter, httpCode int, errorCode string) {
	renderJSON(w, httpCode, map[string]string{
		"error": errorCode,
	})
}

func contains(s []models.RechtspersoonFunctionaris, e models.RechtspersoonFunctionaris) int {
	for index, a := range s {
		if a.KvkNummer == e.KvkNummer {
			return index
		}
	}
	return -1
}

func filterRechtspersonen(bevoegdheidUittreksel *models.BevoegdheidUittreksel) {
	// filter out doubles (kvk nummers that appear multiple times)
	rpfen := []models.RechtspersoonFunctionaris{}
	for _, rp := range bevoegdheidUittreksel.AlleRechtspersoonFunctionarissen {
		index := contains(rpfen, rp)
		if index >= 0 {
			if rpfen[index].Importance < rp.Importance {
				rpfen[index] = rp
			}
		} else {
			rpfen = append(rpfen, rp)
		}
	}
	bevoegdheidUittreksel.AlleRechtspersoonFunctionarissen = rpfen
}

func getEncodedNP(iNP *models.IdentityNP) string {
	strNP := iNP.Voornamen + iNP.VoorvoegselGeslachtsnaam + iNP.Geslachtsnaam + iNP.Geboortedatum
	return b64.StdEncoding.EncodeToString([]byte(strNP))
}
func doGetBevoegdheid(identityNP *models.IdentityNP, kvkNummer string) (httpCode int, errorCode string, bevoegdheidResponse *models.BevoegdheidResponse) {

	if bevoegdheidResponseCache[kvkNummer] != nil {
		pm := bevoegdheidResponseCache[kvkNummer].BevoegdheidUittreksel.Peilmoment
		peilmoment, err := time.Parse("01-02-2006", pm[5:7]+"-"+pm[8:10]+"-"+pm[:4])
		if err == nil {
			threeDaysAgo := time.Now().AddDate(0, 0, -5)
			if peilmoment.After(threeDaysAgo) {
				return 0, "", bevoegdheidResponseCache[kvkNummer]
			}
		}
	}

	encNP := getEncodedNP(identityNP)
	requestCount := requestsPerUser[encNP]
	oneDayAgo := time.Now().Add(-maxRequestsInterval)
	oneMonthAgo := time.Now().Add(-maxRequestsIntervalLongTerm)
	if len(requestCount) >= maxRequestsPerUser && requestCount[len(requestCount)-maxRequestsPerUser].After(oneDayAgo) {
		return 400, "limit-exceeded", nil
	} else if len(requestCount) >= maxRequestsPerUserLongTerm && requestCount[len(requestCount)-maxRequestsPerUserLongTerm].After(oneMonthAgo) {
		return 400, "limit-exceeded-lt", nil
	}

	bevoegdheidResponse, err, _ := kvkBevoegdheden.GetBevoegdheid(kvkNummer, *identityNP, clientID, clientSecret, authServerURL, enableCaching, env)

	requestsPerUser[encNP] = append(requestsPerUser[encNP], time.Now())
	if len(requestsPerUser[encNP]) > maxRequestsPerUserLongTerm {
		requestsPerUser[encNP] = requestsPerUser[encNP][1:]
	}

	if err == kvkBevoegdheden.ErrInschrijvingNotFound {
		return 404, "inschrijving-not-found", nil
	} else if err == kvkBevoegdheden.ErrInvalidInput {
		return 400, "invalid-input", nil
	} else if err == kvkBevoegdheden.ErrParseInschrijving {
		return 400, "parse-error", nil
	} else if err != nil {
		return 500, "internal-server-error", nil
	}

	filterRechtspersonen(bevoegdheidResponse.BevoegdheidUittreksel)

	bevoegdheidResponseCache[bevoegdheidResponse.BevoegdheidUittreksel.KvkNummer] = bevoegdheidResponse

	return 0, "", bevoegdheidResponse
}

func handleLPID(identityNP *models.IdentityNP, w http.ResponseWriter, r *http.Request, rend *render.Render) {
	kvkNummer := chi.URLParam(r, "kvkNummer")

	bevoegdheidResponse, err, _ := kvkBevoegdheden.GetLPID(kvkNummer, *identityNP, clientID, clientSecret, authServerURL, enableCaching, env)

	if err == kvkBevoegdheden.ErrInschrijvingNotFound {
		rend.JSON(w, http.StatusNotFound, err)
		return
	} else if err == kvkBevoegdheden.ErrInvalidInput {
		rend.JSON(w, http.StatusBadRequest, err)
		return
	} else if err != nil {
		rend.JSON(w, http.StatusInternalServerError, err)
		return
	}

	lpidResponse := map[string]interface{}{
		"data": map[string]interface{}{
			"id":                "NLNHR." + bevoegdheidResponse.BevoegdheidUittreksel.KvkNummer,
			"legal_person_name": bevoegdheidResponse.BevoegdheidUittreksel.Naam,
		},
		"metadata": generateMetadata(),
	}

	rend.JSON(w, http.StatusOK, lpidResponse)
}
func handleCompanyCertificate(identityNP *models.IdentityNP, w http.ResponseWriter, r *http.Request, rend *render.Render) {
	kvkNummer := chi.URLParam(r, "kvkNummer")

	bevoegdheidResponse, err, _ := kvkBevoegdheden.GetCompanyCertificate(kvkNummer, *identityNP, clientID, clientSecret, authServerURL, enableCaching, env)

	if err == kvkBevoegdheden.ErrInschrijvingNotFound {
		rend.JSON(w, http.StatusNotFound, err)
		return
	} else if err == kvkBevoegdheden.ErrInvalidInput {
		rend.JSON(w, http.StatusBadRequest, err)
		return
	} else if err != nil {
		rend.JSON(w, http.StatusInternalServerError, err)
		return
	}

	bevoegdheidUittreksel := bevoegdheidResponse.BevoegdheidUittreksel

	companyCertificate := map[string]interface{}{
		"id":                   "NLNHR." + bevoegdheidUittreksel.KvkNummer,
		"legal_person_name":    bevoegdheidUittreksel.Naam,
		"legal_form":           bevoegdheidUittreksel.PersoonRechtsvorm,
		"registration_number":  bevoegdheidUittreksel.KvkNummer,
		"registered_country":   "NL",
		"registered_office":    bevoegdheidUittreksel.Adres,
		"postal_address":       bevoegdheidUittreksel.Adres,
		"electronic_address":   bevoegdheidUittreksel.EmailAdres,
		"date_of_registration": bevoegdheidUittreksel.RegistratieAanvang,
		"authorized_persons":   extractAuthorizedPersons(*bevoegdheidUittreksel),
	}

	companyCertificateResponse := map[string]interface{}{
		"data":     companyCertificate,
		"metadata": generateMetadata(),
	}

	rend.JSON(w, http.StatusOK, companyCertificateResponse)
}

func handleSignatoryRight(inputPersonJSON []byte, bevoegdheidResponse *models.BevoegdheidResponse) string {
	// Parse the inputPerson JSON into an IdentityNP struct
	var inputPerson models.IdentityNP
	err := json.Unmarshal(inputPersonJSON, &inputPerson)
	if err != nil {
		log.Printf("Error unmarshaling inputPerson JSON: %v\n", err)
		return "Error processing input"
	}
	log.Printf("Processing inputPerson: %+v\n", inputPerson)

	// Validate that bevoegdheidResponse and its subfields are not nil
	if bevoegdheidResponse == nil {
		log.Println("bevoegdheidResponse is nil")
		return "Error processing response"
	}
	if bevoegdheidResponse.BevoegdheidUittreksel == nil {
		log.Println("bevoegdheidResponse.BevoegdheidUittreksel is nil")
		return "Error processing response"
	}
	log.Println("bevoegdheidResponse and bevoegdheidUittreksel are valid")

	// Flag to track whether a match is found
	matchFound := false

	// Iterate over all natural person functionaries
	for index, functionaris := range bevoegdheidResponse.BevoegdheidUittreksel.AlleFunctionarissen {
		log.Printf("Checking functionaris at index %d: %+v\n", index, functionaris)

		// Check if geslachtsnaam matches
		if functionaris.Geslachtsnaam != inputPerson.Geslachtsnaam {
			log.Printf("Mismatch in geslachtsnaam: input=%s, functionaris=%s\n",
				inputPerson.Geslachtsnaam, functionaris.Geslachtsnaam)
			continue
		}
		log.Println("Geslachtsnaam matches")

		// Check if voornamen matches
		if functionaris.Voornamen != inputPerson.Voornamen {
			log.Printf("Mismatch in voornamen: input=%s, functionaris=%s\n",
				inputPerson.Voornamen, functionaris.Voornamen)
			continue
		}
		log.Println("Voornamen matches")

		// Check if geboortedatum matches
		if functionaris.Geboortedatum != inputPerson.Geboortedatum {
			log.Printf("Mismatch in geboortedatum: input=%s, functionaris=%s\n",
				inputPerson.Geboortedatum, functionaris.Geboortedatum)
			continue
		}
		log.Println("Geboortedatum matches")

		// Check if isBevoegd is "Ja" or "True"
		if functionaris.Interpretatie.IsBevoegd != "Ja" && functionaris.Interpretatie.IsBevoegd != "True" {
			log.Printf("isBevoegd check failed: functionaris.Interpretatie.IsBevoegd=%s\n",
				functionaris.Interpretatie.IsBevoegd)
			continue
		}
		log.Println("isBevoegd check passed")

		// If all checks pass for this functionaris
		log.Printf("Match found for inputPerson: %s\n", inputPersonJSON)
		matchFound = true
	}

	// Final decision based on matchFound flag
	if matchFound {
		log.Println("At least one match found for inputPerson in bevoegdheidResponse")
		return "Yes"
	}

	log.Println("No match found for inputPerson in bevoegdheidResponse")
	return "No"
}

func extractAuthorizedPersons(bevoegdheidUittreksel models.BevoegdheidUittreksel) []map[string]interface{} {
	prunedAuthorizedPersons := []map[string]interface{}{}
	for _, person := range bevoegdheidUittreksel.AlleFunctionarissen {
		prunedAuthorizedPersons = append(prunedAuthorizedPersons, map[string]interface{}{
			"full_name":     person.Voornamen + " " + person.Geslachtsnaam,
			"date_of_birth": person.Geboortedatum,
		})
	}
	return prunedAuthorizedPersons
}
func generateMetadata() map[string]interface{} {
	return map[string]interface{}{
		"issuing_authority_name": "Kamer van Koophandel",
		"issuer_id":              "NLNHR.59581883",
		"issuing_country":        "NL",
	}
}

func main() {
	// runAll()
	r := chi.NewRouter()
	rend := render.New()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/api/test-inschrijvingen", func(w http.ResponseWriter, r *http.Request) {
		files, err := ioutil.ReadDir("./cache-inschrijvingen/")
		if err != nil {
			rend.JSON(w, http.StatusNotFound, err)
		}
		fileNames := []string{}
		for _, file := range files {
			if !file.IsDir() {
				fn := strings.TrimSuffix(file.Name(), ".json")
				fileNames = append(fileNames, fn)
			}
		}
		rend.JSON(w, http.StatusOK, fileNames)
	})

	r.Post("/api/bevoegdheid/{kvkNummer}", func(w http.ResponseWriter, r *http.Request) {
		kvkNummer := chi.URLParam(r, "kvkNummer")
		identityNP := models.IdentityNP{}
		json.NewDecoder(r.Body).Decode(&identityNP)

		httpCode, errorCode, bevoegdheidResponse := doGetBevoegdheid(&identityNP, kvkNummer)
		if httpCode != 0 {
			sendErrorResponse(w, httpCode, errorCode)
			return
		}

		rend.JSON(w, http.StatusOK, bevoegdheidResponse)
	})
	r.Get("/api/lpid/{kvkNummer}", func(w http.ResponseWriter, r *http.Request) {
		identityNP := models.IdentityNP{}
		handleLPID(&identityNP, w, r, rend)
	})
	r.Get("/api/company-certificate/{kvkNummer}", func(w http.ResponseWriter, r *http.Request) {
		identityNP := models.IdentityNP{}
		handleCompanyCertificate(&identityNP, w, r, rend)
	})

	r.Post("/api/signatory-right/{kvkNummer}", func(w http.ResponseWriter, r *http.Request) {
		kvkNummer := chi.URLParam(r, "kvkNummer")
		identityNP := models.IdentityNP{}
		err := json.NewDecoder(r.Body).Decode(&identityNP)
		if err != nil {
			log.Printf("Error decoding JSON: %v\n", err)
			sendErrorResponse(w, http.StatusBadRequest, "Invalid JSON")
			return
		}

		// Log the inputPerson as JSON
		inputPersonJSON, err := json.Marshal(identityNP)
		if err != nil {
			log.Printf("Error marshaling JSON: %v\n", err)
			sendErrorResponse(w, http.StatusInternalServerError, "Error processing input")
			return
		}

		httpCode, errorCode, bevoegdheidResponse := doGetBevoegdheid(&identityNP, kvkNummer)
		if httpCode != 0 {
			sendErrorResponse(w, httpCode, errorCode)
			return
		}
		result := handleSignatoryRight(inputPersonJSON, bevoegdheidResponse)
		if result == "Yes" {
			// If a match is found, construct the response with the full name
			fullName := fmt.Sprintf("%s %s %s",
				identityNP.VoorvoegselGeslachtsnaam,
				identityNP.Voornamen,
				identityNP.Geslachtsnaam,
			)
			response := map[string]string{
				"fullName":     strings.TrimSpace(fullName),
				"isAuthorized": "Yes",
			}
			rend.JSON(w, http.StatusOK, response)
			return
		}

		// If no match is found, send an appropriate error response
		sendErrorResponse(w, http.StatusNotFound, "No match found or not authorized")
	})
	http.ListenAndServe(":3333", r)
}
