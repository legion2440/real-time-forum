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
	facebookName     = "facebook"
	facebookLabel    = "Facebook"
	facebookAuthURL  = "https://www.facebook.com/dialog/oauth"
	facebookTokenURL = "https://graph.facebook.com/oauth/access_token"
	facebookUserURL  = "https://graph.facebook.com/me"
)

type FacebookProvider struct {
	config ProviderConfig
}

func NewFacebookProvider(config ProviderConfig) (*FacebookProvider, error) {
	if !config.Valid() {
		return nil, ErrProviderDisabled
	}
	return &FacebookProvider{config: config}, nil
}

func (p *FacebookProvider) Name() string  { return facebookName }
func (p *FacebookProvider) Label() string { return facebookLabel }

func (p *FacebookProvider) AuthCodeURL(state string) string {
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(p.config.ClientID))
	values.Set("redirect_uri", strings.TrimSpace(p.config.RedirectURL))
	values.Set("scope", "email,public_profile")
	values.Set("response_type", "code")
	values.Set("state", state)
	return authorizeURL(facebookAuthURL, values)
}

func (p *FacebookProvider) ExchangeCode(ctx context.Context, code string) (*Token, error) {
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(p.config.ClientID))
	values.Set("client_secret", strings.TrimSpace(p.config.ClientSecret))
	values.Set("redirect_uri", strings.TrimSpace(p.config.RedirectURL))
	values.Set("code", strings.TrimSpace(code))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, facebookTokenURL+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}

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

func (p *FacebookProvider) FetchIdentity(ctx context.Context, token *Token) (*Identity, error) {
	values := url.Values{}
	values.Set("fields", "id,name,email,picture.type(large)")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, facebookUserURL+"?"+values.Encode(), nil)
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
		ID      string `json:"id"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		Picture struct {
			Data struct {
				URL string `json:"url"`
			} `json:"data"`
		} `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIdentityFetch, err)
	}

	return &Identity{
		Provider:       facebookName,
		ProviderUserID: strings.TrimSpace(payload.ID),
		Email:          strings.TrimSpace(payload.Email),
		DisplayName:    strings.TrimSpace(payload.Name),
		Username:       strings.TrimSpace(payload.Email),
		AvatarURL:      strings.TrimSpace(payload.Picture.Data.URL),
	}, nil
}

