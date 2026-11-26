package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// On macOS the app encrypts with Chromium's scheme: a password in the login
// keychain ("Claude Safe Storage"), stretched with PBKDF2, and values prefixed
// "v10" encrypted with AES-128-CBC. macOS asks once whether the switcher may
// read that keychain item.

var (
	keyMu     sync.Mutex
	keyVal    []byte
	keyErr    error
	keyFailed time.Time
	// keyLoading is closed when the keychain read in flight ends.
	keyLoading chan struct{}
)

const (
	// macOS waits for the user to answer its prompt; one left open must not
	// hold the switcher up for good.
	keyWait = time.Minute
	// After a denied or failed read the keychain is asked again this much
	// later, not on every check.
	keyRetry = 5 * time.Minute
	// A caller waits this long for a read in flight, then goes on without.
	keyCallerWait = 10 * time.Second
)

// appKey returns the app's key. The keychain is read in the background, one
// read at a time, so a prompt nobody answers holds up no caller for long.
func appKey() ([]byte, error) {
	keyMu.Lock()
	if keyVal == nil && keyLoading == nil && time.Since(keyFailed) >= keyRetry {
		done := make(chan struct{})
		keyLoading = done
		go func() {
			v, err := loadAppKey()
			keyMu.Lock()
			keyVal, keyErr, keyLoading = v, err, nil
			if err != nil {
				keyFailed = time.Now()
			}
			keyMu.Unlock()
			close(done)
		}()
	}
	val, err, loading := keyVal, keyErr, keyLoading
	keyMu.Unlock()
	if loading == nil {
		return val, err
	}
	select {
	case <-loading:
		keyMu.Lock()
		defer keyMu.Unlock()
		return keyVal, keyErr
	case <-time.After(keyCallerWait):
		return nil, errors.New("waiting for keychain access")
	}
}

func loadAppKey() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keyWait)
	defer cancel()
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-w", "-s", "Claude Safe Storage").Output()
	if err != nil {
		return nil, errors.New("could not read \"Claude Safe Storage\" from the keychain")
	}
	return pbkdf2.Key(sha1.New, strings.TrimSpace(string(out)), []byte("saltysalt"), 1003, 16)
}

func decryptValue(data []byte) ([]byte, error) {
	if len(data) < 3+aes.BlockSize || string(data[:3]) != "v10" {
		return nil, errors.New("unexpected encrypted value format")
	}
	key, err := appKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	ct := data[3:]
	if len(ct)%aes.BlockSize != 0 {
		return nil, errors.New("bad ciphertext length")
	}
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte{' '}, aes.BlockSize)).CryptBlocks(plain, ct)
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(plain) {
		return nil, errors.New("bad padding")
	}
	return plain[:len(plain)-pad], nil
}
