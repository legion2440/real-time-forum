package http

import (
	"context"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type RateLimitConfig struct {
	Requests int
	Interval time.Duration
	Burst    int
}

type SecurityOptions struct {
	Now                func() time.Time
	ClientIPResolver   ClientIPResolver
	StartCleanup       bool
	CleanupInterval    time.Duration
	StaleAfter         time.Duration
	LoginWindow        time.Duration
	GlobalHTTP         RateLimitConfig
	AuthEndpoints      RateLimitConfig
	WriteActions       RateLimitConfig
	WebSocketHandshake RateLimitConfig
	LoginBackoff       []time.Duration
	MaxLoginBackoff    time.Duration
}

type ClientIPResolver interface {
	ClientIP(*http.Request) string
}

type Security struct {
	clientIPResolver ClientIPResolver
	globalHTTP       *tokenBucketStore
	authEndpoints    *tokenBucketStore
	writeActions     *tokenBucketStore
	wsHandshake      *tokenBucketStore
	loginThrottler   *loginThrottler

	stopCleanup chan struct{}
	cleanupDone chan struct{}
	closeOnce   sync.Once
}

func NewSecurity(opts SecurityOptions) *Security {
	opts = opts.withDefaults()

	security := &Security{
		clientIPResolver: opts.ClientIPResolver,
		globalHTTP:       newTokenBucketStore(opts.Now, opts.GlobalHTTP, opts.StaleAfter),
		authEndpoints:    newTokenBucketStore(opts.Now, opts.AuthEndpoints, opts.StaleAfter),
		writeActions:     newTokenBucketStore(opts.Now, opts.WriteActions, opts.StaleAfter),
		wsHandshake:      newTokenBucketStore(opts.Now, opts.WebSocketHandshake, opts.StaleAfter),
		loginThrottler:   newLoginThrottler(opts.Now, opts.LoginBackoff, opts.MaxLoginBackoff, opts.LoginWindow, opts.StaleAfter),
	}

	if opts.StartCleanup {
		security.stopCleanup = make(chan struct{})
		security.cleanupDone = make(chan struct{})
		go security.runCleanup(opts.CleanupInterval)
	}

	return security
}

func (s *Security) Close() {
	if s == nil || s.stopCleanup == nil {
		return
	}

	s.closeOnce.Do(func() {
		close(s.stopCleanup)
		<-s.cleanupDone
	})
}

func (s *Security) allowGlobalRequest(r *http.Request) (bool, time.Duration) {
	return s.allowByIP(s.globalHTTP, r)
}

func (s *Security) allowAuthEndpoint(r *http.Request) (bool, time.Duration) {
	return s.allowByIP(s.authEndpoints, r)
}

func (s *Security) allowWriteAction(ctx context.Context, r *http.Request) (bool, time.Duration) {
	if s == nil || s.writeActions == nil {
		return true, 0
	}

	key := s.writeActionKey(ctx, r)
	if key == "" {
		return true, 0
	}

	return s.writeActions.allow(key)
}

func (s *Security) allowWebSocketHandshake(r *http.Request) (bool, time.Duration) {
	if r == nil || !websocket.IsWebSocketUpgrade(r) {
		return true, 0
	}
	return s.allowByIP(s.wsHandshake, r)
}

func (s *Security) localAuthRetryAfter(identifier string) time.Duration {
	if s == nil || s.loginThrottler == nil {
		return 0
	}
	return s.loginThrottler.wait(normalizeLocalAuthIdentifier(identifier))
}

func (s *Security) recordLocalAuthFailure(identifier string) {
	if s == nil || s.loginThrottler == nil {
		return
	}
	s.loginThrottler.failure(normalizeLocalAuthIdentifier(identifier))
}

func (s *Security) resetLocalAuthFailures(identifier string) {
	if s == nil || s.loginThrottler == nil {
		return
	}
	s.loginThrottler.success(normalizeLocalAuthIdentifier(identifier))
}

func (s *Security) writeActionKey(ctx context.Context, r *http.Request) string {
	if userID, ok := userIDFromContext(ctx); ok && userID > 0 {
		return "user:" + strconv.FormatInt(userID, 10)
	}

	clientIP := s.clientIP(r)
	if clientIP == "" {
		return ""
	}
	return "ip:" + clientIP
}

func (s *Security) allowByIP(store *tokenBucketStore, r *http.Request) (bool, time.Duration) {
	if s == nil || store == nil {
		return true, 0
	}

	clientIP := s.clientIP(r)
	if clientIP == "" {
		return true, 0
	}

	return store.allow(clientIP)
}

func (s *Security) clientIP(r *http.Request) string {
	if s == nil || s.clientIPResolver == nil {
		return ""
	}
	return s.clientIPResolver.ClientIP(r)
}

func (s *Security) runCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer func() {
		ticker.Stop()
		close(s.cleanupDone)
	}()

	for {
		select {
		case <-ticker.C:
			s.cleanup()
		case <-s.stopCleanup:
			return
		}
	}
}

