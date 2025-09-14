package context

import (
	"fmt"
	"time"
)

// ===== 上下文資料存儲 =====

// Set 存儲資料到上下文
func (c *Context) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Keys == nil {
		c.Keys = make(map[string]interface{})
	}
	c.Keys[key] = value
}

// Get 從上下文獲取資料
func (c *Context) Get(key string) (value interface{}, exists bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, exists = c.Keys[key]
	return
}

// MustGet 必須獲取資料，不存在則 panic
func (c *Context) MustGet(key string) interface{} {
	if value, exists := c.Get(key); exists {
		return value
	}
	panic(fmt.Sprintf("Key \"%s\" does not exist", key))
}

// GetString 獲取字串值
func (c *Context) GetString(key string) (s string) {
	if val, ok := c.Get(key); ok && val != nil {
		s, _ = val.(string)
	}
	return
}

// GetBool 獲取布林值
func (c *Context) GetBool(key string) (b bool) {
	if val, ok := c.Get(key); ok && val != nil {
		b, _ = val.(bool)
	}
	return
}

// GetInt 獲取整數值
func (c *Context) GetInt(key string) (i int) {
	if val, ok := c.Get(key); ok && val != nil {
		i, _ = val.(int)
	}
	return
}

// GetInt64 獲取 int64 值
func (c *Context) GetInt64(key string) (i64 int64) {
	if val, ok := c.Get(key); ok && val != nil {
		i64, _ = val.(int64)
	}
	return
}

// GetUint 獲取無符號整數值
func (c *Context) GetUint(key string) (ui uint) {
	if val, ok := c.Get(key); ok && val != nil {
		ui, _ = val.(uint)
	}
	return
}

// GetUint64 獲取 uint64 值
func (c *Context) GetUint64(key string) (ui64 uint64) {
	if val, ok := c.Get(key); ok && val != nil {
		ui64, _ = val.(uint64)
	}
	return
}

// GetFloat64 獲取浮點數值
func (c *Context) GetFloat64(key string) (f64 float64) {
	if val, ok := c.Get(key); ok && val != nil {
		f64, _ = val.(float64)
	}
	return
}

// GetTime 獲取時間值
func (c *Context) GetTime(key string) (t time.Time) {
	if val, ok := c.Get(key); ok && val != nil {
		t, _ = val.(time.Time)
	}
	return
}

// GetDuration 獲取時間間隔
func (c *Context) GetDuration(key string) (d time.Duration) {
	if val, ok := c.Get(key); ok && val != nil {
		d, _ = val.(time.Duration)
	}
	return
}

// GetStringSlice 獲取字串切片
func (c *Context) GetStringSlice(key string) (ss []string) {
	if val, ok := c.Get(key); ok && val != nil {
		ss, _ = val.([]string)
	}
	return
}

// GetStringMap 獲取字串 map
func (c *Context) GetStringMap(key string) (sm map[string]interface{}) {
	if val, ok := c.Get(key); ok && val != nil {
		sm, _ = val.(map[string]interface{})
	}
	return
}

// GetStringMapString 獲取字串到字串的 map
func (c *Context) GetStringMapString(key string) (sms map[string]string) {
	if val, ok := c.Get(key); ok && val != nil {
		sms, _ = val.(map[string]string)
	}
	return
}

// GetStringMapStringSlice 獲取字串到字串切片的 map
func (c *Context) GetStringMapStringSlice(key string) (smss map[string][]string) {
	if val, ok := c.Get(key); ok && val != nil {
		smss, _ = val.(map[string][]string)
	}
	return
}

// ===== Session 相關（簡化版）=====

// GetSession 獲取 Session（需要 session 中間件）
func (c *Context) GetSession(key string) interface{} {
	if session, exists := c.Get("session"); exists {
		if s, ok := session.(map[string]interface{}); ok {
			return s[key]
		}
	}
	return nil
}

// SetSession 設置 Session（需要 session 中間件）
func (c *Context) SetSession(key string, value interface{}) {
	session, exists := c.Get("session")
	if !exists {
		session = make(map[string]interface{})
		c.Set("session", session)
	}
	if s, ok := session.(map[string]interface{}); ok {
		s[key] = value
	}
}

// DeleteSession 刪除 Session 項目
func (c *Context) DeleteSession(key string) {
	if session, exists := c.Get("session"); exists {
		if s, ok := session.(map[string]interface{}); ok {
			delete(s, key)
		}
	}
}

// ClearSession 清空 Session
func (c *Context) ClearSession() {
	c.Set("session", make(map[string]interface{}))
}

// ===== 認證相關 =====

// SetUser 設置當前用戶
func (c *Context) SetUser(user interface{}) {
	c.Set("user", user)
}

// GetUser 獲取當前用戶
func (c *Context) GetUser() interface{} {
	user, _ := c.Get("user")
	return user
}

// SetUserID 設置用戶 ID
func (c *Context) SetUserID(userID interface{}) {
	c.Set("user_id", userID)
}

// GetUserID 獲取用戶 ID
func (c *Context) GetUserID() interface{} {
	userID, _ := c.Get("user_id")
	return userID
}

// SetAuth 設置認證信息
func (c *Context) SetAuth(auth interface{}) {
	c.Set("auth", auth)
}

// GetAuth 獲取認證信息
func (c *Context) GetAuth() interface{} {
	auth, _ := c.Get("auth")
	return auth
}

// IsAuthenticated 檢查是否已認證
func (c *Context) IsAuthenticated() bool {
	_, exists := c.Get("user")
	return exists
}

// ===== 語言和國際化 =====

// SetLang 設置語言
func (c *Context) SetLang(lang string) {
	c.Set("lang", lang)
}

// GetLang 獲取語言
func (c *Context) GetLang() string {
	return c.GetString("lang")
}

// SetLocale 設置地區
func (c *Context) SetLocale(locale string) {
	c.Set("locale", locale)
}

// GetLocale 獲取地區
func (c *Context) GetLocale() string {
	return c.GetString("locale")
}

// ===== 臨時數據（Flash）=====

// SetFlash 設置臨時數據
func (c *Context) SetFlash(key string, value interface{}) {
	flash, exists := c.Get("flash")
	if !exists {
		flash = make(map[string]interface{})
		c.Set("flash", flash)
	}
	if f, ok := flash.(map[string]interface{}); ok {
		f[key] = value
	}
}

// GetFlash 獲取並刪除臨時數據
func (c *Context) GetFlash(key string) interface{} {
	if flash, exists := c.Get("flash"); exists {
		if f, ok := flash.(map[string]interface{}); ok {
			value := f[key]
			delete(f, key)
			return value
		}
	}
	return nil
}

// PeekFlash 查看臨時數據（不刪除）
func (c *Context) PeekFlash(key string) interface{} {
	if flash, exists := c.Get("flash"); exists {
		if f, ok := flash.(map[string]interface{}); ok {
			return f[key]
		}
	}
	return nil
}
