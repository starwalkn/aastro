package main

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/starwalkn/aastro/internal/logger"
	"github.com/starwalkn/aastro/sdk"
)

type ctxKeyClaims struct{}

type keyResolver interface {
	KeyFunc(token *jwt.Token) (any, error)
}

// closeable is implemented by resolvers that hold background resources.
type closeable interface {
	stop()
}

type Middleware struct {
	issuer    string
	audience  string
	realm     string
	resolver  keyResolver
	jwtConfig jwtConfig
	log       *zap.Logger
}

type jwtConfig struct {
	alg string

	hmacSecret          []byte         // For HS256.
	rsaPublicKey        *rsa.PublicKey // For static RS256.
	jwksURL             string         // For JWKS.
	jwksRefreshTimeout  time.Duration
	jwksRefreshInterval time.Duration
}

type authChallenge struct {
	errorCode        string
	errorDescription string
}

const (
	defaultLeeway        = 5 * time.Second
	authHeaderPartsCount = 2

	defaultRealm     = "aastro"
	bearerAuthScheme = "Bearer"

	authErrorInvalidRequest = "invalid_request"
	authErrorInvalidToken   = "invalid_token"
)

func NewMiddleware() sdk.Middleware {
	return &Middleware{}
}

func (m *Middleware) Name() string {
	return "auth"
}

func (m *Middleware) Init(config map[string]interface{}) error {
	issuer, ok := config["issuer"].(string)
	if !ok {
		return errors.New("missing issuer")
	}

	audience, ok := config["audience"].(string)
	if !ok {
		return errors.New("missing audience")
	}

	alg, ok := config["alg"].(string)
	if !ok || alg == "" {
		return errors.New("missing or invalid alg")
	}

	m.issuer = issuer
	m.audience = audience
	m.realm = defaultRealm

	rawRealm, hasRealm := config["realm"]
	if hasRealm {
		realm, isString := rawRealm.(string)
		if !isString {
			return errors.New("realm must be a string")
		}

		realm = strings.TrimSpace(realm)
		if realm != "" {
			m.realm = realm
		}
	}

	cfg := jwtConfig{alg: alg}

	hmacSecret, err := parseHMACSecret(config, "hmac_secret")
	if err != nil {
		return err
	}

	cfg.hmacSecret = hmacSecret

	rsaPub, err := parseRSAPublicKey(config, "rsa_public_key")
	if err != nil {
		return err
	}

	cfg.rsaPublicKey = rsaPub

	if url, urlOk := config["jwks_url"].(string); urlOk {
		cfg.jwksURL = url
	}

	cfg.jwksRefreshTimeout = parseDuration(config, "jwks_refresh_timeout", defaultJWKSRefreshTimeout)
	cfg.jwksRefreshInterval = parseDuration(config, "jwks_refresh_interval", defaultJWKSRefreshInterval)

	m.jwtConfig = cfg

	resolver, err := m.newKeyResolver(m.jwtConfig)
	if err != nil {
		return err
	}

	m.resolver = resolver

	log, err := logger.New(false)
	if err != nil {
		return err
	}
	m.log = log

	return nil
}

func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			m.unauthorized(w, authChallenge{})
			return
		}

		parts := strings.SplitN(authHeader, " ", authHeaderPartsCount)
		if len(parts) != authHeaderPartsCount || !strings.EqualFold(parts[0], "Bearer") {
			m.unauthorized(w, authChallenge{
				errorCode:        authErrorInvalidRequest,
				errorDescription: "invalid authorization header",
			})
			return
		}

		token, err := jwt.ParseWithClaims(
			parts[1],
			&jwt.MapClaims{},
			m.resolver.KeyFunc,
			jwt.WithValidMethods([]string{m.jwtConfig.alg}),
			jwt.WithLeeway(defaultLeeway),
		)
		if err != nil || !token.Valid {
			m.unauthorized(w, authChallenge{
				errorCode:        authErrorInvalidToken,
				errorDescription: "invalid or expired token",
			})
			return
		}

		claims, ok := token.Claims.(*jwt.MapClaims)
		if !ok {
			m.unauthorized(w, authChallenge{
				errorCode:        authErrorInvalidToken,
				errorDescription: "invalid token claims",
			})
			return
		}

		if err = m.validateClaims(claims); err != nil {
			m.unauthorized(w, authChallenge{
				errorCode:        authErrorInvalidToken,
				errorDescription: "invalid token claims",
			})
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyClaims{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Middleware) Close() {
	if c, ok := m.resolver.(closeable); ok {
		c.stop()
	}
}

func (m *Middleware) validateClaims(claims *jwt.MapClaims) error {
	issuer, err := claims.GetIssuer()
	if err != nil || issuer != m.issuer {
		return errors.New("invalid issuer")
	}

	audience, err := claims.GetAudience()
	if err != nil || !slices.Contains(audience, m.audience) {
		return errors.New("invalid audience")
	}

	return nil
}

func (m *Middleware) newKeyResolver(cfg jwtConfig) (keyResolver, error) {
	switch cfg.alg {
	case jwt.SigningMethodHS256.Alg():
		if len(cfg.hmacSecret) == 0 {
			return nil, errors.New("HMAC secret not configured")
		}

		return &hmacResolver{HMACSecret: cfg.hmacSecret}, nil

	case jwt.SigningMethodRS256.Alg():
		if cfg.jwksURL != "" {
			return m.newJWKSResolver(cfg)
		}

		if cfg.rsaPublicKey != nil {
			return &rsaResolver{RSAPublic: cfg.rsaPublicKey}, nil
		}

		return nil, errors.New("RSA public key not configured")

	default:
		return nil, fmt.Errorf("unsupported signing method: %s", cfg.alg)
	}
}

func (m *Middleware) newJWKSResolver(cfg jwtConfig) (keyResolver, error) {
	r := &jwksResolver{
		url:             cfg.jwksURL,
		keys:            make(map[string]*rsa.PublicKey),
		mu:              sync.RWMutex{},
		refreshTimeout:  cfg.jwksRefreshTimeout,
		refreshInterval: cfg.jwksRefreshInterval,
		stopCh:          make(chan struct{}),
	}

	if err := r.refresh(cfg.jwksRefreshTimeout); err != nil {
		return nil, fmt.Errorf("initial JWKS fetch failed: %w", err)
	}

	r.start(func(err error) {
		m.log.Error("JWKS background refresh failed", zap.Error(err))
	})

	return r, nil
}

func (m *Middleware) unauthorized(w http.ResponseWriter, c authChallenge) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(
		"WWW-Authenticate",
		buildWWWAuthenticateHeader(m.realm, c.errorCode, c.errorDescription),
	)

	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED"}]}`))
}

func buildWWWAuthenticateHeader(realm, errorCode, errorDescription string) string {
	realm = strings.TrimSpace(realm)

	if realm == "" {
		realm = defaultRealm
	}

	header := bearerAuthScheme + " realm=" + strconv.Quote(realm)

	if errorCode == "" {
		return header
	}

	header += ", error=" + strconv.Quote(errorCode)

	if errorDescription != "" {
		header += ", error_description=" + strconv.Quote(errorDescription)
	}

	return header
}

func parseDuration(config map[string]interface{}, key string, fallback time.Duration) time.Duration {
	raw, ok := config[key].(string)
	if !ok {
		return fallback
	}

	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}

	return d
}
