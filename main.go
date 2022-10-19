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
	kvkExtract "github.com/privacybydesign/kvk-extract"
	"github.com/privacybydesign/kvk-extract/models"
	"github.com/unrolled/render"
)

func main() {
	// runAll()
	r := chi.NewRouter()
	rend := render.New()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/api/test-extracts", func(w http.ResponseWriter, r *http.Request) {
		files, err := ioutil.ReadDir("./cache-extract/")
		if err != nil {
			rend.JSON(w, http.StatusNotFound, err)
		}
		fileNames := []string{}
		for _, file := range files {
			if !file.IsDir() {
				fn := strings.TrimSuffix(file.Name(), ".xml")
				fileNames = append(fileNames, fn)
			}
		}
		rend.JSON(w, http.StatusOK, fileNames)
	})

	r.Post("/api/bevoegdheid/{kvkNummer}", func(w http.ResponseWriter, r *http.Request) {
		kvkNummer := chi.URLParam(r, "kvkNummer")
		identityNP := models.IdentityNP{}
		json.NewDecoder(r.Body).Decode(&identityNP)

		bevoegdheidResponse, err := kvkExtract.GetBevoegdheid(kvkNummer, identityNP, os.Getenv("CERTIFICATE_KVK"), os.Getenv("PRIVATE_KEY_KVK"), true, "preprd")

		if err == kvkExtract.ErrExtractNotFound {
			rend.JSON(w, http.StatusNotFound, err)
			return
		} else if err == kvkExtract.ErrInvalidInput {
			rend.JSON(w, http.StatusBadRequest, err)
			return
		} else if err != nil {
			rend.JSON(w, http.StatusInternalServerError, err)
			return
		}

		rend.JSON(w, http.StatusOK, bevoegdheidResponse)
	})

	http.ListenAndServe(":3333", r)
}
