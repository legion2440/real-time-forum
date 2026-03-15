package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	googleName     = "google"
	googleLabel    = "Google"
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
	googleUserURL  = "https://openidconnect.googleapis.com/v1/userinfo"
)

type GoogleProvider struct {
	config ProviderConfig
}

func NewGoogleProvider(config ProviderConfig) (*GoogleProvider, error) {
	if !config.Valid() {
		return nil, ErrProviderDisabled
	}
	return &GoogleProvider{config: config}, nil
}

func (p *GoogleProvider) Name() string  { return googleName }
func (p *GoogleProvider) Label() string { return googleLabel }

func (p *GoogleProvider) AuthCodeURL(state string) string {
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(p.config.ClientID))
	values.Set("redirect_uri", strings.TrimSpace(p.config.RedirectURL))
	values.Set("response_type", "code")
	values.Set("scope", "openid email profile")
	values.Set("state", state)
	values.Set("access_type", "online")
	values.Set("prompt", "select_account")
	return authorizeURL(googleAuthURL, values)
}

func (p *GoogleProvider) ExchangeCode(ctx context.Context, code string) (*Token, error) {
	form := url.Values{}
	form.Set("code", strings.TrimSpace(code))
	form.Set("client_id", strings.TrimSpace(p.config.ClientID))
	form.Set("client_secret", strings.TrimSpace(p.config.ClientSecret))
	form.Set("redirect_uri", strings.TrimSpace(p.config.RedirectURL))
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.config.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenExchange, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%w: %s", ErrTokenExchange, strings.TrimSpace(string(body)))
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenExchange, err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return nil, ErrTokenExchange
	}

	return &Token{
		AccessToken: strings.TrimSpace(payload.AccessToken),
		TokenType:   strings.TrimSpace(payload.TokenType),
	}, nil
}

func (p *GoogleProvider) FetchIdentity(ctx context.Context, token *Token) (*Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token.AccessToken))

	resp, err := p.config.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIdentityFetch, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%w: %s", ErrIdentityFetch, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIdentityFetch, err)
	}

	return &Identity{
		Provider:       googleName,
		ProviderUserID: strings.TrimSpace(payload.Sub),
		Email:          strings.TrimSpace(payload.Email),
		EmailVerified:  payload.EmailVerified,
		DisplayName:    strings.TrimSpace(payload.Name),
		Username:       strings.TrimSpace(payload.Email),
		AvatarURL:      strings.TrimSpace(payload.Picture),
	}, nil
}