func (s *Security) cleanup() {
	if s == nil {
		return
	}

	if s.globalHTTP != nil {
		s.globalHTTP.cleanup()
	}
	if s.authEndpoints != nil {
		s.authEndpoints.cleanup()
	}
	if s.writeActions != nil {
		s.writeActions.cleanup()
	}
	if s.wsHandshake != nil {
		s.wsHandshake.cleanup()
	}
	if s.loginThrottler != nil {
		s.loginThrottler.cleanup()
	}
}

func (opts SecurityOptions) withDefaults() SecurityOptions {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ClientIPResolver == nil {
		opts.ClientIPResolver = remoteAddrClientIPResolver{}
	}
	if opts.CleanupInterval <= 0 {
		opts.CleanupInterval = time.Minute
	}
	if opts.StaleAfter <= 0 {
		opts.StaleAfter = 15 * time.Minute
	}
	if opts.LoginWindow <= 0 {
		opts.LoginWindow = 15 * time.Minute
	}
	opts.GlobalHTTP = withDefaultRateLimit(opts.GlobalHTTP, 300, time.Minute, 60)
	opts.AuthEndpoints = withDefaultRateLimit(opts.AuthEndpoints, 10, time.Minute, 5)
	opts.WriteActions = withDefaultRateLimit(opts.WriteActions, 30, time.Minute, 10)
	opts.WebSocketHandshake = withDefaultRateLimit(opts.WebSocketHandshake, 10, time.Minute, 3)
	if len(opts.LoginBackoff) == 0 {
		opts.LoginBackoff = []time.Duration{
			1 * time.Second,
			2 * time.Second,
			4 * time.Second,
			8 * time.Second,
			16 * time.Second,
			30 * time.Second,
		}
	}
	if opts.MaxLoginBackoff <= 0 {
		opts.MaxLoginBackoff = 30 * time.Second
	}
	return opts
}

func withDefaultRateLimit(cfg RateLimitConfig, requests int, interval time.Duration, burst int) RateLimitConfig {
	if cfg.Requests <= 0 {
		cfg.Requests = requests
	}
	if cfg.Interval <= 0 {
		cfg.Interval = interval
	}
	if cfg.Burst <= 0 {
		cfg.Burst = burst
	}
	return cfg
}

type remoteAddrClientIPResolver struct{}

func (remoteAddrClientIPResolver) ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}

	host := strings.TrimSpace(r.RemoteAddr)
	if host == "" {
		return ""
	}

	if parsedIP := net.ParseIP(host); parsedIP != nil {
		return parsedIP.String()
	}

	host, _, err := net.SplitHostPort(host)
	if err != nil {
		return ""
	}

	parsedIP := net.ParseIP(strings.TrimSpace(host))
	if parsedIP == nil {
		return ""
	}
	return parsedIP.String()
}

func normalizeLocalAuthIdentifier(identifier string) string {
	return strings.ToLower(strings.TrimSpace(identifier))
}

type tokenBucketStore struct {
	mu     sync.Mutex
	now    func() time.Time
	config tokenBucketConfig
	states map[string]*tokenBucketState
}

type tokenBucketConfig struct {
	tokensPerSecond float64
	capacity        float64
	staleAfter      time.Duration
}

type tokenBucketState struct {
	tokens    float64
	updatedAt time.Time
	lastSeen  time.Time
}

func newTokenBucketStore(now func() time.Time, cfg RateLimitConfig, staleAfter time.Duration) *tokenBucketStore {
	if now == nil {
		now = time.Now
	}
	if cfg.Requests <= 0 || cfg.Interval <= 0 || cfg.Burst <= 0 {
		return nil
	}

	return &tokenBucketStore{
		now: now,
		config: tokenBucketConfig{
			tokensPerSecond: float64(cfg.Requests) / cfg.Interval.Seconds(),
			capacity:        float64(cfg.Burst),
			staleAfter:      staleAfter,
		},
		states: make(map[string]*tokenBucketState),
	}
}

func (s *tokenBucketStore) allow(key string) (bool, time.Duration) {
	if s == nil || strings.TrimSpace(key) == "" {
		return true, 0
	}

	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.stateForLocked(key, now)
	if state.tokens >= 1 {
		state.tokens--
		state.lastSeen = now
		return true, 0
	}

	if s.config.tokensPerSecond <= 0 {
		return false, time.Second
	}

	neededTokens := 1 - state.tokens
	retryAfter := time.Duration(math.Ceil((neededTokens / s.config.tokensPerSecond) * float64(time.Second)))
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	state.lastSeen = now
	return false, retryAfter
}

