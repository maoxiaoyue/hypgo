package websocket

// 重放保護（SecurityConfig.ReplayProtection）的線上格式。
//
// 每個 frame（一律 binary）：
//
//	[seq：8 bytes，big-endian][安全管線的輸出]
//
// 安全管線與未開啟時相同（SignThenEncrypt 決定順序），差別是 HMAC 與 AES-GCM 都把下面
// 25 bytes 當成附加資料（HMAC 簽 ad || data；GCM 以 ad 為 additional data），ad 本身不送出：
//
//	ad = [方向：1 byte][session：16 bytes][seq：8 bytes，big-endian]
//	方向：伺服器 → 客戶端 = 0x01，客戶端 → 伺服器 = 0x02
//
// 連線建立後伺服器先送一個 hello frame：seq = 0、session 以 16 個 0x00 代入、一律使用 Hub 的金鑰，
// 內容是這條連線的 session（16 bytes 隨機值）。之後雙方各自從 seq = 1 起遞增；
// 收到的 seq 必須大於同方向上一則通過驗證的 seq。
//
// 效果：別的方向（把伺服器送出的訊息送回）、別條連線、或已收過的訊息，驗證都不會通過。
// 仍然擋不住的：能改動連線內容的中間人把某個客戶端的訊息改由自己的連線依序轉送
// （它得先讓該客戶端採用自己連線的 session）。這需要傳輸層保護（wss），或每個客戶端各自的金鑰
// （Client.SetEncryptionKey / SetHMACKey）。hello 一律用 Hub 的金鑰（送出時這條連線的金鑰可能還沒設定），
// 之後的訊息用這條連線的金鑰：Go 客戶端以 Hub 的金鑰建立 ClientSession，再呼叫 SetDataKeys。

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	dirServerToClient byte = 0x01
	dirClientToServer byte = 0x02

	replaySessionSize = 16
	replaySeqSize     = 8
)

var errReplay = errors.New("replayed or out-of-order message")

// replayAD 組出附加資料：方向 || session || seq
func replayAD(dir byte, session *[replaySessionSize]byte, seq uint64) []byte {
	ad := make([]byte, 1+replaySessionSize+replaySeqSize)
	ad[0] = dir
	copy(ad[1:], session[:])
	binary.BigEndian.PutUint64(ad[1+replaySessionSize:], seq)
	return ad
}

// replayFrame 以 seq 與 ad 保護 payload，回傳 [seq][管線輸出]
func replayFrame(payload []byte, gcm cipher.AEAD, hmacKey []byte, ste bool, dir byte, session *[replaySessionSize]byte, seq uint64) []byte {
	body := protect(payload, gcm, hmacKey, ste, replayAD(dir, session, seq))
	out := make([]byte, replaySeqSize, replaySeqSize+len(body))
	binary.BigEndian.PutUint64(out, seq)
	return append(out, body...)
}

// replayOpen 驗證 [seq][管線輸出]：seq 必須大於 last，回傳內容與 seq
func replayOpen(frame []byte, gcm cipher.AEAD, hmacKey []byte, ste bool, dir byte, session *[replaySessionSize]byte, last uint64) ([]byte, uint64, error) {
	if len(frame) < replaySeqSize {
		return nil, 0, fmt.Errorf("frame too short: %d bytes", len(frame))
	}
	seq := binary.BigEndian.Uint64(frame)
	if seq <= last {
		return nil, 0, errReplay
	}
	data, err := unprotect(frame[replaySeqSize:], gcm, hmacKey, ste, replayAD(dir, session, seq))
	if err != nil {
		return nil, 0, err
	}
	return data, seq, nil
}

// replayState 伺服器端一條連線的重放保護狀態。
// outSeq 只由 writePump 使用、inSeq 只由 readPump 使用
type replayState struct {
	session [replaySessionSize]byte
	outSeq  uint64
	inSeq   uint64
}

func newReplayState() *replayState {
	rs := &replayState{}
	rand.Read(rs.session[:]) // Go 1.24 起 crypto/rand.Read 不會回傳錯誤
	return rs
}

// hello 回傳連線的第一個 frame（seq 0、session 以零值代入），內容為 session
func (rs *replayState) hello(gcm cipher.AEAD, hmacKey []byte, ste bool) []byte {
	var zero [replaySessionSize]byte
	return replayFrame(rs.session[:], gcm, hmacKey, ste, dirServerToClient, &zero, 0)
}

