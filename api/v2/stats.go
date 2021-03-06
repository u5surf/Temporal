package v2

import (
	"net/http"

	"github.com/RTradeLtd/Temporal/eh"
	"github.com/gin-gonic/gin"
	stats "github.com/semihalev/gin-stats"
)

func (api *API) getStats(c *gin.Context) {
	username, err := GetAuthenticatedUserFromContext(c)
	if err != nil {
		api.LogError(c, err, eh.NoAPITokenError)(http.StatusBadRequest)
		return
	}
	if err := api.validateAdminRequest(username); err != nil {
		FailNotAuthorized(c, eh.UnAuthorizedAdminAccess)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"version":  api.version,
		"response": stats.Report(),
	})
}
