package websocket

import (
	"bytes"
	"encoding/json"
	"testing"
)

// ===== AES Tests =====

func TestAESEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plaintext := []byte("hello, AES-256-GCM encryption!")

	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Error("Ciphertext should differ from plaintext")
	}

	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Decrypted data mismatch:\ngot:  %q\nwant: %q", decrypted, plaintext)
	}
}

func TestAESEncryptDecryptWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	key2[0] = 0xFF

	ciphertext, err := Encrypt([]byte("secret data"), key1)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(ciphertext, key2)
	if err == nil {
		t.Error("Decrypt with wrong key should fail")
	}
}

func TestAESEncryptDecryptEmptyData(t *testing.T) {
	key := make([]byte, 32)

	ciphertext, err := Encrypt([]byte{}, key)
	if err != nil {
		t.Fatalf("Encrypt empty data failed: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if len(decrypted) != 0 {
		t.Errorf("Expected empty, got %d bytes", len(decrypted))
	}
}

func TestAESEncryptDecryptLargeData(t *testing.T) {
	key := make([]byte, 32)
	largeData := make([]byte, 65536)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	ciphertext, err := Encrypt(largeData, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if !bytes.Equal(decrypted, largeData) {
		t.Error("Large data round-trip failed")
	}
}

func TestAESNonceUniqueness(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("same data")

	c1, _ := Encrypt(plaintext, key)
	c2, _ := Encrypt(plaintext, key)

	if bytes.Equal(c1, c2) {
		t.Error("Two encryptions of same data should produce different ciphertext (unique nonces)")
	}
}

// ===== HMAC Tests =====

func TestHMACSignVerifyRoundTrip(t *testing.T) {
	key := []byte("hmac-secret-key-for-testing")
	data := []byte("important message to sign")

	signed := Sign(data, key)
	if len(signed) != 32+len(data) {
		t.Errorf("Signed length: got %d, want %d", len(signed), 32+len(data))
	}

	verified, err := Verify(signed, key)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !bytes.Equal(verified, data) {
		t.Errorf("Verified data mismatch:\ngot:  %q\nwant: %q", verified, data)
	}
}

func TestHMACVerifyWrongKey(t *testing.T) {
	key1 := []byte("key-one")
	key2 := []byte("key-two")

	signed := Sign([]byte("data"), key1)
	_, err := Verify(signed, key2)
	if err == nil {
		t.Error("Verify with wrong key should fail")
	}
}

func TestHMACVerifyTamperedData(t *testing.T) {
	key := []byte("secret")
	signed := Sign([]byte("original"), key)

	// 竄改資料部分
	signed[33] ^= 0xFF

	_, err := Verify(signed, key)
	if err == nil {
		t.Error("Verify of tampered data should fail")
	}
}

func TestHMACSignedDataTooShort(t *testing.T) {
	key := []byte("key")
	_, err := Verify([]byte("short"), key)
	if err == nil {
		t.Error("Verify of too-short data should fail")
	}
}

// ===== Security Pipeline Tests =====

func TestSecurityPipelineSignThenEncrypt(t *testing.T) {
	aesKey := make([]byte, 32)
	hmacKey := []byte("hmac-key-for-pipeline-test")

	sec := &SecurityConfig{
		AESKey:          aesKey,
		HMACKey:         hmacKey,
		SignThenEncrypt: true,
	}

	client := &Client{
		ID:       "test-client",
		metadata: make(map[string]interface{}),
		Channels: make(map[string]bool),
	}

	original := []byte(`{"type":"message","data":"hello"}`)

	// 出站
	secured, err := applySecurityOut(original, client, sec)
	if err != nil {
		t.Fatalf("applySecurityOut failed: %v", err)
	}
	if bytes.Equal(secured, original) {
		t.Error("Secured data should differ from original")
	}

	// 入站
	restored, err := applySecurityIn(secured, client, sec)
	if err != nil {
		t.Fatalf("applySecurityIn failed: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Errorf("Restored data mismatch:\ngot:  %q\nwant: %q", restored, original)
	}
}

func TestSecurityPipelineEncryptThenSign(t *testing.T) {
	aesKey := make([]byte, 32)
	hmacKey := []byte("hmac-key")

	sec := &SecurityConfig{
		AESKey:          aesKey,
		HMACKey:         hmacKey,
		SignThenEncrypt: false,
	}

	client := &Client{
		ID:       "test-client",
		metadata: make(map[string]interface{}),
		Channels: make(map[string]bool),
	}

	original := []byte("encrypt-then-sign payload")

	secured, err := applySecurityOut(original, client, sec)
	if err != nil {
		t.Fatalf("Out failed: %v", err)
	}

	restored, err := applySecurityIn(secured, client, sec)
	if err != nil {
		t.Fatalf("In failed: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Error("Round-trip failed")
	}
}

func TestSecurityPipelineAESOnly(t *testing.T) {
	sec := &SecurityConfig{AESKey: make([]byte, 32)}
	client := &Client{metadata: make(map[string]interface{}), Channels: make(map[string]bool)}
	original := []byte("aes-only")

	secured, err := applySecurityOut(original, client, sec)
	if err != nil {
		t.Fatalf("Out failed: %v", err)
	}

	restored, err := applySecurityIn(secured, client, sec)
	if err != nil {
		t.Fatalf("In failed: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Error("AES-only round-trip failed")
	}
}

func TestSecurityPipelineHMACOnly(t *testing.T) {
	sec := &SecurityConfig{HMACKey: []byte("hmac-only-key")}
	client := &Client{metadata: make(map[string]interface{}), Channels: make(map[string]bool)}
	original := []byte("hmac-only")

	secured, err := applySecurityOut(original, client, sec)
	if err != nil {
		t.Fatalf("Out failed: %v", err)
	}

	restored, err := applySecurityIn(secured, client, sec)
	if err != nil {
		t.Fatalf("In failed: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Error("HMAC-only round-trip failed")
	}
}

func TestSecurityPipelineNoSecurity(t *testing.T) {
	sec := &SecurityConfig{}
	client := &Client{metadata: make(map[string]interface{}), Channels: make(map[string]bool)}
	original := []byte("passthrough")

	secured, err := applySecurityOut(original, client, sec)
	if err != nil {
		t.Fatalf("Out failed: %v", err)
	}
	if !bytes.Equal(secured, original) {
		t.Error("No-security should pass through unchanged")
	}

	restored, err := applySecurityIn(secured, client, sec)
	if err != nil {
		t.Fatalf("In failed: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Error("No-security round-trip should be identity")
	}
}

func TestSecurityPipelineNilConfig(t *testing.T) {
	client := &Client{metadata: make(map[string]interface{}), Channels: make(map[string]bool)}
	original := []byte("nil config")

	secured, err := applySecurityOut(original, client, nil)
	if err != nil {
		t.Fatalf("Out failed: %v", err)
	}
	if !bytes.Equal(secured, original) {
		t.Error("nil config should pass through")
	}
}

func TestPerClientKeyOverride(t *testing.T) {
	hubKey := make([]byte, 32)
	clientKey := make([]byte, 32)
	clientKey[0] = 0xFF // 不同金鑰

	sec := &SecurityConfig{AESKey: hubKey}

	client := &Client{
		ID:       "custom-key",
		metadata: make(map[string]interface{}),
		Channels: make(map[string]bool),
	}
	client.SetEncryptionKey(clientKey)

	original := []byte("per-client encrypted")

	// 使用 per-client key 加密
	secured, err := applySecurityOut(original, client, sec)
	if err != nil {
		t.Fatalf("Out failed: %v", err)
	}

	// 使用 per-client key 解密（應該成功）
	restored, err := applySecurityIn(secured, client, sec)
	if err != nil {
		t.Fatalf("In failed: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Error("Per-client key round-trip failed")
	}

	// 使用另一個 client（沒有覆寫金鑰，使用 hub key）解密應該失敗
	otherClient := &Client{
		ID:       "other",
		metadata: make(map[string]interface{}),
		Channels: make(map[string]bool),
	}
	_, err = applySecurityIn(secured, otherClient, sec)
	if err == nil {
		t.Error("Decrypt with hub key (different from per-client key) should fail")
	}
}

func TestMarshalForClientsWithSecurity(t *testing.T) {
	aesKey := make([]byte, 32)
	hmacKey := []byte("test-hmac-key")
	sec := &SecurityConfig{
		AESKey:          aesKey,
		HMACKey:         hmacKey,
		SignThenEncrypt: true,
	}

	client := &Client{
		ID:          "secured-client",
		codec:       JSONCodec{},
		wsFrameType: 1,
		Send:        make(chan []byte, 10),
		Channels:    make(map[string]bool),
		metadata:    make(map[string]interface{}),
	}

	msg := &Message{
		Type:    "message",
		Channel: "secure-ch",
		Data:    json.RawMessage(`{"secret":"data"}`),
	}

	marshalForClients(msg, []*Client{client}, sec, nil)

	if len(client.Send) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(client.Send))
	}

	secured := <-client.Send

	// 密文不應該是有效 JSON
	testMsg := &Message{}
	if err := json.Unmarshal(secured, testMsg); err == nil {
		t.Error("Secured data should not be valid JSON")
	}

	// 解密後應該是有效的
	decrypted, err := applySecurityIn(secured, client, sec)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if err := json.Unmarshal(decrypted, testMsg); err != nil {
		t.Errorf("Decrypted data should be valid JSON: %v", err)
	}
	if testMsg.Type != "message" {
		t.Errorf("Type: got %q, want %q", testMsg.Type, "message")
	}
}
