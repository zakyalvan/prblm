// Package plugintest tests the mapper plugins together: the error chains
// gorm produces over pgx, and the registration order the plugins document.
//
// It is its own unpublished module, so that no published plugin has to
// require another plugin or its dependencies for these tests.
package plugintest
