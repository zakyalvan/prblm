module github.com/kartaladev/prblm/internal/plugintest

go 1.27

// This module is never published: it tests the mapper plugins in this
// repository together, so no published plugin has to require another, and
// it keeps these replaces after the release.
replace (
	github.com/kartaladev/prblm => ../..
	github.com/kartaladev/prblm/mappers/gormmap => ../../mappers/gormmap
	github.com/kartaladev/prblm/mappers/pgxmap => ../../mappers/pgxmap
)

require (
	github.com/jackc/pgx/v5 v5.13.0
	github.com/kartaladev/prblm v0.0.0-00010101000000-000000000000
	github.com/kartaladev/prblm/mappers/gormmap v0.0.0-00010101000000-000000000000
	github.com/kartaladev/prblm/mappers/pgxmap v0.0.0-00010101000000-000000000000
	gorm.io/driver/postgres v1.6.3
	gorm.io/gorm v1.31.2
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
