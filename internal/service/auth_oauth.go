package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"forum/internal/domain"
	"forum/internal/oauth"
	"forum/internal/repo"
)

const (
	oauthIntentLogin         = "login"
	oauthIntentLink          = "link"
	authFlowKindOAuthState   = "oauth_state"
	authFlowKindAccountLink  = "account_link"
	authFlowKindAccountMerge = "account_merge"
	oauthStateTTL            = 10 * time.Minute
	authDecisionFlowTTL      = 30 * time.Minute
	maxGeneratedUsernameLen  = 32
	maxUsernameVariants      = 500
)

type LinkedAccountStatus struct {
	Provider    string    `json:"provider"`
	Label       string    `json:"label"`
	Enabled     bool      `json:"enabled"`
	Linked      bool      `json:"linked"`
	Email       string    `json:"email,omitempty"`
	LastLoginAt time.Time `json:"lastLoginAt,omitempty"`
	CanUnlink   bool      `json:"canUnlink"`
}

type MyAccount struct {
	User           *domain.User          `json:"user"`
	HasPassword    bool                  `json:"hasPassword"`
	LinkedAccounts []LinkedAccountStatus `json:"linkedAccounts"`
}

type OAuthCallbackResult struct {
	RedirectPath string          `json:"redirectPath"`
	Session      *domain.Session `json:"-"`
}

type FlowUserSummary struct {
	ID             int64                 `json:"id"`
	Email          string                `json:"email"`
	Username       string                `json:"username"`
	DisplayName    string                `json:"displayName"`
	CreatedAt      time.Time             `json:"createdAt"`
	HasPassword    bool                  `json:"hasPassword"`
	LinkedAccounts []LinkedAccountStatus `json:"linkedAccounts"`
}

