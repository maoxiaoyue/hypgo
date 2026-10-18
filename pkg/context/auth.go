// @chris
package context

// ===== 認證相關方法 =====

// HasRole 檢查是否有特定角色
func (c *Context) HasRole(role string) bool {
	if roles, exists := c.Get("roles"); exists {
		switch v := roles.(type) {
		case []string:
			for _, r := range v {
				if r == role {
					return true
				}
			}
		case string:
			return v == role
		}
	}
	return false
}

// SetRoles 設置用戶角色
func (c *Context) SetRoles(roles []string) {
	c.Set("roles", roles)
}

// GetRoles 獲取用戶角色
func (c *Context) GetRoles() []string {
	if roles, exists := c.Get("roles"); exists {
		if r, ok := roles.([]string); ok {
			return r
		}
	}
	return nil
}
