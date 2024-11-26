package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	kvkBevoegdheden "github.com/kvk-innovatie/kvk-bevoegdheden"
	"github.com/kvk-innovatie/kvk-bevoegdheden/models"
	"github.com/unrolled/render"

	b64 "encoding/base64"
)

var (
	bevoegdheidResponseCache map[string]*models.BevoegdheidResponse
	requestsPerUser          map[string][]time.Time
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
	enableCaching := os.Getenv("ENABLE_CACHING") == "true"

	env := "prd"
	if os.Getenv("GO_ENV") == "development" || os.Getenv("GO_ENV") == "test" {
		env = "preprd"
	}

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

	bevoegdheidResponse, err, _ := kvkBevoegdheden.GetBevoegdheid(kvkNummer, *identityNP, os.Getenv("LOOKUP_CLIENTID"), os.Getenv("LOOKUP_CLIENTSECRET"), os.Getenv("LOOKUP_AUTHSERVER_URL"), enableCaching, env)

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

	http.ListenAndServe(":3333", r)
}
