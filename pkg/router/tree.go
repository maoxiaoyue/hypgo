package router

import (
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// nodeType 節點類型
type nodeType uint8

// Radix Tree 節點結構與常量
const (
	static   nodeType = iota // 靜態節點
	root                     // 根節點
	param                    // 參數節點 :id
	catchAll                 // 捕獲所有 *filepath
)

// radixNode Radix Tree 節點
type radixNode struct {
	path      string                   // 該節點對應的路徑段
	indices   string                   // 子節點的第一個字元索引（用於快速查找）
	wildChild bool                     // 是否有通配符子節點
	nType     nodeType                 // 節點類型
	priority  uint32                   // 優先級（命中次數，用於子節點排序）
	children  []*radixNode             // 子節點列表
	handlers  []hypcontext.HandlerFunc // 處理器鏈
	fullPath  string                   // 完整路徑（用於衝突提示）
}

// search 在 Radix Tree 中搜索匹配的路由
// 返回匹配的 handlers 和提取的路徑參數
func (n *radixNode) search(path string, params []Param) ([]hypcontext.HandlerFunc, []Param) {
	p := params

walk:
	for {
		if len(path) > len(n.path) {
			if path[:len(n.path)] == n.path {
				path = path[len(n.path):]

				// 非通配符子節點 → 用 indices 快速查找
				if !n.wildChild {
					c := path[0]
					for i, index := range []byte(n.indices) {
						if c == index {
							n = n.children[i]
							continue walk
						}
					}
					// 沒找到匹配的子節點
					return nil, p
				}

				// 處理通配符子節點
				n = n.children[0]
				switch n.nType {
				case param:
					// 提取 :param 值（到下一個 / 為止）
					end := 0
					for end < len(path) && path[end] != '/' {
						end++
					}

					if p == nil {
						p = make([]Param, 0, 4)
					}
					p = append(p, Param{
						Key:   n.path[1:], // 去掉前導 ':'
						Value: path[:end],
					})

					if end < len(path) {
						// 還有剩餘路徑，繼續向下匹配
						if len(n.children) > 0 {
							path = path[end:]
							n = n.children[0]
							continue walk
						}
						return nil, p
					}

					return n.handlers, p

				case catchAll:
					// 捕獲所有剩餘路徑
					if p == nil {
						p = make([]Param, 0, 4)
					}
					p = append(p, Param{
						Key:   n.path[2:], // 去掉前導 '*' 和 '/'
						Value: path,
					})
					return n.handlers, p
				}
			}
		} else if path == n.path {
			// 完全匹配
			return n.handlers, p
		}

		// 不匹配
		return nil, p
	}
}

// addRoute 添加路由到樹
func (n *radixNode) addRoute(path string, handlers []hypcontext.HandlerFunc) {
	fullPath := path
	n.priority++

	// 空樹：直接插入
	if len(n.path) == 0 && len(n.children) == 0 {
		n.insertChild(path, fullPath, handlers)
		n.nType = root
		return
	}

	// 查找最長公共前綴
	i := longestCommonPrefix(path, n.path)

	// 當前節點需要分割
	if i < len(n.path) {
		child := &radixNode{
			path:      n.path[i:],
			wildChild: n.wildChild,
			nType:     static,
			indices:   n.indices,
			children:  n.children,
			handlers:  n.handlers,
			priority:  n.priority - 1,
		}

		n.children = []*radixNode{child}
		n.indices = string(n.path[i])
		n.path = path[:i]
		n.handlers = nil
		n.wildChild = false
	}

	// 還有剩餘路徑需要插入
	if i < len(path) {
		path = path[i:]

		// 當前節點有通配符子節點
		if n.wildChild {
			n = n.children[0]
			n.priority++

			// 檢查通配符相容性
			if len(path) >= len(n.path) && n.path == path[:len(n.path)] &&
				n.nType != catchAll &&
				(len(n.path) >= len(path) || path[len(n.path)] == '/') {
				n.addRoute(path, handlers)
			} else {
				panic("router: path segment '" + path +
					"' conflicts with existing wildcard '" + n.path +
					"' in path '" + fullPath + "'")
			}
			return
		}

		c := path[0]

		// 參數節點後接 '/' → 進入子節點繼續
		if n.nType == param && c == '/' && len(n.children) == 1 {
			n = n.children[0]
			n.priority++
			n.addRoute(path, handlers)
			return
		}

		// 檢查是否有匹配的子節點
		for i, index := range []byte(n.indices) {
			if c == index {
				i = n.incrementChildPrio(i)
				n = n.children[i]
				n.addRoute(path, handlers)
				return
			}
		}

		// 插入新的靜態子節點
		if c != ':' && c != '*' {
			n.indices += string(c)
			child := &radixNode{fullPath: fullPath}
			n.addChild(child)
			n.incrementChildPrio(len(n.indices) - 1)
			n = child
		}

		n.insertChild(path, fullPath, handlers)
		return
	}

	// 路徑完全匹配到當前節點
	if n.handlers != nil {
		panic("router: handlers already registered for path '" + fullPath + "'")
	}
	n.handlers = handlers
}

// insertChild 插入含通配符的子節點，處理通配符插入
func (n *radixNode) insertChild(path, fullPath string, handlers []hypcontext.HandlerFunc) {
	for {
		// 查找路徑中的通配符
		wildcard, i, valid := findWildcard(path)
		if i < 0 {
			break // 無通配符，跳出
		}

		if !valid {
			panic("router: only one wildcard per path segment is allowed" +
				", has: '" + wildcard + "' in path '" + fullPath + "'")
		}

		if len(wildcard) < 2 {
			panic("router: wildcards must be named with a non-empty name" +
				" in path '" + fullPath + "'")
		}

		if wildcard[0] == ':' {
			// ===== 參數節點 :param =====
			if i > 0 {
				// 通配符前有靜態前綴
				n.path = path[:i]
				path = path[i:]
			}

			child := &radixNode{
				nType:    param,
				path:     wildcard,
				fullPath: fullPath,
			}
			n.children = []*radixNode{child}
			n.wildChild = true
			n = child
			n.priority++

			if len(wildcard) < len(path) {
				// 通配符後還有路徑
				path = path[len(wildcard):]
				child := &radixNode{
					priority: 1,
					fullPath: fullPath,
				}
				n.children = []*radixNode{child}
				n = child
				continue
			}

			// 通配符就是路徑末端
			n.handlers = handlers
			return

		} else {
			// ===== 捕獲所有 *filepath =====
			if i+len(wildcard) != len(path) {
				panic("router: catch-all routes are only allowed at the end" +
					" of the path in path '" + fullPath + "'")
			}

			if i > 0 {
				n.path = path[:i]
			}

			// 創建 catchAll 中間節點
			child := &radixNode{
				wildChild: true,
				nType:     catchAll,
				fullPath:  fullPath,
			}
			n.children = []*radixNode{child}
			n.indices = string('/')
			n = child
			n.priority++

			// 創建 catchAll 葉節點
			child = &radixNode{
				path:     path[i:],
				nType:    catchAll,
				handlers: handlers,
				priority: 1,
				fullPath: fullPath,
			}
			n.children = []*radixNode{child}
			return
		}
	}

	// 無通配符 → 純靜態路徑
	n.path = path
	n.handlers = handlers
	n.fullPath = fullPath
}

// addChild 安全地添加子節點，節點輔助方法
// 如果已有通配符子節點，新節點插在通配符前面
func (n *radixNode) addChild(child *radixNode) {
	if n.wildChild && len(n.children) > 0 {
		// 通配符子節點始終在最後
		wildcardChild := n.children[len(n.children)-1]
		n.children = append(n.children[:len(n.children)-1], child, wildcardChild)
	} else {
		n.children = append(n.children, child)
	}
}

// incrementChildPrio 調整子節點優先級排序
// 被訪問越多的子節點排越前面，加速常用路由的匹配
func (n *radixNode) incrementChildPrio(pos int) int {
	cs := n.children
	cs[pos].priority++
	prio := cs[pos].priority

	// 向前冒泡排序
	newPos := pos
	for ; newPos > 0 && cs[newPos-1].priority < prio; newPos-- {
		cs[newPos-1], cs[newPos] = cs[newPos], cs[newPos-1]
	}

	// 同步更新 indices
	if newPos != pos {
		n.indices = n.indices[:newPos] +
			n.indices[pos:pos+1] +
			n.indices[newPos:pos] +
			n.indices[pos+1:]
	}

	return newPos
}
