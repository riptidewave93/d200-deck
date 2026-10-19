package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Scopes are what deckhost asks the user to allow.
var Scopes = []string{"rpc", "rpc.voice.read", "rpc.voice.write"}

// TokenURL is Discord's OAuth2 token endpoint.
var TokenURL = "https://discord.com/api/oauth2/token"

// RedirectURI must be one of the app's redirects in the developer portal.
const RedirectURI = "http://localhost"

// AuthorizeTimeout is how long the user has to answer Discord's prompt.
var AuthorizeTimeout = 2 * time.Minute

// App is a Discord application the user made for deckhost.
type App struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// Token is an OAuth2 token.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// Authorize shows Discord's prompt to allow app and returns the code to
// exchange for a token.
func (d *Conn) Authorize(app App) (string, error) {
	data, err := d.do("AUTHORIZE", map[string]any{"client_id": app.ClientID, "scopes": Scopes}, "", AuthorizeTimeout)
	if err != nil {
		return "", err
	}
	var r struct{ Code string }
	if err := json.Unmarshal(data, &r); err != nil || r.Code == "" {
		return "", fmt.Errorf("discord: AUTHORIZE gave no code")
	}
	return r.Code, nil
}

// Exchange trades an Authorize code for a token.
func (app App) Exchange(code string) (Token, error) {
	return app.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {RedirectURI}})
}

// Refresh renews a token.
func (app App) Refresh(t Token) (Token, error) {
	return app.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {t.RefreshToken}})
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func (app App) token(form url.Values) (Token, error) {
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	resp, err := httpClient.Post(TokenURL, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return Token{}, fmt.Errorf("discord token: %s: %s", resp.Status, b)
	}
	var r struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(b, &r); err != nil || r.AccessToken == "" {
		return Token{}, fmt.Errorf("discord token: bad reply")
	}
	return Token{r.AccessToken, r.RefreshToken, time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)}, nil
}

// Authenticate logs in with an access token.
func (d *Conn) Authenticate(t Token) error {
	_, err := d.Do("AUTHENTICATE", map[string]string{"access_token": t.AccessToken}, "")
	return err
}

// VoiceSettings are the parts of the user's voice settings deckhost uses.
type VoiceSettings struct {
	Mute bool `json:"mute"`
	Deaf bool `json:"deaf"`
	Mode struct {
		Type string `json:"type"` // VOICE_ACTIVITY or PUSH_TO_TALK
	} `json:"mode"`
	Input struct {
		Volume float64 `json:"volume"` // 0-100
	} `json:"input"`
	Output struct {
		Volume float64 `json:"volume"` // 0-200
	} `json:"output"`
	NoiseSuppression     bool `json:"noise_suppression"`
	EchoCancellation     bool `json:"echo_cancellation"`
	AutomaticGainControl bool `json:"automatic_gain_control"`
}

// VoiceSettings returns the user's voice settings.
func (d *Conn) VoiceSettings() (VoiceSettings, error) {
	return d.voice("GET_VOICE_SETTINGS", nil)
}

// SetVoiceSettings changes the given settings (e.g. {"mute": true}) and
// returns the result.
func (d *Conn) SetVoiceSettings(change map[string]any) (VoiceSettings, error) {
	return d.voice("SET_VOICE_SETTINGS", change)
}

func (d *Conn) voice(cmd string, args any) (VoiceSettings, error) {
	var v VoiceSettings
	data, err := d.Do(cmd, args, "")
	if err == nil {
		err = json.Unmarshal(data, &v)
	}
	return v, err
}