// sealOut 保護一則出站訊息（依呼叫順序編號，必須與寫入順序相同）
func (rs *replayState) sealOut(payload []byte, gcm cipher.AEAD, hmacKey []byte, ste bool) []byte {
	rs.outSeq++
	return replayFrame(payload, gcm, hmacKey, ste, dirServerToClient, &rs.session, rs.outSeq)
}

// openIn 驗證一則入站訊息；通過後才推進 inSeq
func (rs *replayState) openIn(frame []byte, gcm cipher.AEAD, hmacKey []byte, ste bool) ([]byte, error) {
	data, seq, err := replayOpen(frame, gcm, hmacKey, ste, dirClientToServer, &rs.session, rs.inSeq)
	if err != nil {
		return nil, err
	}
	rs.inSeq = seq
	return data, nil
}

// ClientSession 是重放保護的客戶端這一側，供 Go 客戶端與測試使用：
// 收到的每個 frame 交給 Open（第一個是 hello），要送出的內容經 Seal。
// 不可並行使用（讀與寫各自呼叫時需自行加鎖）
type ClientSession struct {
	gcm     cipher.AEAD // hello 用（Hub 的金鑰）
	hmacKey []byte
	dataGCM cipher.AEAD // hello 之後的訊息用；沒有呼叫 SetDataKeys 時與上面相同
	dataMAC []byte
	ste     bool
	session [replaySessionSize]byte
	ready   bool
	in, out uint64
}

// NewClientSession 以與伺服器相同的金鑰與 SignThenEncrypt 建立；aesKey 為空表示不加密，
// hmacKey 為空表示不簽名（至少要有一個）
func NewClientSession(aesKey, hmacKey []byte, signThenEncrypt bool) (*ClientSession, error) {
	s := &ClientSession{ste: signThenEncrypt}
	if len(aesKey) > 0 {
		gcm, err := newAEAD(aesKey)
		if err != nil {
			return nil, err
		}
		s.gcm = gcm
	}
	if len(hmacKey) > 0 {
		s.hmacKey = append([]byte(nil), hmacKey...)
	}
	if s.gcm == nil && s.hmacKey == nil {
		return nil, errors.New("websocket: ClientSession needs an AES key or an HMAC key")
	}
	s.dataGCM, s.dataMAC = s.gcm, s.hmacKey
	return s, nil
}

// SetDataKeys 設定 hello 之後的訊息使用的金鑰，對應伺服器端的 Client.SetEncryptionKey / SetHMACKey；
// 空的那一個沿用建立時的金鑰（與伺服器端「沒設定的沿用 Hub 的」相同）。
// 先前 ClientSession 只有一組金鑰：用 Hub 的金鑰建立，之後每則訊息驗證失敗；
// 用這條連線的金鑰建立，hello 就失敗——重放保護與每個客戶端各自的金鑰無法並用
func (s *ClientSession) SetDataKeys(aesKey, hmacKey []byte) error {
	if len(aesKey) > 0 {
		gcm, err := newAEAD(aesKey)
		if err != nil {
			return err
		}
		s.dataGCM = gcm
	}
	if len(hmacKey) > 0 {
		s.dataMAC = append([]byte(nil), hmacKey...)
	}
	return nil
}

// Open 驗證伺服器送來的 frame 並回傳內容。第一個 frame 是 hello：hello 為 true、沒有內容
func (s *ClientSession) Open(frame []byte) (payload []byte, hello bool, err error) {
	if !s.ready {
		if len(frame) < replaySeqSize || binary.BigEndian.Uint64(frame) != 0 {
			return nil, false, errors.New("websocket: expected hello frame")
		}
		var zero [replaySessionSize]byte
		data, err := unprotect(frame[replaySeqSize:], s.gcm, s.hmacKey, s.ste, replayAD(dirServerToClient, &zero, 0))
		if err != nil {
			return nil, false, err
		}
		if len(data) != replaySessionSize {
			return nil, false, errors.New("websocket: malformed hello frame")
		}
		copy(s.session[:], data)
		s.ready = true
		return nil, true, nil
	}
	data, seq, err := replayOpen(frame, s.dataGCM, s.dataMAC, s.ste, dirServerToClient, &s.session, s.in)
	if err != nil {
		return nil, false, err
	}
	s.in = seq
	return data, false, nil
}

// Seal 保護要送給伺服器的內容；必須先收到 hello，且依回傳的順序送出
func (s *ClientSession) Seal(payload []byte) ([]byte, error) {
	if !s.ready {
		return nil, errors.New("websocket: Seal before hello frame")
	}
	s.out++
	return replayFrame(payload, s.dataGCM, s.dataMAC, s.ste, dirClientToServer, &s.session, s.out), nil
}
