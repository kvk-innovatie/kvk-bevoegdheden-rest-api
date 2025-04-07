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
	"github.com/go-chi/cors"
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
	if kvkNummer == "" {
		kvkNummer = "90000021"
	}

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
	if kvkNummer == "" {
		kvkNummer = "90000021"
	}

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

	// Build the legal person data map
	legalPerson := map[string]interface{}{
		"legal_person_name":         bevoegdheidUittreksel.Naam,
		"legal_person_id":           "NLNHR." + bevoegdheidUittreksel.KvkNummer,
		"legal_form_type":           bevoegdheidUittreksel.PersoonRechtsvorm,
		"registration_member_state": "NL",
		"registered_address": map[string]interface{}{
			"full_address": bevoegdheidUittreksel.Adres,
		},
		"registration_date":   formatDateISO8601(bevoegdheidUittreksel.RegistratieAanvang),
		"legal_person_status": getLegalPersonStatus(bevoegdheidUittreksel),
	}

	// Add SBI activity if available
	sbiCode := getSbiCode(bevoegdheidUittreksel.SbiActiviteit)
	sbiDescription := getSbiDescription(bevoegdheidUittreksel.SbiActiviteit)
	if sbiCode != "" || sbiDescription != "" {
		legalPersonActivity := map[string]interface{}{}
		if sbiCode != "" {
			legalPersonActivity["code"] = sbiCode
		}
		if sbiDescription != "" {
			legalPersonActivity["description"] = sbiDescription
		}
		legalPerson["legal_person_activity"] = legalPersonActivity
	}

	// Add contact point information only if available
	if bevoegdheidUittreksel.EmailAdres != "" {
		contactPoint := map[string]interface{}{}
		contactPoint["contact_email"] = bevoegdheidUittreksel.EmailAdres
		legalPerson["contact_point"] = contactPoint
	}

	// Only add share_capital if we have meaningful data
	// Omitting this field entirely since we don't have the data

	companyCertificate := map[string]interface{}{
		"legal_person":         legalPerson,
		"legal_representative": extractLegalRepresentatives(*bevoegdheidUittreksel),
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

func getKvkNummer(r *http.Request) string {
	kvkNummer := chi.URLParam(r, "kvkNummer")
	if kvkNummer == "" {
		return "90000021" // Default KVK number
	}
	return kvkNummer
}

func getLegalPersonStatus(b *models.BevoegdheidUittreksel) string {
	if b.DatumUitschrijving != "" {
		return "terminated"
	}
	if b.BijzondereRechtstoestand != "" {
		return "special_status"
	}
	return "active"
}

func getSbiCode(sbiActivity string) string {
	if sbiActivity == "" {
		return ""
	}
	parts := strings.Split(sbiActivity, ", ")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func getSbiDescription(sbiActivity string) string {
	if sbiActivity == "" {
		return ""
	}
	parts := strings.Split(sbiActivity, ", ")
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}

func extractLegalRepresentatives(b models.BevoegdheidUittreksel) []map[string]interface{} {
	representatives := make([]map[string]interface{}, 0)

	// Add natural person representatives
	for _, np := range b.AlleFunctionarissen {
		rep := map[string]interface{}{
			"natural_person": map[string]interface{}{
				"full_name":      np.VolledigeNaam,
				"date_of_birth":  formatDateISO8601(np.Geboortedatum),
				"nationality":    "NL",
				"signatory_rule": getSignatoryRule(np.Functionaris),
			},
		}
		representatives = append(representatives, rep)
	}

	// Add legal person representatives
	for _, rp := range b.AlleRechtspersoonFunctionarissen {
		rep := map[string]interface{}{
			"legal_person": map[string]interface{}{
				"legal_person_name": rp.Naam,
				"legal_person_id":   "NLNHR." + rp.KvkNummer,
				"legal_form_type":   rp.PersoonRechtsvorm,
				"signatory_rule":    getSignatoryRule(rp.Functionaris),
			},
		}
		representatives = append(representatives, rep)
	}

	return representatives
}

func getSignatoryRule(f models.Functionaris) string {
	// Cases where person has full authority to act alone
	switch f.SoortBevoegdheid {
	case "Alleen/zelfstandig bevoegd", "Onbeperkt bevoegd":
		return "alone"
	case "Gezamenlijk bevoegd", "Beperkt bevoegd":
		return "joint"
	}

	// Check volmacht (power of attorney) type
	if f.TypeVolmacht == "Volledige volmacht" {
		return "alone"
	}

	return "unknown"
}

func formatDateISO8601(date string) string {
	if date == "" {
		return ""
	}
	// Convert from DD-MM-YYYY to YYYY-MM-DD
	parts := strings.Split(date, "-")
	if len(parts) != 3 {
		return date
	}
	return fmt.Sprintf("%s-%s-%s", parts[2], parts[1], parts[0])
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
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"}, // Replace with your frontend's URL
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300, // Maximum value for preflight requests caching
	}))

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
	r.Get("/api/lpid", func(w http.ResponseWriter, r *http.Request) {
		identityNP := models.IdentityNP{}
		handleLPID(&identityNP, w, r, rend)
	})

	r.Get("/api/company-certificate/{kvkNummer}", func(w http.ResponseWriter, r *http.Request) {
		identityNP := models.IdentityNP{}
		handleCompanyCertificate(&identityNP, w, r, rend)
	})
	r.Get("/api/company-certificate", func(w http.ResponseWriter, r *http.Request) {
		identityNP := models.IdentityNP{}
		handleCompanyCertificate(&identityNP, w, r, rend)
	})

	r.Post("/api/signatory-right/{kvkNummer}", func(w http.ResponseWriter, r *http.Request) {
		kvkNummer := chi.URLParam(r, "kvkNummer")
		if kvkNummer == "" {
			kvkNummer = "90000021"
		}
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
	r.Post("/api/signatory-right", func(w http.ResponseWriter, r *http.Request) {
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

		httpCode, errorCode, bevoegdheidResponse := doGetBevoegdheid(&identityNP, "90000021")
		if httpCode != 0 {
			sendErrorResponse(w, httpCode, errorCode)
			return
		}
		result := handleSignatoryRight(inputPersonJSON, bevoegdheidResponse)
		if result == "Yes" {
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

		sendErrorResponse(w, http.StatusNotFound, "No match found or not authorized")
	})

	r.Post("/api/por/{kvkNummer}", func(w http.ResponseWriter, r *http.Request) {
		kvkNummer := chi.URLParam(r, "kvkNummer")
		if kvkNummer == "" {
			kvkNummer = "90000021"
		}
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
			fullName := fmt.Sprintf("%s %s %s",
				identityNP.VoorvoegselGeslachtsnaam,
				identityNP.Voornamen,
				identityNP.Geslachtsnaam,
			)
			response := map[string]interface{}{
				"data": map[string]interface{}{
					"fullName":          strings.TrimSpace(fullName),
					"isAuthorized":      "Yes",
					"id":                "NLNHR." + bevoegdheidResponse.BevoegdheidUittreksel.KvkNummer,
					"legal_person_name": bevoegdheidResponse.BevoegdheidUittreksel.Naam,
				},
				"metadata": generateMetadata(),
			}
			rend.JSON(w, http.StatusOK, response)
			return
		}

		sendErrorResponse(w, http.StatusNotFound, "No match found or not authorized")
	})
	r.Post("/api/por", func(w http.ResponseWriter, r *http.Request) {
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

		httpCode, errorCode, bevoegdheidResponse := doGetBevoegdheid(&identityNP, "90000021")
		if httpCode != 0 {
			sendErrorResponse(w, httpCode, errorCode)
			return
		}
		result := handleSignatoryRight(inputPersonJSON, bevoegdheidResponse)
		if result == "Yes" {
			fullName := fmt.Sprintf("%s %s %s",
				identityNP.VoorvoegselGeslachtsnaam,
				identityNP.Voornamen,
				identityNP.Geslachtsnaam,
			)
			response := map[string]interface{}{
				"data": map[string]interface{}{
					"fullName":          strings.TrimSpace(fullName),
					"isAuthorized":      "Yes",
					"id":                "NLNHR." + bevoegdheidResponse.BevoegdheidUittreksel.KvkNummer,
					"legal_person_name": bevoegdheidResponse.BevoegdheidUittreksel.Naam,
				},
				"metadata": generateMetadata(),
			}
			rend.JSON(w, http.StatusOK, response)
			return
		}

		sendErrorResponse(w, http.StatusNotFound, "No match found or not authorized")
	})

	http.ListenAndServe(":3333", r)
}
