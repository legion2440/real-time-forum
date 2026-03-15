package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	githubName      = "github"
	githubLabel     = "GitHub"
	githubAuthURL   = "https://github.com/login/oauth/authorize"
	githubTokenURL  = "https://github.com/login/oauth/access_token"
	githubUserURL   = "https://api.github.com/user"
	githubEmailsURL = "https://api.github.com/user/emails"
)

type GitHubProvider struct {
	config ProviderConfig
}

func NewGitHubProvider(config ProviderConfig) (*GitHubProvider, error) {
	if !config.Valid() {
		return nil, ErrProviderDisabled
	}
	return &GitHubProvider{config: config}, nil
}

func (p *GitHubProvider) Name() string  { return githubName }
func (p *GitHubProvider) Label() string { return githubLabel }

func (p *GitHubProvider) AuthCodeURL(state string) string {
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(p.config.ClientID))
	values.Set("redirect_uri", strings.TrimSpace(p.config.RedirectURL))
	values.Set("scope", "read:user user:email")
	values.Set("state", state)
	return authorizeURL(githubAuthURL, values)
}

func (p *GitHubProvider) ExchangeCode(ctx context.Context, code string) (*Token, error) {
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(p.config.ClientID))
	form.Set("client_secret", strings.TrimSpace(p.config.ClientSecret))
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", strings.TrimSpace(p.config.RedirectURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
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

func (p *GitHubProvider) FetchIdentity(ctx context.Context, token *Token) (*Identity, error) {
	profileReq, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserURL, nil)
	if err != nil {
		return nil, err
	}
	profileReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token.AccessToken))
	profileReq.Header.Set("Accept", "application/vnd.github+json")
	profileReq.Header.Set("User-Agent", "forum-oauth")

	profileResp, err := p.config.client().Do(profileReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIdentityFetch, err)
	}
	defer profileResp.Body.Close()

	if profileResp.StatusCode < http.StatusOK || profileResp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(profileResp.Body, 4096))
		return nil, fmt.Errorf("%w: %s", ErrIdentityFetch, strings.TrimSpace(string(body)))
	}

	var profile struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(profileResp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIdentityFetch, err)
	}

	email := strings.TrimSpace(profile.Email)
	emailVerified := false
	if email == "" {
		email, emailVerified, err = p.fetchVerifiedEmail(ctx, token)
		if err != nil {
			return nil, err
		}
	}

	return &Identity{
		Provider:       githubName,
		ProviderUserID: strconv.FormatInt(profile.ID, 10),
		Email:          email,
		EmailVerified:  emailVerified,
		DisplayName:    strings.TrimSpace(profile.Name),
		Username:       strings.TrimSpace(profile.Login),
		AvatarURL:      strings.TrimSpace(profile.AvatarURL),
	}, nil
}

func (p *GitHubProvider) fetchVerifiedEmail(ctx context.Context, token *Token) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubEmailsURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token.AccessToken))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "forum-oauth")

	resp, err := p.config.client().Do(req)
	if err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrIdentityFetch, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", false, fmt.Errorf("%w: %s", ErrIdentityFetch, strings.TrimSpace(string(body)))
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrIdentityFetch, err)
	}

	for _, item := range emails {
		if item.Primary && item.Verified && strings.TrimSpace(item.Email) != "" {
			return strings.TrimSpace(item.Email), true, nil
		}
	}
	for _, item := range emails {
		if item.Verified && strings.TrimSpace(item.Email) != "" {
			return strings.TrimSpace(item.Email), true, nil
		}
	}
	return "", false, ErrEmailUnavailable
}

