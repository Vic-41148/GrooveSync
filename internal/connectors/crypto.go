// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 GrooveSync Authors

package connectors

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// encryptToken encrypts plaintext using AES-GCM with the provided key. The
// returned string is hex(nonce || ciphertext). If key is nil or empty, the
// plaintext is returned unchanged.
func encryptToken(key []byte, plaintext string) (string, error) {
	if len(key) == 0 {
		return plaintext, nil
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return "", fmt.Errorf("invalid key size: %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := append(nonce, ct...)
	return hex.EncodeToString(out), nil
}

// decryptToken attempts to decrypt a hex(nonce || ciphertext) blob using the
// provided key. If the key is nil or empty, the input is returned unchanged.
// If the input is not valid hex or decryption fails, the original input is
// returned so existing plaintext rows remain readable when no key was used to
// encrypt them.
func decryptToken(key []byte, blob string) (string, error) {
	if len(key) == 0 {
		return blob, nil
	}
	data, err := hex.DecodeString(blob)
	if err != nil {
		// Not hex, assume plaintext stored
		return blob, nil
	}
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return "", fmt.Errorf("invalid key size: %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		// Not a valid ciphertext, treat as plaintext
		return blob, nil
	}
	nonce := data[:ns]
	ct := data[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		// Decryption failed; return original blob so we don't lose data
		return blob, nil
	}
	return string(pt), nil
}

// EncryptToken is a public wrapper for encryptToken so external tools can use
// the same encryption logic without duplicating code.
func EncryptToken(key []byte, plaintext string) (string, error) {
	return encryptToken(key, plaintext)
}

// TryDecryptToken attempts to decrypt a blob with the provided key. It
// returns the plaintext, a boolean indicating whether decryption occurred
// (true when the blob was valid ciphertext and decrypted successfully), and
// an error only for unexpected failures. If the blob is not valid hex or
// decryption fails the original blob is returned and ok=false.
func TryDecryptToken(key []byte, blob string) (string, bool, error) {
	if len(key) == 0 {
		return blob, false, nil
	}
	pt, err := decryptToken(key, blob)
	if err != nil {
		return blob, false, err
	}
	if pt == blob {
		return blob, false, nil
	}
	return pt, true, nil
}
