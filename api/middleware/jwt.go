package middleware

import (
	"net/http"
	"time"

	"github.com/RTradeLtd/Temporal/models"
	jwt "github.com/appleboy/gin-jwt"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
)

var realmName = "temporal-realm"

// JwtConfigGenerate is used to generate our JWT configuration
func JwtConfigGenerate(jwtKey string, db *gorm.DB) *jwt.GinJWTMiddleware {

	// will implement metamaks/msg signing with ethereum accounts
	// as the authentication metho
	authMiddleware := &jwt.GinJWTMiddleware{
		Realm:      realmName,
		Key:        []byte(jwtKey),
		Timeout:    time.Hour * 24,
		MaxRefresh: time.Hour * 24,
		Authenticator: func(userId string, password string, c *gin.Context) (string, bool) { // userId = uploader address
			userManager := models.NewUserManager(db)
			validAuth, err := userManager.ComparePlaintextPasswordToHash(userId, password)
			if err != nil {
				return userId, false
			}
			if !validAuth {
				return userId, false
			}
			return userId, true
		},
		Authorizator: func(userId string, c *gin.Context) bool {

			return true
		},
		Unauthorized: func(c *gin.Context, code int, message string) {
			c.JSON(code, gin.H{
				"code":    code,
				"message": message,
			})
		},

		TokenLookup:   "header:Authorization",
		TokenHeadName: "Bearer",
		TimeFunc:      time.Now,
	}

	return authMiddleware
}

func GetAuthenticatedUser(c *gin.Context) {
	claims := jwt.ExtractClaims(c)
	c.JSON(http.StatusFound, gin.H{"claims-id": claims["id"]})
	return
}