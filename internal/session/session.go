package session

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const CookieName = "session"

type CookieOptions struct {
	Path     string
	HTTPOnly bool
	SameSite http.SameSite
	Secure   bool
	TTL      time.Duration
}

type Manager struct {
	encryptionKey []byte
	signingKey    []byte
	now           func() time.Time
	options       CookieOptions
	logger        *slog.Logger
}

type encryptedPayload struct {
	IV string `json:"iv"`
	CR string `json:"cr"`
}

type passwordPayload struct {
	Password string    `json:"password"`
	Expires  time.Time `json:"expires"`
}

type Store map[string]encryptedPayload

func DefaultCookieOptions() CookieOptions {
	return CookieOptions{
		Path:     "/",
		HTTPOnly: true,
		SameSite: http.SameSiteStrictMode,
		TTL:      time.Hour,
	}
}

func NewManager(secret []byte, now func() time.Time, options CookieOptions, logger *slog.Logger) (*Manager, error) {
	if len(secret) == 0 {
		return nil, errors.New("session secret is required")
	}
	if now == nil {
		now = time.Now
	}
	if options.Path == "" {
		options = mergeCookieOptions(options, DefaultCookieOptions())
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{
		encryptionKey: deriveKey(secret, "encryption"),
		signingKey:    deriveKey(secret, "signing"),
		now:           now,
		options:       options,
		logger:        logger,
	}, nil
}

func NewForTest(secret []byte, now func() time.Time, options CookieOptions, logger *slog.Logger) *Manager {
	manager, err := NewManager(secret, now, options, logger)
	if err != nil {
		panic(err)
	}
	return manager
}

func (m *Manager) Password(r *http.Request, shareKey string) string {
	store, err := m.read(r)
	if err != nil {
		m.logger.Debug("read session cookie", "error", err)
		return ""
	}
	payload, ok := store[shareKey]
	if !ok {
		return ""
	}
	decrypted, err := m.decrypt(payload)
	if err != nil {
		m.logger.Debug("decrypt session entry", "share_key", shareKey, "error", err)
		return ""
	}
	var decoded passwordPayload
	if err := json.Unmarshal([]byte(decrypted), &decoded); err != nil {
		m.logger.Debug("decode session entry", "share_key", shareKey, "error", err)
		return ""
	}
	if decoded.Expires.After(m.now()) {
		return decoded.Password
	}
	return ""
}

func (m *Manager) SetPassword(w http.ResponseWriter, r *http.Request, shareKey, password string) error {
	store, err := m.read(r)
	if err != nil {
		m.logger.Debug("read session cookie before set", "error", err)
		store = Store{}
	}
	data, err := json.Marshal(passwordPayload{
		Password: password,
		Expires:  m.now().Add(m.options.TTL),
	})
	if err != nil {
		return fmt.Errorf("marshal session payload: %w", err)
	}
	encrypted, err := m.encrypt(string(data))
	if err != nil {
		return err
	}
	store[shareKey] = encrypted
	return m.write(w, store)
}

func (m *Manager) ClearKey(w http.ResponseWriter, r *http.Request, shareKey string) error {
	store, err := m.read(r)
	if err != nil {
		m.logger.Debug("read session cookie before clear", "error", err)
		store = Store{}
	}
	delete(store, shareKey)
	return m.write(w, store)
}

func (m *Manager) read(r *http.Request) (Store, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return Store{}, nil
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 || !m.validSignature(parts[0], parts[1]) {
		return Store{}, errors.New("invalid session signature")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Store{}, fmt.Errorf("decode session payload: %w", err)
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return Store{}, fmt.Errorf("unmarshal session payload: %w", err)
	}
	if store == nil {
		return Store{}, nil
	}
	return store, nil
}

func (m *Manager) write(w http.ResponseWriter, store Store) error {
	data, err := json.Marshal(store)
	if err != nil {
		return fmt.Errorf("marshal session store: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	value := encoded + "." + m.signature(encoded)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     m.options.Path,
		HttpOnly: m.options.HTTPOnly,
		SameSite: m.options.SameSite,
		Secure:   m.options.Secure,
		MaxAge:   int(m.options.TTL.Seconds()),
	})
	return nil
}

func (m *Manager) encrypt(text string) (encryptedPayload, error) {
	block, err := aes.NewCipher(m.encryptionKey)
	if err != nil {
		return encryptedPayload{}, err
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return encryptedPayload{}, fmt.Errorf("generate iv: %w", err)
	}
	plaintext := pkcs7Pad([]byte(text), aes.BlockSize)
	ciphertext := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plaintext)
	return encryptedPayload{
		IV: hex.EncodeToString(iv),
		CR: hex.EncodeToString(ciphertext),
	}, nil
}

func (m *Manager) decrypt(payload encryptedPayload) (string, error) {
	iv, err := hex.DecodeString(payload.IV)
	if err != nil {
		return "", err
	}
	ciphertext, err := hex.DecodeString(payload.CR)
	if err != nil {
		return "", err
	}
	if len(iv) != aes.BlockSize || len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("invalid encrypted payload")
	}
	block, err := aes.NewCipher(m.encryptionKey)
	if err != nil {
		return "", err
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	plaintext, err = pkcs7Unpad(plaintext, aes.BlockSize)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (m *Manager) signature(value string) string {
	mac := hmac.New(sha256.New, m.signingKey)
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (m *Manager) validSignature(value, sig string) bool {
	expected := m.signature(value)
	return hmac.Equal([]byte(expected), []byte(sig))
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

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padtext...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid padding size")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, errors.New("invalid padding")
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, errors.New("invalid padding")
		}
	}
	return data[:len(data)-padding], nil
}
