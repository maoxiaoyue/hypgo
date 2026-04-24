package cassandra

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// NodetoolExec wraps the `nodetool` binary for action commands that cannot be
// expressed via CQL (flush / compact / repair / drain / cleanup / snapshot…).
//
// Connection (JMX) and auth are passed as explicit flags rather than inherited
// from the CassandraDB CQL config because JMX usually runs on a different port
// and often with different credentials.
type NodetoolExec struct {
	// Binary is the path to nodetool; defaults to "nodetool" on PATH.
	Binary string
	// Host is the JMX host (-h). Empty means localhost.
	Host string
	// Port is the JMX port (-p). Zero means default (7199).
	Port int
	// Username / Password forward to -u / -pw.
	Username string
	Password string
	// PasswordFile forwards to -pwf.
	PasswordFile string
	// ExtraArgs are appended after the connection flags, before the subcommand.
	ExtraArgs []string
}

// NewNodetool returns an exec wrapper with only the binary set.
func NewNodetool(binary string) *NodetoolExec {
	if binary == "" {
		binary = "nodetool"
	}
	return &NodetoolExec{Binary: binary}
}

// connFlags builds the JMX connection flags.
func (n *NodetoolExec) connFlags() []string {
	var args []string
	if n.Host != "" {
		args = append(args, "-h", n.Host)
	}
	if n.Port > 0 {
		args = append(args, "-p", strconv.Itoa(n.Port))
	}
	if n.Username != "" {
		args = append(args, "-u", n.Username)
	}
	if n.Password != "" {
		args = append(args, "-pw", n.Password)
	}
	if n.PasswordFile != "" {
		args = append(args, "-pwf", n.PasswordFile)
	}
	args = append(args, n.ExtraArgs...)
	return args
}

// Run executes a nodetool subcommand and returns stdout. Stderr is surfaced
// via error when the exit code is non-zero.
func (n *NodetoolExec) Run(ctx context.Context, subcommand string, args ...string) (string, error) {
	if n.Binary == "" {
		n.Binary = "nodetool"
	}
	if subcommand == "" {
		return "", fmt.Errorf("nodetool: subcommand is required")
	}
	full := append(n.connFlags(), subcommand)
	full = append(full, args...)

	cmd := exec.CommandContext(ctx, n.Binary, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("nodetool %s: %s", subcommand, msg)
	}
	return stdout.String(), nil
}

// RunQuiet is like Run but discards stdout. Returns only error.
func (n *NodetoolExec) RunQuiet(ctx context.Context, subcommand string, args ...string) error {
	_, err := n.Run(ctx, subcommand, args...)
	return err
}

// ===== High-level wrappers =====

// Flush flushes memtables to disk. Empty keyspace flushes all; tables empty flushes all tables in the keyspace.
func (n *NodetoolExec) Flush(ctx context.Context, keyspace string, tables ...string) error {
	args := []string{}
	if keyspace != "" {
		args = append(args, keyspace)
		args = append(args, tables...)
	}
	return n.RunQuiet(ctx, "flush", args...)
}

// Compact forces a major compaction. Empty keyspace compacts all.
func (n *NodetoolExec) Compact(ctx context.Context, keyspace string, tables ...string) error {
	args := []string{}
	if keyspace != "" {
		args = append(args, keyspace)
		args = append(args, tables...)
	}
	return n.RunQuiet(ctx, "compact", args...)
}

// RepairOptions controls `nodetool repair` behaviour.
type RepairOptions struct {
	Full            bool     // -full
	Sequential      bool     // -seq
	ParallelUnsafe  bool     // -par
	PrimaryRange    bool     // -pr
	DCs             []string // -dc
	Hosts           []string // -hosts
	TraceRepair     bool     // -tr
	IncrementalOnly bool     // (default in 4.x+; set -full explicitly if you want full)
	Tokens          string   // -st and -et form; pass "start:end" or leave empty
}

// Repair runs `nodetool repair [options] [keyspace [tables...]]`.
func (n *NodetoolExec) Repair(ctx context.Context, keyspace string, tables []string, opts RepairOptions) error {
	args := []string{}
	if opts.Full {
		args = append(args, "-full")
	}
	if opts.Sequential {
		args = append(args, "-seq")
	}
	if opts.ParallelUnsafe {
		args = append(args, "-par")
	}
	if opts.PrimaryRange {
		args = append(args, "-pr")
	}
	if opts.TraceRepair {
		args = append(args, "-tr")
	}
	for _, dc := range opts.DCs {
		args = append(args, "-dc", dc)
	}
	for _, h := range opts.Hosts {
		args = append(args, "-hosts", h)
	}
	if opts.Tokens != "" {
		if i := strings.Index(opts.Tokens, ":"); i > 0 {
			args = append(args, "-st", opts.Tokens[:i], "-et", opts.Tokens[i+1:])
		}
	}
	if keyspace != "" {
		args = append(args, keyspace)
		args = append(args, tables...)
	}
	return n.RunQuiet(ctx, "repair", args...)
}

