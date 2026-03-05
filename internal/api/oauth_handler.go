package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/config"
	"github.com/superset-studio/majordomo-gateway/internal/models"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

var errEmailAlreadyExists = errors.New("an account with this email already exists")

// OAuthHandler provides OAuth login endpoints for GitHub and Google.
type OAuthHandler struct {
	users       storage.UserStorage
	jwt         *auth.JWTService
	oauth       config.OAuthConfig
	baseURL     string // gateway's own base URL for constructing callback URLs
	frontendURL string // frontend URL to redirect back to after auth
}

// NewOAuthHandler creates a new OAuth handler.
func NewOAuthHandler(
	users storage.UserStorage,
	jwtSvc *auth.JWTService,
	oauthCfg config.OAuthConfig,
	baseURL string,
) *OAuthHandler {
	frontendURL := oauthCfg.FrontendURL
	if frontendURL == "" {
		frontendURL = baseURL
	}
	return &OAuthHandler{
		users:       users,
		jwt:         jwtSvc,
		oauth:       oauthCfg,
		baseURL:     baseURL,
		frontendURL: frontendURL,
	}
}

// --- GitHub OAuth ---

// GitHubLogin handles GET /api/v1/admin/auth/github
func (h *OAuthHandler) GitHubLogin(w http.ResponseWriter, r *http.Request) {
	state := generateState()
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	params := url.Values{
		"client_id":    {h.oauth.GitHub.ClientID},
		"redirect_uri": {h.baseURL + "/api/v1/admin/auth/github/callback"},
		"state":        {state},
		"scope":        {"user:email"},
	}
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+params.Encode(), http.StatusFound)
}

// GitHubCallback handles GET /api/v1/admin/auth/github/callback
func (h *OAuthHandler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	if err := verifyOAuthState(r); err != nil {
		slog.Warn("OAuth state verification failed", "error", err)
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code parameter", http.StatusBadRequest)
		return
	}

	// Exchange code for access token
	tokenResp, err := exchangeGitHubCode(code, h.oauth.GitHub.ClientID, h.oauth.GitHub.ClientSecret, h.baseURL+"/api/v1/admin/auth/github/callback")
	if err != nil {
		slog.Error("failed to exchange GitHub code", "error", err)
		http.Error(w, "failed to authenticate with GitHub", http.StatusInternalServerError)
		return
	}

	// Fetch user info from GitHub
	ghUser, err := fetchGitHubUser(tokenResp.AccessToken)
	if err != nil {
		slog.Error("failed to fetch GitHub user", "error", err)
		http.Error(w, "failed to fetch GitHub user info", http.StatusInternalServerError)
		return
	}

	// If GitHub doesn't return an email in the user profile, fetch from emails API
	email := ghUser.Email
	if email == "" {
		email, _ = fetchGitHubPrimaryEmail(tokenResp.AccessToken)
	}

	providerID := fmt.Sprintf("%d", ghUser.ID)
	user, err := h.findOrCreateOAuthUser(r, "github", providerID, ghUser.Login, email)
	if err != nil {
		if errors.Is(err, errEmailAlreadyExists) {
			h.redirectWithError(w, r, "An account with this email already exists. Try signing in with a different method.")
			return
		}
		slog.Error("failed to find/create OAuth user", "error", err)
		http.Error(w, "failed to create user account", http.StatusInternalServerError)
		return
	}

	h.redirectWithToken(w, r, user)
}

// --- Google OAuth ---

// GoogleLogin handles GET /api/v1/admin/auth/google
func (h *OAuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	state := generateState()
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	params := url.Values{
		"client_id":     {h.oauth.Google.ClientID},
		"redirect_uri":  {h.baseURL + "/api/v1/admin/auth/google/callback"},
		"state":         {state},
		"scope":         {"openid email profile"},
		"response_type": {"code"},
	}
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+params.Encode(), http.StatusFound)
}

