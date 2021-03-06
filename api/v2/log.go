package v2

import (
	"github.com/gin-gonic/gin"
)

// LogError is a wrapper used by the API to handle logging of errors. Returns a
// callback to also fail a gin context with an optional status code, which
// defaults to http.StatusInternalServerError. Fields is an optional set of
// params provided in pairs, where the first of a pair is the key, and the second
// is the value
//
// Passing in the initial gin.Context to LogError is used to extract the X-Request-ID associated
// with the given request to make it easier to debug user-submitted erros.
func (api *API) LogError(c *gin.Context, err error, message string, fields ...interface{}) func(code ...int) {
	// create base entry with the associated request id
	// for some reason this is being set as `X-Request-Id` despite initially
	// setting the header to `X-Request-ID`
	var logger = api.l.With("request-id", c.GetHeader("X-Request-Id"))

	// write log
	if fields != nil && len(fields)%2 == 0 {
		logger.Errorw(message, append(fields, "error", err.Error())...)
	} else {
		logger.Errorw(message, "error", err.Error())
	}

	// return utility callback
	if message == "" && err != nil {
		return func(code ...int) { Fail(c, err, code...) }
	}
	return func(code ...int) { FailWithMessage(c, message, code...) }
}
