module thyris-sz

go 1.23

require (
	github.com/joho/godotenv v1.5.1
	github.com/redis/go-redis/v9 v9.7.0
	github.com/xeipuuv/gojsonschema v1.2.0
	github.com/thyrisAI/safe-zone/pkg/tszclient-go v0.0.0
	gorm.io/driver/postgres v1.5.11
	gorm.io/gorm v1.25.12
)

require (
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/pgx/v5 v5.5.5 // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20180127040702-4e3ac2762d5f // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)

// Make the tszclient-go module importable from tests and other packages within this repo.
replace github.com/thyrisAI/safe-zone/pkg/tszclient-go => ./pkg/tszclient-go
