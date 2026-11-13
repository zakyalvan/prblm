package ginprblm

import (
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
)

// Scope returns gin middleware that attaches mappers to the request context
// for the routes it guards, usually a route group. They run before the
// handler's own mappers (see [prblm.ScopeMappers]), and only for those
// routes:
//
//	contracts := engine.Group("/v1/contracts", ginprblm.Scope(
//		prblm.Status(ErrLocked, http.StatusLocked),
//	))
//
// Nil mappers, including a nil [prblm.MapperFunc], are ignored, and a scope
// left with no mappers changes nothing.
func Scope(mappers ...prblm.Mapper) gin.HandlerFunc {
	if len(mappers) == 0 {
		return func(c *gin.Context) { c.Next() }
	}

	// The core skips nil mappers; the copy keeps a caller that reuses its
	// slice from changing the scope.
	mappers = slices.Clone(mappers)

	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(prblm.ScopeMappers(c.Request.Context(), mappers...))
		c.Next()
	}
}
