module github.com/kartaladev/prblm/mappers/pgxmap

go 1.27

replace github.com/kartaladev/prblm => ../..

require (
	github.com/jackc/pgx/v5 v5.13.0
	github.com/kartaladev/prblm v0.0.0-00010101000000-000000000000
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	golang.org/x/text v0.41.0 // indirect
)
