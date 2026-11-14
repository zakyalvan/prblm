module github.com/kartaladev/prblm/mappers/gormmap

go 1.27

replace github.com/kartaladev/prblm => ../..

require (
	github.com/kartaladev/prblm v0.0.0-00010101000000-000000000000
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/mattn/go-sqlite3 v1.14.22 // indirect
	golang.org/x/text v0.20.0 // indirect
)