type ExternalIdentityPreview struct {
	Provider      string `json:"provider"`
	ProviderLabel string `json:"providerLabel"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	DisplayName   string `json:"displayName"`
	Username      string `json:"username"`
	AvatarURL     string `json:"avatarUrl"`
}

type AccountLinkView struct {
	Token                         string                  `json:"token"`
	ExistingAccount               FlowUserSummary         `json:"existingAccount"`
	ExternalIdentity              ExternalIdentityPreview `json:"externalIdentity"`
	CurrentSessionMatchesExisting bool                    `json:"currentSessionMatchesExisting"`
	NextPath                      string                  `json:"nextPath,omitempty"`
}

type AccountMergeView struct {
	Token              string          `json:"token"`
	CanonicalUser      FlowUserSummary `json:"canonicalUser"`
	SourceUser         FlowUserSummary `json:"sourceUser"`
	DefaultDisplayName string          `json:"defaultDisplayName"`
	FinalEmail         string          `json:"finalEmail"`
	FinalUsername      string          `json:"finalUsername"`
	LocalLoginUserID   int64           `json:"localLoginUserId,omitempty"`
	Reason             string          `json:"reason"`
	Provider           string          `json:"provider,omitempty"`
}

type AuthFlowView struct {
	Kind  string            `json:"kind"`
	Link  *AccountLinkView  `json:"link,omitempty"`
	Merge *AccountMergeView `json:"merge,omitempty"`
}

type oauthStateFlowPayload struct {
	Provider string `json:"provider"`
	Intent   string `json:"intent"`
	NextPath string `json:"nextPath,omitempty"`
}

type accountLinkFlowPayload struct {
	Provider       string `json:"provider"`
	ProviderUserID string `json:"providerUserId"`
	Email          string `json:"email"`
	EmailVerified  bool   `json:"emailVerified"`
	DisplayName    string `json:"displayName"`
	Username       string `json:"username"`
	AvatarURL      string `json:"avatarUrl"`
	MatchedUserID  int64  `json:"matchedUserId"`
	NextPath       string `json:"nextPath,omitempty"`
}

type accountMergeFlowPayload struct {
	CanonicalUserID    int64  `json:"canonicalUserId"`
	SourceUserID       int64  `json:"sourceUserId"`
	DefaultDisplayName string `json:"defaultDisplayName"`
	Reason             string `json:"reason"`
	Provider           string `json:"provider,omitempty"`
}

func (s *AuthService) OAuthProviders() []oauth.ProviderInfo {
	if s.oauth == nil || s.oauth.providers == nil {
		return []oauth.ProviderInfo{}
	}
	return s.oauth.providers.Infos()
}

func (s *AuthService) GetMyAccount(ctx context.Context, userID int64) (*MyAccount, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	linkedAccounts, err := s.listLinkedAccounts(ctx, user)
	if err != nil {
		return nil, err
	}

	return &MyAccount{
		User:           user,
		HasPassword:    userHasPassword(user),
		LinkedAccounts: linkedAccounts,
	}, nil
}

func (s *AuthService) StartOAuth(ctx context.Context, provider, intent, nextPath string, currentUserID int64) (string, error) {
	providerImpl, err := s.oauthProvider(provider)
	if err != nil {
		return "", err
	}

	intent = strings.ToLower(strings.TrimSpace(intent))
	switch intent {
	case "", oauthIntentLogin:
		intent = oauthIntentLogin
	case oauthIntentLink:
		if currentUserID <= 0 {
			return "", ErrUnauthorized
		}
	default:
		return "", ErrInvalidInput
	}

	token, err := s.idGen.New()
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(oauthStateFlowPayload{
		Provider: providerImpl.Name(),
		Intent:   intent,
		NextPath: normalizeNextPath(nextPath),
	})
	if err != nil {
		return "", err
	}

	now := s.clock.Now()
	if err := s.oauth.flows.Create(ctx, &domain.AuthFlow{
		Token:     token,
		Kind:      authFlowKindOAuthState,
		UserID:    currentUserID,
		Payload:   string(payload),
		CreatedAt: now,
		ExpiresAt: now.Add(oauthStateTTL),
	}); err != nil {
		return "", err
	}

	return providerImpl.AuthCodeURL(token), nil
}

func (s *AuthService) HandleOAuthCallback(ctx context.Context, providerName, stateToken, code, providerErr string) (*OAuthCallbackResult, error) {
	if strings.TrimSpace(providerErr) != "" {
		return nil, ErrOAuthProviderReturnedError
	}
	if strings.TrimSpace(code) == "" {
		return nil, ErrOAuthCodeMissing
	}
	if strings.TrimSpace(stateToken) == "" {
		return nil, ErrOAuthStateInvalid
	}

	providerImpl, err := s.oauthProvider(providerName)
	if err != nil {
		return nil, err
	}

	flow, err := s.oauth.flows.TakeByToken(ctx, strings.TrimSpace(stateToken))
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrOAuthStateInvalid
		}
		return nil, err
	}

	var payload oauthStateFlowPayload
	if err := s.decodeFlowPayload(flow, authFlowKindOAuthState, &payload); err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Provider), providerImpl.Name()) {
		return nil, ErrOAuthStateInvalid
	}

	token, err := providerImpl.ExchangeCode(ctx, strings.TrimSpace(code))
	if err != nil {
		return nil, mapOAuthProviderError(err)
	}

	identity, err := providerImpl.FetchIdentity(ctx, token)
	if err != nil {
		return nil, mapOAuthProviderError(err)
	}

	normalizedIdentity, err := normalizeExternalIdentity(providerImpl.Name(), identity)
	if err != nil {
		return nil, err
	}

	existingIdentity, err := s.oauth.identities.GetByProviderUserID(ctx, normalizedIdentity.Provider, normalizedIdentity.ProviderUserID)
	if err == nil {
		return s.handleExistingOAuthIdentity(ctx, flow.UserID, payload, normalizedIdentity, existingIdentity)
	}
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}

	return s.handleFreshOAuthIdentity(ctx, flow.UserID, payload, normalizedIdentity)
}

func (s *AuthService) GetAuthFlowView(ctx context.Context, token string, currentUserID int64) (*AuthFlowView, error) {
	flow, err := s.getValidAuthFlow(ctx, token)
	if err != nil {
		return nil, err
	}

	switch flow.Kind {
	case authFlowKindAccountLink:
		var payload accountLinkFlowPayload
		if err := decodeJSONPayload(flow.Payload, &payload); err != nil {
			return nil, ErrInvalidInput
		}

		matchedUser, err := s.GetUserByID(ctx, payload.MatchedUserID)
		if err != nil {
			return nil, err
		}

		matchedAccounts, err := s.listLinkedAccounts(ctx, matchedUser)
		if err != nil {
			return nil, err
		}

		return &AuthFlowView{
			Kind: flow.Kind,
			Link: &AccountLinkView{
				Token: token,
				ExistingAccount: FlowUserSummary{
					ID:             matchedUser.ID,
					Email:          matchedUser.Email,
					Username:       matchedUser.Username,
					DisplayName:    userPublicDisplayName(matchedUser),
					CreatedAt:      matchedUser.CreatedAt,
					HasPassword:    userHasPassword(matchedUser),
					LinkedAccounts: linkedOnly(matchedAccounts),
				},
				ExternalIdentity: ExternalIdentityPreview{
					Provider:      payload.Provider,
					ProviderLabel: s.providerLabel(payload.Provider),
					Email:         payload.Email,
					EmailVerified: payload.EmailVerified,
					DisplayName:   strings.TrimSpace(payload.DisplayName),
					Username:      strings.TrimSpace(payload.Username),
					AvatarURL:     strings.TrimSpace(payload.AvatarURL),
				},
				CurrentSessionMatchesExisting: currentUserID > 0 && currentUserID == matchedUser.ID,
				NextPath:                      normalizeNextPath(payload.NextPath),
			},
		}, nil
	case authFlowKindAccountMerge:
		var payload accountMergeFlowPayload
		if err := decodeJSONPayload(flow.Payload, &payload); err != nil {
			return nil, ErrInvalidInput
		}
		if currentUserID <= 0 || (currentUserID != payload.CanonicalUserID && currentUserID != payload.SourceUserID) {
			return nil, ErrForbidden
		}

		canonicalUser, canonicalStatuses, sourceUser, sourceStatuses, finalEmail, finalUsername, localLoginUserID, err := s.buildMergePreview(ctx, payload.CanonicalUserID, payload.SourceUserID)
		if err != nil {
			return nil, err
		}

		return &AuthFlowView{
			Kind: flow.Kind,
			Merge: &AccountMergeView{
				Token: token,
				CanonicalUser: FlowUserSummary{
					ID:             canonicalUser.ID,
					Email:          canonicalUser.Email,
					Username:       canonicalUser.Username,
					DisplayName:    userPublicDisplayName(canonicalUser),
					CreatedAt:      canonicalUser.CreatedAt,
					HasPassword:    userHasPassword(canonicalUser),
					LinkedAccounts: linkedOnly(canonicalStatuses),
				},
				SourceUser: FlowUserSummary{
					ID:             sourceUser.ID,
					Email:          sourceUser.Email,
					Username:       sourceUser.Username,
					DisplayName:    userPublicDisplayName(sourceUser),
					CreatedAt:      sourceUser.CreatedAt,
					HasPassword:    userHasPassword(sourceUser),
					LinkedAccounts: linkedOnly(sourceStatuses),
				},
				DefaultDisplayName: payload.DefaultDisplayName,
				FinalEmail:         finalEmail,
				FinalUsername:      finalUsername,
				LocalLoginUserID:   localLoginUserID,
				Reason:             payload.Reason,
				Provider:           payload.Provider,
			},
		}, nil
	default:
		return nil, ErrNotFound
	}
}

func (s *AuthService) ConfirmLinkFlowWithLocal(ctx context.Context, token, login, password string) (*OAuthCallbackResult, error) {
	flow, err := s.getValidAuthFlow(ctx, token)
	if err != nil {
		return nil, err
	}
	if flow.Kind != authFlowKindAccountLink {
		return nil, ErrInvalidInput
	}

	var payload accountLinkFlowPayload
	if err := decodeJSONPayload(flow.Payload, &payload); err != nil {
		return nil, ErrInvalidInput
	}

	user, err := s.authenticateLocalUser(ctx, login, password)
	if err != nil {
		return nil, err
	}
	if user.ID != payload.MatchedUserID {
		return nil, ErrUnauthorized
	}

	if err := s.linkPendingIdentity(ctx, user.ID, payload); err != nil {
		return nil, err
	}
	if err := s.oauth.flows.DeleteByToken(ctx, token); err != nil {
		return nil, err
	}

	session, err := s.createSessionForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &OAuthCallbackResult{
		Session:      session,
		RedirectPath: s.resolveLinkCompletionPath(ctx, user.ID, payload.NextPath, payload.Provider),
	}, nil
}

func (s *AuthService) CompleteLinkFlow(ctx context.Context, token string, currentUserID int64) (*OAuthCallbackResult, error) {
	if currentUserID <= 0 {
		return nil, ErrUnauthorized
	}

	flow, err := s.getValidAuthFlow(ctx, token)
	if err != nil {
		return nil, err
	}
	if flow.Kind != authFlowKindAccountLink {
		return nil, ErrInvalidInput
	}

	var payload accountLinkFlowPayload
	if err := decodeJSONPayload(flow.Payload, &payload); err != nil {
		return nil, ErrInvalidInput
	}
	if currentUserID != payload.MatchedUserID {
		return nil, ErrForbidden
	}

	if err := s.linkPendingIdentity(ctx, currentUserID, payload); err != nil {
		return nil, err
	}
	if err := s.oauth.flows.DeleteByToken(ctx, token); err != nil {
		return nil, err
	}

	return &OAuthCallbackResult{
		RedirectPath: s.resolveLinkCompletionPath(ctx, currentUserID, payload.NextPath, payload.Provider),
	}, nil
}

func (s *AuthService) StartLocalAccountMerge(ctx context.Context, currentUserID int64, login, password string) (string, error) {
	if currentUserID <= 0 {
		return "", ErrUnauthorized
	}

	otherUser, err := s.authenticateLocalUser(ctx, login, password)
	if err != nil {
		return "", err
	}
	if otherUser.ID == currentUserID {
		return "", errors.Join(ErrConflict, ErrAlreadyLinked)
	}

	return s.createMergeFlow(ctx, currentUserID, otherUser.ID, "local_merge", "")
}

func (s *AuthService) CompleteMergeFlow(ctx context.Context, token string, currentUserID int64, displayName string) (*OAuthCallbackResult, error) {
	if currentUserID <= 0 {
		return nil, ErrUnauthorized
	}

	flow, err := s.getValidAuthFlow(ctx, token)
	if err != nil {
		return nil, err
	}
	if flow.Kind != authFlowKindAccountMerge {
		return nil, ErrInvalidInput
	}

	var payload accountMergeFlowPayload
	if err := decodeJSONPayload(flow.Payload, &payload); err != nil {
		return nil, ErrInvalidInput
	}
	if currentUserID != payload.CanonicalUserID && currentUserID != payload.SourceUserID {
		return nil, ErrForbidden
	}

	canonicalUser, canonicalStatuses, sourceUser, sourceStatuses, _, finalUsername, _, err := s.buildMergePreview(ctx, payload.CanonicalUserID, payload.SourceUserID)
	if err != nil {
		return nil, err
	}

	chosenDisplayName := strings.TrimSpace(displayName)
	if chosenDisplayName == "" {
		chosenDisplayName = strings.TrimSpace(payload.DefaultDisplayName)
	}
	if chosenDisplayName == "" {
		chosenDisplayName = userPublicDisplayName(canonicalUser)
	}
	if len([]rune(chosenDisplayName)) > maxDisplayNameLength {
		return nil, ErrInvalidInput
	}

	taken, err := s.isMergeDisplayNameTaken(ctx, chosenDisplayName, canonicalUser.ID, sourceUser.ID)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrDisplayNameTaken
	}
	if err := s.validateMergeIdentities(canonicalStatuses, sourceStatuses); err != nil {
		return nil, err
	}

	targetPassHash := canonicalUser.PassHash
	targetEmail := canonicalUser.Email
	targetUsername := canonicalUser.Username
	if !userHasPassword(canonicalUser) && userHasPassword(sourceUser) {
		targetPassHash = sourceUser.PassHash
		targetEmail = sourceUser.Email
		targetUsername = sourceUser.Username
	}

	if err := s.oauth.accounts.MergeUsers(ctx, repo.AccountMergeInput{
		TargetUserID:             canonicalUser.ID,
		SourceUserID:             sourceUser.ID,
		DisplayName:              normalizeStoredDisplayName(chosenDisplayName),
		TargetEmail:              targetEmail,
		TargetUsername:           targetUsername,
		TargetPassHash:           targetPassHash,
		TargetProfileInitialized: canonicalUser.ProfileInitialized || sourceUser.ProfileInitialized,
		Now:                      s.clock.Now(),
	}); err != nil {
		return nil, err
	}
	if err := s.oauth.flows.DeleteByToken(ctx, token); err != nil {
		return nil, err
	}

	session, err := s.createSessionForUser(ctx, canonicalUser.ID)
	if err != nil {
		return nil, err
	}

	redirectPath := appendQueryParam(profilePathForUsername(finalUsername), "merged", "1")
	return &OAuthCallbackResult{
		Session:      session,
		RedirectPath: redirectPath,
	}, nil
}

func (s *AuthService) UnlinkAccount(ctx context.Context, currentUserID int64, provider string) error {
	if currentUserID <= 0 {
		return ErrUnauthorized
	}
	if s.oauth == nil {
		return ErrOAuthProviderUnavailable
	}

	user, err := s.GetUserByID(ctx, currentUserID)
	if err != nil {
		return err
	}
	if _, err := s.oauth.identities.GetByUserProvider(ctx, currentUserID, provider); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	linkedCount, err := s.oauth.identities.CountByUserID(ctx, currentUserID)
	if err != nil {
		return err
	}
	if !userHasPassword(user) && linkedCount <= 1 {
		return ErrUnlinkDenied
	}

	if err := s.oauth.identities.DeleteByUserProvider(ctx, currentUserID, provider); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *AuthService) handleExistingOAuthIdentity(ctx context.Context, currentUserID int64, payload oauthStateFlowPayload, identity *oauth.Identity, existing *domain.AuthIdentity) (*OAuthCallbackResult, error) {
	existing.ProviderEmail = identity.Email
	existing.ProviderEmailVerified = identity.EmailVerified
	existing.ProviderDisplayName = strings.TrimSpace(identity.DisplayName)
	existing.ProviderAvatarURL = strings.TrimSpace(identity.AvatarURL)
	existing.LastLoginAt = s.clock.Now()

	if err := s.oauth.identities.Update(ctx, existing); err != nil {
		return nil, err
	}

	if currentUserID > 0 {
		if existing.UserID == currentUserID {
			return &OAuthCallbackResult{
				RedirectPath: s.resolveLinkCompletionPath(ctx, currentUserID, payload.NextPath, identity.Provider),
			}, nil
		}

		mergeToken, err := s.createMergeFlow(ctx, currentUserID, existing.UserID, "provider_conflict", identity.Provider)
		if err != nil {
			return nil, err
		}
		return &OAuthCallbackResult{
			RedirectPath: "/account-merge?flow=" + url.QueryEscape(mergeToken),
		}, nil
	}

	session, err := s.createSessionForUser(ctx, existing.UserID)
	if err != nil {
		return nil, err
	}

	return &OAuthCallbackResult{
		Session:      session,
		RedirectPath: resolveNextPath(payload.NextPath, "/"),
	}, nil
}

func (s *AuthService) handleFreshOAuthIdentity(ctx context.Context, currentUserID int64, payload oauthStateFlowPayload, identity *oauth.Identity) (*OAuthCallbackResult, error) {
	if currentUserID > 0 {
		if err := s.ensureProviderSlotFree(ctx, currentUserID, identity.Provider); err != nil {
			return nil, err
		}
		if err := s.createIdentity(ctx, currentUserID, identity); err != nil {
			return nil, err
		}
		return &OAuthCallbackResult{
			RedirectPath: s.resolveLinkCompletionPath(ctx, currentUserID, payload.NextPath, identity.Provider),
		}, nil
	}

	matchedUser, err := s.users.GetByEmailCI(ctx, identity.Email)
	if err == nil {
		if err := s.ensureProviderSlotFree(ctx, matchedUser.ID, identity.Provider); err != nil {
			return nil, err
		}
		linkToken, err := s.createAccountLinkFlow(ctx, matchedUser.ID, payload.NextPath, identity)
		if err != nil {
			return nil, err
		}
		return &OAuthCallbackResult{
			RedirectPath: "/account-link?flow=" + url.QueryEscape(linkToken),
		}, nil
	}
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}

	return s.createNewOAuthUser(ctx, payload.NextPath, identity)
}

func (s *AuthService) createNewOAuthUser(ctx context.Context, nextPath string, identity *oauth.Identity) (*OAuthCallbackResult, error) {
	if s.oauth == nil || s.oauth.accounts == nil {
		return nil, ErrOAuthProviderUnavailable
	}

	username, err := s.generateOAuthUsername(ctx, identity)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now()
	user := &domain.User{
		Email:       identity.Email,
		Username:    username,
		DisplayName: strings.TrimSpace(identity.DisplayName),
		CreatedAt:   now,
	}
	if strings.EqualFold(strings.TrimSpace(user.DisplayName), strings.TrimSpace(username)) {
		user.DisplayName = ""
	}

	linkIdentity := &domain.AuthIdentity{
		Provider:              identity.Provider,
		ProviderUserID:        identity.ProviderUserID,
		ProviderEmail:         identity.Email,
		ProviderEmailVerified: identity.EmailVerified,
		ProviderDisplayName:   strings.TrimSpace(identity.DisplayName),
		ProviderAvatarURL:     strings.TrimSpace(identity.AvatarURL),
		LinkedAt:              now,
		LastLoginAt:           now,
	}

	userID, err := s.oauth.accounts.CreateUserWithIdentity(ctx, user, linkIdentity)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrConflict
		}
		return nil, err
	}

	session, err := s.createSessionForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &OAuthCallbackResult{
		Session:      session,
		RedirectPath: resolveNextPath(nextPath, "/"),
	}, nil
}

func (s *AuthService) createAccountLinkFlow(ctx context.Context, matchedUserID int64, nextPath string, identity *oauth.Identity) (string, error) {
	token, err := s.idGen.New()
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(accountLinkFlowPayload{
		Provider:       identity.Provider,
		ProviderUserID: identity.ProviderUserID,
		Email:          identity.Email,
		EmailVerified:  identity.EmailVerified,
		DisplayName:    strings.TrimSpace(identity.DisplayName),
		Username:       strings.TrimSpace(identity.Username),
		AvatarURL:      strings.TrimSpace(identity.AvatarURL),
		MatchedUserID:  matchedUserID,
		NextPath:       normalizeNextPath(nextPath),
	})
	if err != nil {
		return "", err
	}

	now := s.clock.Now()
	if err := s.oauth.flows.Create(ctx, &domain.AuthFlow{
		Token:     token,
		Kind:      authFlowKindAccountLink,
		Payload:   string(payload),
		CreatedAt: now,
		ExpiresAt: now.Add(authDecisionFlowTTL),
	}); err != nil {
		return "", err
	}

	return token, nil
}

func (s *AuthService) createMergeFlow(ctx context.Context, currentUserID, otherUserID int64, reason, provider string) (string, error) {
	if currentUserID <= 0 || otherUserID <= 0 || currentUserID == otherUserID {
		return "", ErrInvalidInput
	}
	if s.oauth == nil || s.oauth.accounts == nil {
		return "", ErrOAuthProviderUnavailable
	}

	canonicalUser, canonicalStatuses, sourceUser, sourceStatuses, _, _, _, err := s.buildMergePreview(ctx, currentUserID, otherUserID)
	if err != nil {
		return "", err
	}
	if err := s.validateMergeIdentities(canonicalStatuses, sourceStatuses); err != nil {
		return "", err
	}

	token, err := s.idGen.New()
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(accountMergeFlowPayload{
		CanonicalUserID:    canonicalUser.ID,
		SourceUserID:       sourceUser.ID,
		DefaultDisplayName: userPublicDisplayName(canonicalUser),
		Reason:             strings.TrimSpace(reason),
		Provider:           strings.TrimSpace(provider),
	})
	if err != nil {
		return "", err
	}

	now := s.clock.Now()
	if err := s.oauth.flows.Create(ctx, &domain.AuthFlow{
		Token:     token,
		Kind:      authFlowKindAccountMerge,
		UserID:    currentUserID,
		Payload:   string(payload),
		CreatedAt: now,
		ExpiresAt: now.Add(authDecisionFlowTTL),
	}); err != nil {
		return "", err
	}

	return token, nil
}

func (s *AuthService) buildMergePreview(ctx context.Context, userAID, userBID int64) (*domain.User, []LinkedAccountStatus, *domain.User, []LinkedAccountStatus, string, string, int64, error) {
	userA, err := s.GetUserByID(ctx, userAID)
	if err != nil {
		return nil, nil, nil, nil, "", "", 0, err
	}
	userB, err := s.GetUserByID(ctx, userBID)
	if err != nil {
		return nil, nil, nil, nil, "", "", 0, err
	}

	hasDM, err := s.oauth.accounts.HasDirectMessagesBetweenUsers(ctx, userAID, userBID)
	if err != nil {
		return nil, nil, nil, nil, "", "", 0, err
	}
	if hasDM {
		return nil, nil, nil, nil, "", "", 0, ErrMergeDenied
	}

	statusA, err := s.listLinkedAccounts(ctx, userA)
	if err != nil {
		return nil, nil, nil, nil, "", "", 0, err
	}
	statusB, err := s.listLinkedAccounts(ctx, userB)
	if err != nil {
		return nil, nil, nil, nil, "", "", 0, err
	}

	canonicalUser, sourceUser := chooseCanonicalMergeUsers(userA, userB)
	canonicalStatuses := statusA
	sourceStatuses := statusB
	if canonicalUser.ID != userA.ID {
		canonicalStatuses, sourceStatuses = statusB, statusA
	}

	finalEmail := canonicalUser.Email
	finalUsername := canonicalUser.Username
	localLoginUserID := canonicalUser.ID
	if !userHasPassword(canonicalUser) && userHasPassword(sourceUser) {
		finalEmail = sourceUser.Email
		finalUsername = sourceUser.Username
		localLoginUserID = sourceUser.ID
	}
	if !userHasPassword(canonicalUser) && !userHasPassword(sourceUser) {
		localLoginUserID = 0
	}

	return canonicalUser, canonicalStatuses, sourceUser, sourceStatuses, finalEmail, finalUsername, localLoginUserID, nil
}

func (s *AuthService) linkPendingIdentity(ctx context.Context, userID int64, payload accountLinkFlowPayload) error {
	existingIdentity, err := s.oauth.identities.GetByProviderUserID(ctx, payload.Provider, payload.ProviderUserID)
	if err == nil {
		if existingIdentity.UserID == userID {
			return nil
		}
		return ErrConflict
	}
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return err
	}
	if err := s.ensureProviderSlotFree(ctx, userID, payload.Provider); err != nil {
		return err
	}

	if _, err := s.oauth.identities.Create(ctx, &domain.AuthIdentity{
		UserID:                userID,
		Provider:              payload.Provider,
		ProviderUserID:        payload.ProviderUserID,
		ProviderEmail:         payload.Email,
		ProviderEmailVerified: payload.EmailVerified,
		ProviderDisplayName:   strings.TrimSpace(payload.DisplayName),
		ProviderAvatarURL:     strings.TrimSpace(payload.AvatarURL),
		LinkedAt:              s.clock.Now(),
		LastLoginAt:           s.clock.Now(),
	}); err != nil {
		if isUniqueConstraintError(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (s *AuthService) createIdentity(ctx context.Context, userID int64, identity *oauth.Identity) error {
	if _, err := s.oauth.identities.Create(ctx, &domain.AuthIdentity{
		UserID:                userID,
		Provider:              identity.Provider,
		ProviderUserID:        identity.ProviderUserID,
		ProviderEmail:         identity.Email,
		ProviderEmailVerified: identity.EmailVerified,
		ProviderDisplayName:   strings.TrimSpace(identity.DisplayName),
		ProviderAvatarURL:     strings.TrimSpace(identity.AvatarURL),
		LinkedAt:              s.clock.Now(),
		LastLoginAt:           s.clock.Now(),
	}); err != nil {
		if isUniqueConstraintError(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (s *AuthService) ensureProviderSlotFree(ctx context.Context, userID int64, provider string) error {
	if s.oauth == nil {
		return ErrOAuthProviderUnavailable
	}

	existing, err := s.oauth.identities.GetByUserProvider(ctx, userID, provider)
	if err == nil && existing != nil {
		return errors.Join(ErrConflict, ErrAlreadyLinked)
	}
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return err
	}
	return nil
}

func (s *AuthService) listLinkedAccounts(ctx context.Context, user *domain.User) ([]LinkedAccountStatus, error) {
	if user == nil || s.oauth == nil || s.oauth.identities == nil {
		return []LinkedAccountStatus{}, nil
	}

	identities, err := s.oauth.identities.ListByUserID(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	statusByProvider := make(map[string]LinkedAccountStatus)
	for _, info := range s.OAuthProviders() {
		statusByProvider[normalizeProviderName(info.Name)] = LinkedAccountStatus{
			Provider: normalizeProviderName(info.Name),
			Label:    info.Label,
			Enabled:  info.Enabled,
		}
	}

	linkedCount := len(identities)
	for _, identity := range identities {
		provider := normalizeProviderName(identity.Provider)
		status := statusByProvider[provider]
		status.Provider = provider
		if strings.TrimSpace(status.Label) == "" {
			status.Label = s.providerLabel(provider)
		}
		status.Enabled = status.Enabled || s.providerEnabled(provider)
		status.Linked = true
		status.Email = strings.TrimSpace(identity.ProviderEmail)
		status.LastLoginAt = identity.LastLoginAt
		status.CanUnlink = userHasPassword(user) || linkedCount > 1
		statusByProvider[provider] = status
	}

	statuses := make([]LinkedAccountStatus, 0, len(statusByProvider))
	for _, status := range statusByProvider {
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Provider < statuses[j].Provider
	})
	return statuses, nil
}

func (s *AuthService) oauthProvider(name string) (oauth.Provider, error) {
	if s.oauth == nil || s.oauth.providers == nil {
		return nil, ErrOAuthProviderUnavailable
	}
	provider, err := s.oauth.providers.Get(name)
	if err == nil {
		return provider, nil
	}
	if errors.Is(err, oauth.ErrProviderNotFound) || errors.Is(err, oauth.ErrProviderDisabled) {
		return nil, ErrOAuthProviderUnavailable
	}
	return nil, err
}

func (s *AuthService) getValidAuthFlow(ctx context.Context, token string) (*domain.AuthFlow, error) {
	if s.oauth == nil || s.oauth.flows == nil {
		return nil, ErrOAuthProviderUnavailable
	}

	flow, err := s.oauth.flows.GetByToken(ctx, strings.TrimSpace(token))
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if flow.ExpiresAt.Before(s.clock.Now()) {
		_ = s.oauth.flows.DeleteByToken(ctx, flow.Token)
		return nil, ErrAuthFlowExpired
	}
	return flow, nil
}

func (s *AuthService) decodeFlowPayload(flow *domain.AuthFlow, expectedKind string, out any) error {
	if flow == nil || flow.Kind != expectedKind {
		return ErrInvalidInput
	}
	if flow.ExpiresAt.Before(s.clock.Now()) {
		return ErrAuthFlowExpired
	}
	return decodeJSONPayload(flow.Payload, out)
}

func (s *AuthService) generateOAuthUsername(ctx context.Context, identity *oauth.Identity) (string, error) {
	base := sanitizeUsernameCandidate(preferredUsernameBase(identity))
	if base == "" {
		base = "user"
	}

	for i := 0; i < maxUsernameVariants; i++ {
		candidate := base
		if i > 0 {
			suffix := fmt.Sprintf("-%d", i+1)
			candidate = truncateUsername(base, len(suffix)) + suffix
		}

		_, err := s.users.GetByUsernameCI(ctx, candidate)
		if errors.Is(err, repo.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}

	return "", ErrConflict
}

func (s *AuthService) authenticateLocalUser(ctx context.Context, login, password string) (*domain.User, error) {
	login = strings.TrimSpace(login)
	password = strings.TrimSpace(password)
	if login == "" || password == "" {
		return nil, ErrInvalidInput
	}

	user, err := s.findLoginUser(ctx, login, "")
	if err != nil {
		return nil, err
	}
	if !userHasPassword(user) {
		return nil, ErrUnauthorized
	}
	if err := s.hasher.Compare(user.PassHash, password); err != nil {
		return nil, ErrUnauthorized
	}
	return user, nil
}

func (s *AuthService) resolveLinkCompletionPath(ctx context.Context, userID int64, nextPath, provider string) string {
	fallback := appendQueryParam(s.profilePath(ctx, userID), "linked", normalizeProviderName(provider))
	return appendQueryParam(resolveNextPath(nextPath, fallback), "linked", normalizeProviderName(provider))
}

func (s *AuthService) profilePath(ctx context.Context, userID int64) string {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return "/"
	}
	return profilePathForUsername(user.Username)
}

func (s *AuthService) providerLabel(name string) string {
	switch normalizeProviderName(name) {
	case "google":
		return "Google"
	case "github":
		return "GitHub"
	case "facebook":
		return "Facebook"
	default:
		name = strings.TrimSpace(name)
		if name == "" {
			return "Provider"
		}
		runes := []rune(strings.ToLower(name))
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	}
}

func (s *AuthService) providerEnabled(name string) bool {
	if s.oauth == nil || s.oauth.providers == nil {
		return false
	}
	_, err := s.oauth.providers.Get(name)
	return err == nil
}

func (s *AuthService) validateMergeIdentities(canonical, source []LinkedAccountStatus) error {
	seen := make(map[string]struct{})
	for _, status := range canonical {
		if !status.Linked {
			continue
		}
		seen[normalizeProviderName(status.Provider)] = struct{}{}
	}
	for _, status := range source {
		if !status.Linked {
			continue
		}
		if _, ok := seen[normalizeProviderName(status.Provider)]; ok {
			return ErrMergeDenied
		}
	}
	return nil
}

func (s *AuthService) isMergeDisplayNameTaken(ctx context.Context, displayName string, keepUserIDs ...int64) (bool, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return false, nil
	}

	keep := make(map[int64]struct{}, len(keepUserIDs))
	for _, userID := range keepUserIDs {
		if userID > 0 {
			keep[userID] = struct{}{}
		}
	}

	user, err := s.users.GetByUsernameCI(ctx, displayName)
	if err == nil {
		if _, ok := keep[user.ID]; !ok {
			return true, nil
		}
	}
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return false, err
	}

	user, err = s.users.GetByDisplayNameCI(ctx, displayName)
	if err == nil {
		if _, ok := keep[user.ID]; !ok {
			return true, nil
		}
	}
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return false, err
	}

	return false, nil
}

func chooseCanonicalMergeUsers(userA, userB *domain.User) (*domain.User, *domain.User) {
	if userA.CreatedAt.Before(userB.CreatedAt) {
		return userA, userB
	}
	if userB.CreatedAt.Before(userA.CreatedAt) {
		return userB, userA
	}
	if userA.ID <= userB.ID {
		return userA, userB
	}
	return userB, userA
}

func normalizeExternalIdentity(providerName string, identity *oauth.Identity) (*oauth.Identity, error) {
	if identity == nil {
		return nil, ErrOAuthIdentityFetchFailed
	}

	normalized := &oauth.Identity{
		Provider:       normalizeProviderName(identity.Provider),
		ProviderUserID: strings.TrimSpace(identity.ProviderUserID),
		Email:          strings.TrimSpace(identity.Email),
		EmailVerified:  identity.EmailVerified,
		DisplayName:    strings.TrimSpace(identity.DisplayName),
		Username:       strings.TrimSpace(identity.Username),
		AvatarURL:      strings.TrimSpace(identity.AvatarURL),
	}
	if normalized.Provider == "" {
		normalized.Provider = normalizeProviderName(providerName)
	}
	if normalized.ProviderUserID == "" {
		return nil, ErrOAuthIdentityFetchFailed
	}
	if normalized.Email == "" {
		return nil, ErrOAuthEmailUnavailable
	}
	if normalized.Provider == "google" && !normalized.EmailVerified {
		return nil, ErrOAuthEmailUnavailable
	}
	return normalized, nil
}

func mapOAuthProviderError(err error) error {
	switch {
	case errors.Is(err, oauth.ErrTokenExchange):
		return ErrOAuthTokenExchangeFailed
	case errors.Is(err, oauth.ErrIdentityFetch):
		return ErrOAuthIdentityFetchFailed
	case errors.Is(err, oauth.ErrEmailUnavailable):
		return ErrOAuthEmailUnavailable
	default:
		return err
	}
}

func decodeJSONPayload(payload string, out any) error {
	if strings.TrimSpace(payload) == "" {
		return ErrInvalidInput
	}
	return json.Unmarshal([]byte(payload), out)
}

func normalizeProviderName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

func normalizeNextPath(nextPath string) string {
	nextPath = strings.TrimSpace(nextPath)
	if nextPath == "" || !strings.HasPrefix(nextPath, "/") || strings.HasPrefix(nextPath, "//") {
		return ""
	}
	if strings.HasPrefix(nextPath, "/api/") || strings.HasPrefix(nextPath, "/auth/") {
		return ""
	}
	return nextPath
}

func resolveNextPath(nextPath, fallback string) string {
	nextPath = normalizeNextPath(nextPath)
	if nextPath != "" {
		return nextPath
	}
	if fallback == "" {
		return "/"
	}
	return fallback
}

func appendQueryParam(rawPath, key, value string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		rawPath = "/"
	}
	u, err := url.Parse(rawPath)
	if err != nil {
		return rawPath
	}
	query := u.Query()
	query.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	u.RawQuery = query.Encode()
	return u.String()
}

func profilePathForUsername(username string) string {
	username = normalizeUsername(username)
	if username == "" {
		return "/"
	}
	return "/u/" + url.PathEscape(username)
}

func preferredUsernameBase(identity *oauth.Identity) string {
	candidates := []string{
		strings.TrimSpace(identity.Username),
		strings.TrimSpace(identity.DisplayName),
		emailLocalPart(identity.Email),
		strings.TrimSpace(identity.Provider) + "-" + strings.TrimSpace(identity.ProviderUserID),
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return "user"
}

func sanitizeUsernameCandidate(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	var builder strings.Builder
	lastSeparator := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastSeparator = false
		case r == '-' || r == '_' || r == '.':
			if builder.Len() == 0 || lastSeparator {
				continue
			}
			builder.WriteRune(r)
			lastSeparator = true
		default:
			if builder.Len() == 0 || lastSeparator {
				continue
			}
			builder.WriteRune('-')
			lastSeparator = true
		}
	}

	result := strings.Trim(builder.String(), "-_.")
	if len(result) > maxGeneratedUsernameLen {
		result = strings.Trim(result[:maxGeneratedUsernameLen], "-_.")
	}
	return result
}

func truncateUsername(base string, suffixLen int) string {
	if suffixLen < 0 {
		suffixLen = 0
	}
	limit := maxGeneratedUsernameLen - suffixLen
	if limit < 1 {
		limit = 1
	}
	if len(base) <= limit {
		return base
	}
	return strings.Trim(base[:limit], "-_.")
}

func emailLocalPart(email string) string {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	if at <= 0 {
		return email
	}
	return email[:at]
}

func userHasPassword(user *domain.User) bool {
	return user != nil && strings.TrimSpace(user.PassHash) != ""
}

func userPublicDisplayName(user *domain.User) string {
	if user == nil {
		return "user"
	}
	displayName := strings.TrimSpace(user.DisplayName)
	if displayName != "" {
		return displayName
	}
	username := strings.TrimSpace(user.Username)
	if username != "" {
		return username
	}
	return "user"
}

func linkedOnly(statuses []LinkedAccountStatus) []LinkedAccountStatus {
	filtered := make([]LinkedAccountStatus, 0, len(statuses))
	for _, status := range statuses {
		if status.Linked {
			filtered = append(filtered, status)
		}
	}
	return filtered
}

func isUniqueConstraintError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