// Cleanup removes data for tokens no longer owned by this node.
func (n *NodetoolExec) Cleanup(ctx context.Context, keyspace string, tables ...string) error {
	args := []string{}
	if keyspace != "" {
		args = append(args, keyspace)
		args = append(args, tables...)
	}
	return n.RunQuiet(ctx, "cleanup", args...)
}

// Drain stops accepting writes and flushes memtables; used before shutdown.
func (n *NodetoolExec) Drain(ctx context.Context) error {
	return n.RunQuiet(ctx, "drain")
}

// Scrub rebuilds SSTables for a keyspace/tables.
func (n *NodetoolExec) Scrub(ctx context.Context, keyspace string, tables ...string) error {
	args := []string{}
	if keyspace != "" {
		args = append(args, keyspace)
		args = append(args, tables...)
	}
	return n.RunQuiet(ctx, "scrub", args...)
}

// UpgradeSSTables rewrites SSTables to the latest format.
func (n *NodetoolExec) UpgradeSSTables(ctx context.Context, keyspace string, tables ...string) error {
	args := []string{}
	if keyspace != "" {
		args = append(args, keyspace)
		args = append(args, tables...)
	}
	return n.RunQuiet(ctx, "upgradesstables", args...)
}

// Snapshot takes a snapshot. Name empty → nodetool assigns timestamp; tag empty → all keyspaces.
func (n *NodetoolExec) Snapshot(ctx context.Context, name, keyspace string, tables ...string) error {
	args := []string{}
	if name != "" {
		args = append(args, "-t", name)
	}
	if len(tables) > 0 && keyspace != "" {
		args = append(args, "-kt")
		kt := make([]string, 0, len(tables))
		for _, t := range tables {
			kt = append(kt, keyspace+"."+t)
		}
		args = append(args, strings.Join(kt, ","))
	} else if keyspace != "" {
		args = append(args, keyspace)
	}
	return n.RunQuiet(ctx, "snapshot", args...)
}

// ClearSnapshot removes snapshots. Empty name removes all snapshots.
func (n *NodetoolExec) ClearSnapshot(ctx context.Context, name string, keyspaces ...string) error {
	args := []string{}
	if name != "" {
		args = append(args, "-t", name)
	}
	args = append(args, keyspaces...)
	return n.RunQuiet(ctx, "clearsnapshot", args...)
}

// Refresh loads newly placed SSTables into a keyspace/table.
func (n *NodetoolExec) Refresh(ctx context.Context, keyspace, table string) error {
	if keyspace == "" || table == "" {
		return fmt.Errorf("nodetool refresh: keyspace and table are required")
	}
	return n.RunQuiet(ctx, "refresh", keyspace, table)
}

// Decommission triggers graceful removal of the local node from the ring.
func (n *NodetoolExec) Decommission(ctx context.Context, force bool) error {
	args := []string{}
	if force {
		args = append(args, "--force")
	}
	return n.RunQuiet(ctx, "decommission", args...)
}

// Move moves the local node to a new token. Pass the new token as a string.
func (n *NodetoolExec) Move(ctx context.Context, token string) error {
	if token == "" {
		return fmt.Errorf("nodetool move: token is required")
	}
	return n.RunQuiet(ctx, "move", token)
}

// SetCompactionThroughput sets MB/s; 0 means unlimited.
func (n *NodetoolExec) SetCompactionThroughput(ctx context.Context, mbPerSec int) error {
	return n.RunQuiet(ctx, "setcompactionthroughput", strconv.Itoa(mbPerSec))
}

// SetStreamThroughput sets MB/s for streaming; 0 means unlimited.
func (n *NodetoolExec) SetStreamThroughput(ctx context.Context, mbPerSec int) error {
	return n.RunQuiet(ctx, "setstreamthroughput", strconv.Itoa(mbPerSec))
}

// SetLoggingLevel dynamically changes a logger's level.
func (n *NodetoolExec) SetLoggingLevel(ctx context.Context, logger, level string) error {
	if logger == "" || level == "" {
		return fmt.Errorf("nodetool setlogginglevel: logger and level are required")
	}
	return n.RunQuiet(ctx, "setlogginglevel", logger, level)
}

// Info returns the raw output of `nodetool info` — useful for display.
func (n *NodetoolExec) Info(ctx context.Context) (string, error) {
	return n.Run(ctx, "info")
}

// Status returns the raw output of `nodetool status [keyspace]`.
func (n *NodetoolExec) Status(ctx context.Context, keyspace string) (string, error) {
	args := []string{}
	if keyspace != "" {
		args = append(args, keyspace)
	}
	return n.Run(ctx, "status", args...)
}

// Version returns the nodetool version string (useful as a connectivity check).
func (n *NodetoolExec) Version(ctx context.Context) (string, error) {
	return n.Run(ctx, "version")
}
