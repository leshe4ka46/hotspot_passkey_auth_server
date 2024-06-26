package wa

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"hotspot_passkey_auth/consts"
	"hotspot_passkey_auth/db"
	"hotspot_passkey_auth/store"
	"io/ioutil"
)

func AssertionGet(database *db.DB, wba *webauthn.WebAuthn, config *Config) gin.HandlerFunc {
	fn := func(c *gin.Context) {
		var opts = []webauthn.LoginOption{
			webauthn.WithUserVerification(protocol.VerificationPreferred),
		}
		cookie, err := c.Cookie(consts.LoginCookieName)
		if err != nil {
			c.JSON(404, gin.H{"error": "Cookie not found"})
			return
		}
		db_user, err := database.GetUserByCookie(cookie)
		if err != nil {
			c.JSON(404, gin.H{"error": "User not found"})
			return
		}
		var (
			assertion *protocol.CredentialAssertion
			data      *webauthn.SessionData
		)
		if assertion, data, err = wba.BeginDiscoverableLogin(opts...); err != nil {
			c.JSON(404, gin.H{"error": "Not found"})
			return
		}
		fmt.Printf("data: %+v\n", data)
		db_user.Webauthn = JSONString(data)
		database.UpdateUser(db_user)
		c.JSON(200, gin.H{"status": "OK", "data": assertion})
	}
	return gin.HandlerFunc(fn)
}

type MacFromAssertion struct {
	Mac string `json:"mac"`
}

func AssertionPost(database *db.DB, wba *webauthn.WebAuthn, config *Config) gin.HandlerFunc {
	fn := func(c *gin.Context) {
		var (
			credential     *webauthn.Credential
			parsedResponse *protocol.ParsedCredentialAssertionData
			err            error
		)
		postData, err := ioutil.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(404, gin.H{"error": "Body not found"})
			return
		}
		if parsedResponse, err = protocol.ParseCredentialRequestResponseBody(bytes.NewReader(postData)); err != nil {
			c.JSON(404, gin.H{"error": "Error parsing body"})
			return
		}
		cookie, err := c.Cookie(consts.LoginCookieName)
		if err != nil {
			c.JSON(404, gin.H{"error": "Cookie not found"})
			return
		}
		db_user, err := database.GetUserByCookie(cookie)
		if err != nil {
			c.JSON(404, gin.H{"error": "User not found"})
			return
		}
		fmt.Printf("usr: %+v\n", db_user)
		var user store.User
		json.Unmarshal([]byte(db_user.WebauthnUser), &user)
		var webauthnData webauthn.SessionData
		json.Unmarshal([]byte(db_user.Webauthn), &webauthnData)
		var credssignin []webauthn.Credential
		json.Unmarshal([]byte(db_user.CredentialsSignIn), &credssignin)
		if credential, err = wba.ValidateDiscoverableLogin(func(_, userHandle []byte) (_ webauthn.User, err error) {
			fmt.Println("userHandle:", userHandle)
			db_user, err = database.GetUserByUsername(string(userHandle))
			if err != nil {
				fmt.Println("Failed to get user from db:",string(userHandle))
				return &store.User{}, errors.New("user not found")
			}
			asserting_user := &store.User{
				ID:          string(db_user.Username),
				Name:        string(userHandle),
				DisplayName: string(userHandle),
			}
			json.Unmarshal([]byte(db_user.Credentials), &asserting_user.Credentials)
			return asserting_user, nil
		}, webauthnData, parsedResponse); err != nil {
			c.JSON(404, gin.H{"error": "Failed to validate"})
			return
		}
		var macData MacFromAssertion
		json.Unmarshal(postData, &macData)
		credssignin = append(credssignin, *credential)
		db_user.CredentialsSignIn = JSONString(credssignin)
		db_user.Mac = db.AddStr(db_user.Mac, macData.Mac)
		db_user.Cookies = db.AddStr(db_user.Cookies, cookie)
		database.UpdateUser(db_user)
		database.DelByCookie(cookie)
		c.SetCookie(consts.LoginCookieName, db.GetFirst(db_user.Cookies), consts.CookieLifeTime, "/", consts.CookieDomain, false, true)
		database.AddMacRadcheck(macData.Mac)
		c.JSON(200, gin.H{"status": "OK"})
	}
	return gin.HandlerFunc(fn)
}
