package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Config llega como JSON en APP_CONFIG (secreto app-config). Mientras el
// secreto tenga el placeholder, la app arranca pero no deja entrar a nadie.
type Config struct {
	GoogleClientID     string   `json:"google_client_id"`
	GoogleClientSecret string   `json:"google_client_secret"`
	SessionKey         string   `json:"session_key"` // >= 32 bytes aleatorios
	BaseURL            string   `json:"base_url"`    // https://app.fixcomap.com
	Admins             []string `json:"admins"`      // pueden crear equipos y ven todos
}

func loadConfig() (Config, error) {
	var c Config
	raw := os.Getenv("APP_CONFIG")
	if raw == "" {
		return c, errors.New("APP_CONFIG vacío")
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return c, fmt.Errorf("APP_CONFIG no es JSON válido: %w", err)
	}
	if c.GoogleClientID == "" || c.GoogleClientSecret == "" || c.BaseURL == "" || len(c.SessionKey) < 32 {
		return c, errors.New("APP_CONFIG incompleto: google_client_id, google_client_secret, base_url y session_key (>= 32)")
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	for i, a := range c.Admins {
		c.Admins[i] = normEmail(a)
	}
	return c, nil
}

func normEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func (c Config) isAdmin(email string) bool { return slices.Contains(c.Admins, email) }

const (
	sessionCookie = "session"
	stateCookie   = "oauth_state"
	sessionTTL    = 30 * 24 * time.Hour // en el móvil, en la banda, no se quiere relogin cada semana
)

// La sesión es una cookie firmada (email|caducidad|hmac): sin tabla de
// sesiones ni dependencias. Revocar a alguien es quitarle del equipo, que se
// comprueba en cada petición contra la BD.
func signSession(key []byte, email string, exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(email)) + "." + strconv.FormatInt(exp.Unix(), 10)
	return payload + "." + mac(key, payload)
}

func verifySession(key []byte, v string, now time.Time) (string, bool) {
	i := strings.LastIndexByte(v, '.')
	if i < 0 {
		return "", false
	}
	payload, sig := v[:i], v[i+1:]
	if !hmac.Equal([]byte(sig), []byte(mac(key, payload))) {
		return "", false
	}
	e, expStr, ok := strings.Cut(payload, ".")
	if !ok {
		return "", false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || now.Unix() > exp {
		return "", false
	}
	email, err := base64.RawURLEncoding.DecodeString(e)
	if err != nil {
		return "", false
	}
	return string(email), true
}

func mac(key []byte, s string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b) // crypto/rand no falla en plataformas soportadas
	return base64.RawURLEncoding.EncodeToString(b)
}

type ctxKey struct{}

type user struct {
	Email string
	Admin bool
}

func currentUser(ctx context.Context) user {
	u, _ := ctx.Value(ctxKey{}).(user)
	return u
}

type auth struct {
	cfg        Config
	httpClient *http.Client
	tokenURL   string // variable para tests
	// devUser: solo local (sin K_SERVICE). Permite probar la app sin OAuth.
	devUser string
}

func (a *auth) secure() bool { return strings.HasPrefix(a.cfg.BaseURL, "https://") }

func (a *auth) setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, Secure: a.secure(), SameSite: http.SameSiteLaxMode,
	})
}

// require deja pasar solo con sesión válida; si no, a /login.
func (a *auth) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email := a.devUser
		if email == "" {
			c, err := r.Cookie(sessionCookie)
			if err != nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			var ok bool
			email, ok = verifySession([]byte(a.cfg.SessionKey), c.Value, time.Now())
			if !ok {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, user{Email: email, Admin: a.cfg.isAdmin(email) || a.devUser != ""})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *auth) start(w http.ResponseWriter, r *http.Request) {
	state := randomToken()
	a.setCookie(w, stateCookie, state, 10*time.Minute)
	q := url.Values{
		"client_id":     {a.cfg.GoogleClientID},
		"redirect_uri":  {a.cfg.BaseURL + "/auth/callback"},
		"response_type": {"code"},
		"scope":         {"openid email"},
		"state":         {state},
		"prompt":        {"select_account"},
	}
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+q.Encode(), http.StatusSeeOther)
}

type idClaims struct {
	Iss           string `json:"iss"`
	Aud           string `json:"aud"`
	Exp           int64  `json:"exp"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// claimsFromIDToken no verifica la firma: el token llega directamente del
// endpoint de Google por TLS en el intercambio del code con el client secret,
// que OIDC Core 3.1.3.7 admite como validación. Sí se validan iss, aud y exp.
func claimsFromIDToken(tok, clientID string, now time.Time) (idClaims, error) {
	var c idClaims
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return c, errors.New("id_token mal formado")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return c, err
	}
	switch {
	case c.Iss != "https://accounts.google.com" && c.Iss != "accounts.google.com":
		return c, errors.New("iss inesperado")
	case c.Aud != clientID:
		return c, errors.New("aud inesperado")
	case now.Unix() > c.Exp:
		return c, errors.New("id_token caducado")
	case !c.EmailVerified || c.Email == "":
		return c, errors.New("email no verificado")
	}
	return c, nil
}

func (a *auth) callback(w http.ResponseWriter, r *http.Request) {
	st, err := r.Cookie(stateCookie)
	if err != nil || r.FormValue("state") == "" || !hmac.Equal([]byte(st.Value), []byte(r.FormValue("state"))) {
		http.Error(w, "estado de login inválido, vuelve a intentarlo", http.StatusBadRequest)
		return
	}
	a.setCookie(w, stateCookie, "", -1)

	resp, err := a.httpClient.PostForm(a.tokenURL, url.Values{
		"code":          {r.FormValue("code")},
		"client_id":     {a.cfg.GoogleClientID},
		"client_secret": {a.cfg.GoogleClientSecret},
		"redirect_uri":  {a.cfg.BaseURL + "/auth/callback"},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		logger(r.Context()).Error("oauth token", "err", err)
		http.Error(w, "no se pudo completar el login", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&tok) != nil {
		logger(r.Context()).Error("oauth token", "status", resp.StatusCode)
		http.Error(w, "no se pudo completar el login", http.StatusBadGateway)
		return
	}
	claims, err := claimsFromIDToken(tok.IDToken, a.cfg.GoogleClientID, time.Now())
	if err != nil {
		logger(r.Context()).Warn("oauth id_token", "err", err)
		http.Error(w, "login rechazado", http.StatusForbidden)
		return
	}
	email := normEmail(claims.Email)
	a.setCookie(w, sessionCookie, signSession([]byte(a.cfg.SessionKey), email, time.Now().Add(sessionTTL)), sessionTTL)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *auth) logout(w http.ResponseWriter, r *http.Request) {
	a.setCookie(w, sessionCookie, "", -1)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
