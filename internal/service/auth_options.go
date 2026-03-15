package service

import (
	"forum/internal/oauth"
	"forum/internal/repo"
)

type AuthOption func(*AuthService)

type OAuthDependencies struct {
	Providers  *oauth.Registry
	Identities repo.AuthIdentityRepo
	Flows      repo.AuthFlowRepo
	Accounts   repo.AccountRepo
}

type oauthDependencies struct {
	providers  *oauth.Registry
	identities repo.AuthIdentityRepo
	flows      repo.AuthFlowRepo
	accounts   repo.AccountRepo
}

func WithOAuth(deps OAuthDependencies) AuthOption {
	return func(service *AuthService) {
		if service == nil {
			return
		}
		if deps.Providers == nil || deps.Identities == nil || deps.Flows == nil || deps.Accounts == nil {
			return
		}
		service.oauth = &oauthDependencies{
			providers:  deps.Providers,
			identities: deps.Identities,
			flows:      deps.Flows,
			accounts:   deps.Accounts,
		}
	}
}