// GoogleCallback handles GET /api/v1/admin/auth/google/callback
func (h *OAuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	if err := verifyOAuthState(r); err != nil {
		slog.Warn("OAuth state verification failed", "error", err)
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code parameter", http.StatusBadRequest)
		return
	}

	// Exchange code for access token
	tokenResp, err := exchangeGoogleCode(code, h.oauth.Google.ClientID, h.oauth.Google.ClientSecret, h.baseURL+"/api/v1/admin/auth/google/callback")
	if err != nil {
		slog.Error("failed to exchange Google code", "error", err)
		http.Error(w, "failed to authenticate with Google", http.StatusInternalServerError)
		return
	}

	// Fetch user info from Google
	gUser, err := fetchGoogleUser(tokenResp.AccessToken)
	if err != nil {
		slog.Error("failed to fetch Google user", "error", err)
		http.Error(w, "failed to fetch Google user info", http.StatusInternalServerError)
		return
	}

	// Derive username from email
	username := gUser.Email
	if idx := strings.Index(username, "@"); idx > 0 {
		username = username[:idx]
	}

	user, err := h.findOrCreateOAuthUser(r, "google", gUser.ID, username, gUser.Email)
	if err != nil {
		if errors.Is(err, errEmailAlreadyExists) {
			h.redirectWithError(w, r, "An account with this email already exists. Try signing in with a different method.")
			return
		}
		slog.Error("failed to find/create OAuth user", "error", err)
		http.Error(w, "failed to create user account", http.StatusInternalServerError)
		return
	}

	h.redirectWithToken(w, r, user)
}

// --- Shared helpers ---

func (h *OAuthHandler) findOrCreateOAuthUser(r *http.Request, provider, providerID, username, email string) (*models.User, error) {
	ctx := r.Context()

	// Look up existing user by provider
	user, err := h.users.GetUserByAuthProvider(ctx, provider, providerID)
	if err != nil {
		return nil, fmt.Errorf("get user by auth provider: %w", err)
	}
	if user != nil {
		return user, nil
	}

	// Create new OAuth user — try with the provider username first
	var emailPtr *string
	if email != "" {
		emailPtr = &email
	}

	input := &models.CreateOAuthUserInput{
		Username:       username,
		Email:          emailPtr,
		AuthProvider:   provider,
		AuthProviderID: providerID,
	}

	user, err = h.users.CreateOAuthUser(ctx, input)
	if err != nil {
		if isEmailConflict(err) {
			return nil, errEmailAlreadyExists
		}
		// Username conflict — append random suffix and retry
		suffix, _ := randomHex(4)
		input.Username = username + "-" + suffix
		user, err = h.users.CreateOAuthUser(ctx, input)
		if err != nil {
			if isEmailConflict(err) {
				return nil, errEmailAlreadyExists
			}
			return nil, fmt.Errorf("create oauth user: %w", err)
		}
	}

	return user, nil
}

func isEmailConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "idx_users_email")
}

func (h *OAuthHandler) redirectWithError(w http.ResponseWriter, r *http.Request, msg string) {
	redirectURL := h.frontendURL + "/login?error=" + url.QueryEscape(msg)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (h *OAuthHandler) redirectWithToken(w http.ResponseWriter, r *http.Request, user *models.User) {
	token, err := h.jwt.GenerateToken(user.ID, user.Username)
	if err != nil {
		slog.Error("failed to generate JWT for OAuth user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Clear the state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	redirectURL := h.frontendURL + "/login?token=" + url.QueryEscape(token)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func generateState() string {
	s, _ := randomHex(16)
	return s
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func verifyOAuthState(r *http.Request) error {
	cookie, err := r.Cookie("oauth_state")
	if err != nil {
		return fmt.Errorf("missing state cookie: %w", err)
	}
	state := r.URL.Query().Get("state")
	if state == "" || state != cookie.Value {
		return fmt.Errorf("state mismatch")
	}
	return nil
}

// --- GitHub API types and helpers ---

type githubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type githubUser struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func exchangeGitHubCode(code, clientID, clientSecret, redirectURI string) (*githubTokenResponse, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}

	req, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github token exchange: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read github token response: %w", err)
	}

	var tokenResp githubTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse github token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("github returned empty access token")
	}

	return &tokenResp, nil
}

func fetchGitHubUser(accessToken string) (*githubUser, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch github user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github user API returned status %d", resp.StatusCode)
	}

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("parse github user: %w", err)
	}

	return &user, nil
}

func fetchGitHubPrimaryEmail(accessToken string) (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch github emails: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github emails API returned status %d", resp.StatusCode)
	}

	var emails []githubEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", fmt.Errorf("parse github emails: %w", err)
	}

	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}

	return "", nil
}

// --- Google API types and helpers ---

type googleTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	IDToken     string `json:"id_token"`
}

type googleUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func exchangeGoogleCode(code, clientID, clientSecret, redirectURI string) (*googleTokenResponse, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	req, err := http.NewRequest("POST", "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google token exchange: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read google token response: %w", err)
	}

	var tokenResp googleTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse google token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("google returned empty access token")
	}

	return &tokenResp, nil
}

func fetchGoogleUser(accessToken string) (*googleUser, error) {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch google user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google userinfo API returned status %d", resp.StatusCode)
	}

	var user googleUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("parse google user: %w", err)
	}

	return &user, nil
}
