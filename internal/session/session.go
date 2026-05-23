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
	"net/http"
	"strings"
	"time"
)

const CookieName = "session"

type Manager struct {
	encryptionKey []byte
	signingKey    []byte
	now           func() time.Time
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

func New() (*Manager, error) {
	encryptionKey := make([]byte, 32)
	signingKey := make([]byte, 32)
	if _, err := rand.Read(encryptionKey); err != nil {
		return nil, err
	}
	if _, err := rand.Read(signingKey); err != nil {
		return nil, err
	}
	return &Manager{
		encryptionKey: encryptionKey,
		signingKey:    signingKey,
		now:           time.Now,
	}, nil
}

func NewForTest(encryptionKey, signingKey []byte, now func() time.Time) *Manager {
	return &Manager{encryptionKey: encryptionKey, signingKey: signingKey, now: now}
}

func (m *Manager) Password(r *http.Request, shareKey string) string {
	store := m.read(r)
	payload, ok := store[shareKey]
	if !ok {
		return ""
	}
	decrypted, err := m.decrypt(payload)
	if err != nil {
		return ""
	}
	var decoded passwordPayload
	if err := json.Unmarshal([]byte(decrypted), &decoded); err != nil {
		return ""
	}
	if decoded.Expires.After(m.now()) {
		return decoded.Password
	}
	return ""
}

func (m *Manager) SetPassword(w http.ResponseWriter, r *http.Request, shareKey, password string) {
	store := m.read(r)
	data, _ := json.Marshal(passwordPayload{
		Password: password,
		Expires:  m.now().Add(time.Hour),
	})
	encrypted, err := m.encrypt(string(data))
	if err != nil {
		return
	}
	store[shareKey] = encrypted
	m.write(w, store)
}

func (m *Manager) ClearKey(w http.ResponseWriter, r *http.Request, shareKey string) {
	store := m.read(r)
	delete(store, shareKey)
	m.write(w, store)
}

func (m *Manager) read(r *http.Request) Store {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return Store{}
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 || !m.validSignature(parts[0], parts[1]) {
		return Store{}
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Store{}
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return Store{}
	}
	if store == nil {
		return Store{}
	}
	return store
}

func (m *Manager) write(w http.ResponseWriter, store Store) {
	data, err := json.Marshal(store)
	if err != nil {
		return
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	value := encoded + "." + m.signature(encoded)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (m *Manager) encrypt(text string) (encryptedPayload, error) {
	block, err := aes.NewCipher(m.encryptionKey)
	if err != nil {
		return encryptedPayload{}, err
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return encryptedPayload{}, err
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
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (m *Manager) validSignature(value, sig string) bool {
	expected := m.signature(value)
	return hmac.Equal([]byte(expected), []byte(sig))
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
