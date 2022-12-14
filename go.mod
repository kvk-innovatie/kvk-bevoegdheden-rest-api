module github.com/kvk-innovatie/kvk-bevoegdheden-rest-api

go 1.18

replace gitlab.com/signicat/orange-stack/ciam/kvk-issuer/kvk-extract => /Users/dbxmlm/gitlab/signicat/kvk-issuer/kvk-extract

require (
	github.com/go-chi/chi/v5 v5.0.7
	github.com/kvk-innovatie/kvk-bevoegdheden v0.0.0-20221121144537-5c9012676680
	github.com/unrolled/render v1.4.1
	gitlab.com/signicat/orange-stack/ciam/kvk-issuer/kvk-extract v0.0.0-00010101000000-000000000000
)

require (
	github.com/beevik/etree v1.1.0 // indirect
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/uuid v1.3.0 // indirect
	golang.org/x/net v0.2.0 // indirect
	golang.org/x/oauth2 v0.2.0 // indirect
	golang.org/x/sys v0.2.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
)
