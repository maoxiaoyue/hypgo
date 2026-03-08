package websocket

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

// SecurityConfig 安全管線配置（AES-256-GCM 加密 + HMAC-SHA256 簽名）
type SecurityConfig struct {
	// AESKey AES-256-GCM 加密金鑰（32 bytes）。nil 表示不加密。
	AESKey []byte

	// HMACKey HMAC-SHA256 簽名金鑰。nil 表示不簽名。
	HMACKey []byte

	// SignThenEncrypt 安全管線操作順序
	// true（預設推薦）：先簽名再加密 — 簽名被加密保護
	// false：先加密再簽名 — 可在解密前拒絕竄改
	SignThenEncrypt bool
}

// ===== AES-256-GCM 加密 =====

// Encrypt 使用 AES-256-GCM 加密資料
// 輸出格式：[nonce (12 bytes)][ciphertext + GCM tag]
func Encrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce generation: %w", err)
	}

	// Seal 會將 nonce 作為前綴
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt 使用 AES-256-GCM 解密資料
// 輸入格式：[nonce (12 bytes)][ciphertext + GCM tag]
func Decrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short: need at least %d bytes, got %d", nonceSize, len(ciphertext))
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// ===== HMAC-SHA256 簽名 =====

// Sign 使用 HMAC-SHA256 簽名資料
// 輸出格式：[32-byte HMAC signature][original data]
func Sign(data, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	sig := mac.Sum(nil) // 32 bytes

	result := make([]byte, 32+len(data))
	copy(result, sig)
	copy(result[32:], data)
	return result
}

// Verify 驗證 HMAC-SHA256 簽名並返回原始資料
// 輸入格式：[32-byte HMAC signature][original data]
func Verify(signed, key []byte) ([]byte, error) {
	if len(signed) < 32 {
		return nil, fmt.Errorf("signed data too short: need at least 32 bytes, got %d", len(signed))
	}

	sig := signed[:32]
	data := signed[32:]

	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	expected := mac.Sum(nil)

	if !hmac.Equal(sig, expected) {
		return nil, fmt.Errorf("HMAC verification failed")
	}
	return data, nil
}

// ===== 安全管線 =====

// getAESKey 取得客戶端的 AES 金鑰（per-client 覆寫 > hub 預設）
func getAESKey(client *Client, hubSec *SecurityConfig) []byte {
	if key, ok := client.GetMetadata("_aes_key"); ok {
		if k, ok := key.([]byte); ok && len(k) > 0 {
			return k
		}
	}
	if hubSec != nil {
		return hubSec.AESKey
	}
	return nil
}

// getHMACKey 取得客戶端的 HMAC 金鑰（per-client 覆寫 > hub 預設）
func getHMACKey(client *Client, hubSec *SecurityConfig) []byte {
	if key, ok := client.GetMetadata("_hmac_key"); ok {
		if k, ok := key.([]byte); ok && len(k) > 0 {
			return k
		}
	}
	if hubSec != nil {
		return hubSec.HMACKey
	}
	return nil
}

// applySecurityOut 出站安全管線：序列化後 → 簽名/加密 → 寫入 wire
func applySecurityOut(data []byte, client *Client, hubSec *SecurityConfig) ([]byte, error) {
	if hubSec == nil {
		return data, nil
	}

	aesKey := getAESKey(client, hubSec)
	hmacKey := getHMACKey(client, hubSec)

	if aesKey == nil && hmacKey == nil {
		return data, nil
	}

	var err error

	if hubSec.SignThenEncrypt {
		// Sign-then-Encrypt（預設）
		if hmacKey != nil {
			data = Sign(data, hmacKey)
		}
		if aesKey != nil {
			data, err = Encrypt(data, aesKey)
			if err != nil {
				return nil, err
			}
		}
	} else {
		// Encrypt-then-Sign
		if aesKey != nil {
			data, err = Encrypt(data, aesKey)
			if err != nil {
				return nil, err
			}
		}
		if hmacKey != nil {
			data = Sign(data, hmacKey)
		}
	}

	return data, nil
}

// applySecurityIn 入站安全管線：從 wire 讀取 → 解密/驗證 → 反序列化
func applySecurityIn(data []byte, client *Client, hubSec *SecurityConfig) ([]byte, error) {
	if hubSec == nil {
		return data, nil
	}

	aesKey := getAESKey(client, hubSec)
	hmacKey := getHMACKey(client, hubSec)

	if aesKey == nil && hmacKey == nil {
		return data, nil
	}

	var err error

	if hubSec.SignThenEncrypt {
		// 反向：先解密，再驗證
		if aesKey != nil {
			data, err = Decrypt(data, aesKey)
			if err != nil {
				return nil, fmt.Errorf("AES decrypt: %w", err)
			}
		}
		if hmacKey != nil {
			data, err = Verify(data, hmacKey)
			if err != nil {
				return nil, fmt.Errorf("HMAC verify: %w", err)
			}
		}
	} else {
		// 反向：先驗證，再解密
		if hmacKey != nil {
			data, err = Verify(data, hmacKey)
			if err != nil {
				return nil, fmt.Errorf("HMAC verify: %w", err)
			}
		}
		if aesKey != nil {
			data, err = Decrypt(data, aesKey)
			if err != nil {
				return nil, fmt.Errorf("AES decrypt: %w", err)
			}
		}
	}

	return data, nil
}
