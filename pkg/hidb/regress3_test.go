package hidb

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"database/sql"

	"github.com/maoxiaoyue/hypgo/pkg/resource"
	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun"
)

// fakeRedis 最小的 RESP 伺服器：記下收到的命令，回 +OK / +QUEUED / EXEC 陣列
type fakeRedis struct {
	mu   sync.Mutex
	cmds []string
}

func (f *fakeRedis) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	queued := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		n, _ := strconv.Atoi(strings.TrimSpace(line[1:]))
		args := make([]string, 0, n)
		for i := 0; i < n; i++ {
			l, _ := r.ReadString('\n')
			sz, _ := strconv.Atoi(strings.TrimSpace(l[1:]))
			buf := make([]byte, sz+2)
			io.ReadFull(r, buf)
			args = append(args, string(buf[:sz]))
		}
		name := strings.ToLower(args[0])
		f.mu.Lock()
		f.cmds = append(f.cmds, strings.Join(args, " "))
		f.mu.Unlock()
		switch {
		case name == "hello":
			fmt.Fprint(c, "-ERR unknown command 'hello'\r\n")
		case name == "multi":
			queued = 0
			fmt.Fprint(c, "+OK\r\n")
		case name == "exec":
			fmt.Fprintf(c, "*%d\r\n", queued)
			for i := 0; i < queued; i++ {
				fmt.Fprint(c, "+OK\r\n")
			}
			queued = -1
		case queued >= 0:
			queued++
			fmt.Fprint(c, "+QUEUED\r\n")
		default:
			fmt.Fprint(c, "+OK\r\n")
		}
	}
}

func (f *fakeRedis) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.cmds
	f.cmds = nil
	return out
}

// TestRedisCountsOnlyApplicationCommands：連線初始化與 MULTI / EXEC 不計數。
// 先前新連線上的第一個 SET 計成 6 次（HELLO、AUTH、SELECT、兩個 CLIENT SETINFO），
// TxPipelined 內 2 個命令計成 4 次
func TestRedisCountsOnlyApplicationCommands(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	f := &fakeRedis{}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	client := redis.NewClient(&redis.Options{Addr: l.Addr().String(), Password: "pw", DB: 2})
	client.AddHook(redisResourceHook{})
	defer client.Close()
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		run      func()
		ops, key int64
	}{
		{"first SET on a new connection", func() { client.Set(ctx, "a", 1, 0) }, 1, 1},
		{"Pipelined 2 SET", func() {
			client.Pipelined(ctx, func(p redis.Pipeliner) error { p.Set(ctx, "a", 1, 0); p.Set(ctx, "b", 1, 0); return nil })
		}, 2, 2},
		{"TxPipelined 2 SET", func() {
			client.TxPipelined(ctx, func(p redis.Pipeliner) error { p.Set(ctx, "a", 1, 0); p.Set(ctx, "b", 1, 0); return nil })
		}, 2, 2},
	} {
		base := resource.Snapshot()
		tc.run()
		d := resource.Snapshot().Sub(base)
		seen := f.take()
		if d.Redis != tc.ops || d.RedisKeys != tc.key {
			t.Errorf("%s: counted ops=%d keys=%d, want %d and %d (server saw %q)", tc.name, d.Redis, d.RedisKeys, tc.ops, tc.key, seen)
		}
	}
}

// TestRedisCmdKeyCount：多 key 命令的 key 數。先前 EVAL / BLPOP / SINTER / RENAME 一律算 1
func TestRedisCmdKeyCount(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		args []interface{}
		want int
	}{
		{[]interface{}{"set", "a", 1}, 1},
		{[]interface{}{"del", "a", "b", "c"}, 3},
		{[]interface{}{"mset", "a", 1, "b", 2}, 2},
		{[]interface{}{"eval", "return 1", 3, "k1", "k2", "k3", "arg"}, 3},
		{[]interface{}{"evalsha", "abc", 0}, 0},
		{[]interface{}{"eval", "return 1", "x"}, 0},
		{[]interface{}{"eval", "return 1", 9, "k1"}, 0},
		{[]interface{}{"blpop", "q1", "q2", 0}, 2},
		{[]interface{}{"sinter", "s1", "s2", "s3"}, 3},
		{[]interface{}{"rename", "a", "b"}, 2},
		{[]interface{}{"bitop", "and", "dst", "a", "b"}, 3},
		{[]interface{}{"zunionstore", "dst", 2, "z1", "z2"}, 3},
		{[]interface{}{"publish", "chan", "m"}, 0},
		{[]interface{}{"ping"}, 0},
	} {
		if got := redisCmdKeyCount(redis.NewCmd(ctx, tc.args...)); got != tc.want {
			t.Errorf("%v: keys = %d, want %d", tc.args, got, tc.want)
		}
	}
}

// TestTransactionControlIsNotCounted：BEGIN / COMMIT 不計入查詢數，也不算寫入。
// 先前 bun 交易裡 2 個 SELECT 計成 4 次，唯讀交易結束後該請求之後的讀取改走主庫
func TestTransactionControlIsNotCounted(t *testing.T) {
	db, _ := openNodes(t, []string{"replica1"})
	ctx := context.Background()
	delta := func(fn func()) int64 {
		base := resource.Snapshot()
		fn()
		return resource.Snapshot().Sub(base).DB
	}
	sess := &session{key: func() string { return "u" }}
	ctx = context.WithValue(ctx, sessionKey{}, sess)
	if n := delta(func() {
		db.HypDBTransaction(ctx, func(ctx context.Context, tx bun.Tx) error {
			var u []r2User
			tx.NewSelect().Model(&u).Scan(ctx)
			tx.NewSelect().Model(&u).Scan(ctx)
			return nil
		})
	}); n != 2 {
		t.Errorf("bun transaction with 2 SELECTs counted %d, want 2", n)
	}
	if sess.wrote.Load() {
		t.Error("a read-only transaction marked the request as having written")
	}
	// 原始交易整筆計一次（裡面的語句不經過 bun）
	if n := delta(func() {
		db.Transaction(ctx, func(tx *sql.Tx) error {
			tx.ExecContext(ctx, "UPDATE a SET x=1")
			tx.ExecContext(ctx, "UPDATE a SET x=2")
			return nil
		})
	}); n != 1 {
		t.Errorf("raw transaction counted %d, want 1", n)
	}
}
