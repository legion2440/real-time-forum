package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

var (
	ErrProviderNotFound  = errors.New("oauth provider not found")
	ErrProviderDisabled  = errors.New("oauth provider disabled")
	ErrProviderError     = errors.New("oauth provider error")
	ErrMissingCode       = errors.New("oauth code missing")
	ErrInvalidState      = errors.New("oauth state invalid")
	ErrTokenExchange     = errors.New("oauth token exchange failed")
	ErrIdentityFetch     = errors.New("oauth identity fetch failed")
	ErrEmailUnavailable  = errors.New("oauth email unavailable")
)

type Provider interface {
	Name() string
	Label() string
	AuthCodeURL(state string) string
	ExchangeCode(ctx context.Context, code string) (*Token, error)
	FetchIdentity(ctx context.Context, token *Token) (*Identity, error)
}

type Token struct {
	AccessToken string
	TokenType   string
}

type Identity struct {
	Provider              string
	ProviderUserID        string
	Email                 string
	EmailVerified         bool
	DisplayName           string
	Username              string
	AvatarURL             string
}

type ProviderInfo struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
}

type Registry struct {
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) *Registry {
	items := make(map[string]Provider, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		name := strings.TrimSpace(strings.ToLower(provider.Name()))
		if name == "" {
			continue
		}
		items[name] = provider
	}
	return &Registry{providers: items}
}

func (r *Registry) Get(name string) (Provider, error) {
	if r == nil {
		return nil, ErrProviderDisabled
	}
	provider, ok := r.providers[strings.TrimSpace(strings.ToLower(name))]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return provider, nil
}

func (r *Registry) Infos() []ProviderInfo {
	infos := make([]ProviderInfo, 0, len(r.providers))
	for _, provider := range r.providers {
		infos = append(infos, ProviderInfo{
			Name:    provider.Name(),
			Label:   provider.Label(),
			Enabled: true,
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos
}

type ProviderConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTPClient   *http.Client
}

func (c ProviderConfig) Valid() bool {
	return strings.TrimSpace(c.ClientID) != "" &&
		strings.TrimSpace(c.ClientSecret) != "" &&
		strings.TrimSpace(c.RedirectURL) != ""
}

func (c ProviderConfig) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func authorizeURL(baseURL string, values url.Values) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	u.RawQuery = values.Encode()
	return u.String()
}