func (s *tokenBucketStore) stateForLocked(key string, now time.Time) *tokenBucketState {
	state, ok := s.states[key]
	if !ok {
		state = &tokenBucketState{
			tokens:    s.config.capacity,
			updatedAt: now,
			lastSeen:  now,
		}
		s.states[key] = state
	}

	if elapsed := now.Sub(state.updatedAt); elapsed > 0 && s.config.tokensPerSecond > 0 {
		state.tokens = math.Min(s.config.capacity, state.tokens+(elapsed.Seconds()*s.config.tokensPerSecond))
		state.updatedAt = now
	}
	if state.tokens > s.config.capacity {
		state.tokens = s.config.capacity
	}
	state.lastSeen = now
	return state
}

func (s *tokenBucketStore) cleanup() {
	if s == nil || s.config.staleAfter <= 0 {
		return
	}

	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, state := range s.states {
		if state == nil {
			delete(s.states, key)
			continue
		}
		if now.Sub(state.lastSeen) >= s.config.staleAfter {
			delete(s.states, key)
		}
	}
}

type loginThrottler struct {
	mu          sync.Mutex
	now         func() time.Time
	backoff     []time.Duration
	maxBackoff  time.Duration
	window      time.Duration
	staleAfter  time.Duration
	identifiers map[string]*loginThrottleState
}

type loginThrottleState struct {
	failures     []time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

func newLoginThrottler(now func() time.Time, backoff []time.Duration, maxBackoff, window, staleAfter time.Duration) *loginThrottler {
	if now == nil {
		now = time.Now
	}
	return &loginThrottler{
		now:         now,
		backoff:     append([]time.Duration(nil), backoff...),
		maxBackoff:  maxBackoff,
		window:      window,
		staleAfter:  staleAfter,
		identifiers: make(map[string]*loginThrottleState),
	}
}

func (t *loginThrottler) wait(identifier string) time.Duration {
	identifier = normalizeLocalAuthIdentifier(identifier)
	if t == nil || identifier == "" {
		return 0
	}

	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.identifiers[identifier]
	if !ok || state == nil {
		return 0
	}
	t.pruneFailuresLocked(state, now)

	state.lastSeen = now
	if !now.Before(state.blockedUntil) {
		return 0
	}
	return state.blockedUntil.Sub(now)
}

func (t *loginThrottler) failure(identifier string) time.Duration {
	identifier = normalizeLocalAuthIdentifier(identifier)
	if t == nil || identifier == "" {
		return 0
	}

	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.identifiers[identifier]
	if !ok || state == nil {
		state = &loginThrottleState{}
		t.identifiers[identifier] = state
	}

	t.pruneFailuresLocked(state, now)
	state.failures = append(state.failures, now)
	delay := t.delayForFailures(len(state.failures))
	if delay > 0 {
		state.blockedUntil = now.Add(delay)
	} else {
		state.blockedUntil = time.Time{}
	}
	state.lastSeen = now
	return delay
}

func (t *loginThrottler) success(identifier string) {
	identifier = normalizeLocalAuthIdentifier(identifier)
	if t == nil || identifier == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.identifiers, identifier)
}

func (t *loginThrottler) cleanup() {
	if t == nil || t.staleAfter <= 0 {
		return
	}

	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()

	for identifier, state := range t.identifiers {
		if state == nil {
			delete(t.identifiers, identifier)
			continue
		}
		t.pruneFailuresLocked(state, now)
		if now.Sub(state.lastSeen) >= t.staleAfter || (len(state.failures) == 0 && !now.Before(state.blockedUntil)) {
			delete(t.identifiers, identifier)
		}
	}
}

func (t *loginThrottler) delayForFailures(failures int) time.Duration {
	if failures <= 4 {
		return 0
	}

	index := failures - 5
	if index >= len(t.backoff) {
		index = len(t.backoff) - 1
	}
	if index < 0 {
		return 0
	}

	delay := t.backoff[index]
	if t.maxBackoff > 0 && delay > t.maxBackoff {
		return t.maxBackoff
	}
	return delay
}

func (t *loginThrottler) pruneFailuresLocked(state *loginThrottleState, now time.Time) {
	if t == nil || state == nil || len(state.failures) == 0 || t.window <= 0 {
		return
	}

	keepFrom := 0
	for keepFrom < len(state.failures) && now.Sub(state.failures[keepFrom]) > t.window {
		keepFrom++
	}
	if keepFrom == 0 {
		return
	}
	state.failures = append([]time.Time(nil), state.failures[keepFrom:]...)
}
