module github.com/kvk-innovatie/kvk-bevoegdheden-rest-api

go 1.22.2

toolchain go1.23.3

replace github.com/kvk-innovatie/kvk-bevoegdheden => "C:/Users/Erwin Nieuwlaar/Documents/KVK/Archipels/kvk-bevoegdheden"

require (
	github.com/go-chi/chi/v5 v5.0.7
	github.com/go-chi/cors v1.2.1
	github.com/joho/godotenv v1.5.1
	github.com/kvk-innovatie/kvk-bevoegdheden v0.0.0-20221121144537-5c9012676680
	github.com/unrolled/render v1.4.1
)

require (
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	golang.org/x/oauth2 v0.19.0 // indirect
	golang.org/x/sys v0.2.0 // indirect
)
