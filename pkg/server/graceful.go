// @chris
package server

// ListenAndServeWithGracefulShutdown 啟動伺服器並支援優雅關閉。
//
// v0.8.11 起 Start() 已內建預設中間件與 SIGINT/SIGTERM 優雅關閉，
// 本方法保留為相容包裝，行為與 Start() 完全相同。
func (s *Server) ListenAndServeWithGracefulShutdown() error {
	return s.Start()
}
