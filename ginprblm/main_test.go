package ginprblm_test

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	// Test mode silences gin's debug route listing; it is process-wide, so it
	// is set once here rather than in parallel tests.
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}
