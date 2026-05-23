package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

const (
	cookieName        = "session"
	defaultVersion    = 1
	defaultMaxEntries = 32
)

type CookieOptions struct {
	Path     string
	HTTPOnly bool
	SameSite http.SameSite
	Secure   bool
	TTL      time.Duration
}

type Store struct {
	aead       cipher.AEAD
	now        func() time.Time
	options    CookieOptions
	maxEntries int
	logger     *slog.Logger
}

type sharePasswordRecord struct {
	Password string    `json:"password"`
	Expires  time.Time `json:"expires"`
}

type cookieState struct {
	Version int                            `json:"version"`
	Shares  map[string]sharePasswordRecord `json:"entries"`
}

func DefaultCookieOptions() CookieOptions {
	return CookieOptions{
		Path:     "/",
		HTTPOnly: true,
		SameSite: http.SameSiteStrictMode,
		TTL:      time.Hour,
	}
}

func New(secret []byte, now func() time.Time, options CookieOptions, logger *slog.Logger) (*Store, error) {
	if len(secret) == 0 {
		return nil, errors.New("session secret is required")
	}
	if now == nil {
		now = time.Now
	}
	options = mergeCookieOptions(options, DefaultCookieOptions())
	if logger == nil {
		logger = slog.Default()
	}

	block, err := aes.NewCipher(deriveKey(secret, "session-aead"))
	if err != nil {
		return nil, fmt.Errorf("build session cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build session aead: %w", err)
	}

	return &Store{
		aead:       aead,
		now:        now,
		options:    options,
		maxEntries: defaultMaxEntries,
		logger:     logger,
	}, nil
}

func NewForTest(secret []byte, now func() time.Time, options CookieOptions, logger *slog.Logger) *Store {
	store, err := New(secret, now, options, logger)
	if err != nil {
		panic(err)
	}
	return store
}

func (s *Store) PasswordForShare(r *http.Request, shareKey string) string {
	state, err := s.read(r)
	if err != nil {
		s.logger.Debug("read session cookie", "error", err)
		return ""
	}
	record, ok := state.Shares[shareKey]
	if !ok || !record.Expires.After(s.now()) {
		return ""
	}
	return record.Password
}

func (s *Store) RememberPassword(w http.ResponseWriter, r *http.Request, shareKey, password string) error {
	state, err := s.read(r)
	if err != nil {
		s.logger.Debug("read session cookie before set", "error", err)
		state = newCookieState()
	}
	state.Shares[shareKey] = sharePasswordRecord{
		Password: password,
		Expires:  s.now().Add(s.options.TTL),
	}
	s.prune(&state)
	return s.write(w, state)
}

func (s *Store) ForgetShare(w http.ResponseWriter, r *http.Request, shareKey string) error {
	state, err := s.read(r)
	if err != nil {
		s.logger.Debug("read session cookie before clear", "error", err)
		state = newCookieState()
	}
	delete(state.Shares, shareKey)
	s.prune(&state)
	return s.write(w, state)
}

func (s *Store) read(r *http.Request) (cookieState, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return newCookieState(), nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return cookieState{}, fmt.Errorf("decode session payload: %w", err)
	}
	nonceSize := s.aead.NonceSize()
	if len(raw) < nonceSize {
		return cookieState{}, errors.New("invalid session payload")
	}

	nonce := raw[:nonceSize]
	ciphertext := raw[nonceSize:]
	plaintext, err := s.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return cookieState{}, fmt.Errorf("decrypt session payload: %w", err)
	}

	var state cookieState
	if err := json.Unmarshal(plaintext, &state); err != nil {
		return cookieState{}, fmt.Errorf("unmarshal session payload: %w", err)
	}
	if state.Version == 0 {
		state.Version = defaultVersion
	}
	if state.Shares == nil {
		state.Shares = map[string]sharePasswordRecord{}
	}
	s.prune(&state)
	return state, nil
}

func (s *Store) write(w http.ResponseWriter, state cookieState) error {
	if len(state.Shares) == 0 {
		s.clearCookie(w)
		return nil
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal session store: %w", err)
	}

	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate session nonce: %w", err)
	}
	ciphertext := s.aead.Seal(nil, nonce, data, nil)
	value := base64.RawURLEncoding.EncodeToString(append(nonce, ciphertext...))

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     s.options.Path,
		HttpOnly: s.options.HTTPOnly,
		SameSite: s.options.SameSite,
		Secure:   s.options.Secure,
		MaxAge:   int(s.options.TTL.Seconds()),
	})
	return nil
}

func (s *Store) prune(state *cookieState) {
	now := s.now()
	for key, record := range state.Shares {
		if !record.Expires.After(now) {
			delete(state.Shares, key)
		}
	}

	if len(state.Shares) <= s.maxEntries {
		return
	}

	keys := make([]string, 0, len(state.Shares))
	for key := range state.Shares {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return state.Shares[a].Expires.Compare(state.Shares[b].Expires)
	})
	for _, key := range keys[:len(keys)-s.maxEntries] {
		delete(state.Shares, key)
	}
}

func (s *Store) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     s.options.Path,
		HttpOnly: s.options.HTTPOnly,
		SameSite: s.options.SameSite,
		Secure:   s.options.Secure,
		MaxAge:   -1,
	})
}

func mergeCookieOptions(current, defaults CookieOptions) CookieOptions {
	if current.Path == "" {
		current.Path = defaults.Path
	}
	if !current.HTTPOnly {
		current.HTTPOnly = defaults.HTTPOnly
	}
	if current.SameSite == 0 {
		current.SameSite = defaults.SameSite
	}
	if current.TTL == 0 {
		current.TTL = defaults.TTL
	}
	return current
}

func deriveKey(secret []byte, label string) []byte {
	sum := sha256.Sum256(append(append([]byte{}, secret...), []byte(":"+label)...))
	return sum[:]
}

func newCookieState() cookieState {
	return cookieState{
		Version: defaultVersion,
		Shares:  map[string]sharePasswordRecord{},
	}
}
