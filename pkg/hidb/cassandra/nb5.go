package cassandra

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// NB5Exec wraps the `nb5` (NoSQLBench v5) binary to drive workload-based
// benchmarks and load tests against a Cassandra / CQL-compatible cluster.
//
// nb5 is an external tool from DataStax — install it separately and make it
// available on PATH (or set Binary to a full path). This wrapper is a safe
// argv-vector launcher (exec.CommandContext, no shell) and a set of fluent
// builders for the most common CLI shapes.
type NB5Exec struct {
	// Binary is the path to nb5. Defaults to "nb5" on PATH.
	Binary string
	// Host is a contact point for the CQL driver (--host).
	Host string
	// Port is the CQL port (--port). Zero = driver default (9042).
	Port int
	// LocalDC forwards to --localdc (required by modern driver policies).
	LocalDC string
	// Username / Password forward to --username / --password.
	Username string
	Password string
	// Driver selects the gocql driver family ("cqld4" is the default in nb5).
	Driver string
	// WorkingDir is the cwd for the process (useful when loading local YAMLs).
	WorkingDir string
	// ExtraArgs are appended to every invocation after connection flags.
	ExtraArgs []string
	// Env supplements os.Environ() for the child process (KEY=VALUE).
	Env []string
}

// NewNB5 returns an NB5Exec with only the binary set.
func NewNB5(binary string) *NB5Exec {
	if binary == "" {
		binary = "nb5"
	}
	return &NB5Exec{Binary: binary}
}

// connFlags builds the standard CQL connection flags nb5 understands as
// top-level driver params. They can also be passed per-activity, but setting
// them at the top level makes them apply to every activity in the scenario.
func (n *NB5Exec) connFlags() []string {
	var args []string
	if n.Driver != "" {
		args = append(args, "driver="+n.Driver)
	}
	if n.Host != "" {
		args = append(args, "host="+n.Host)
	}
	if n.Port > 0 {
		args = append(args, "port="+strconv.Itoa(n.Port))
	}
	if n.LocalDC != "" {
		args = append(args, "localdc="+n.LocalDC)
	}
	if n.Username != "" {
		args = append(args, "username="+n.Username)
	}
	if n.Password != "" {
		args = append(args, "password="+n.Password)
	}
	return args
}

// Run executes nb5 with arbitrary args and returns stdout. Stderr is folded
// into the error when the exit code is non-zero.
func (n *NB5Exec) Run(ctx context.Context, args ...string) (string, error) {
	if n.Binary == "" {
		n.Binary = "nb5"
	}
	if len(args) == 0 {
		return "", fmt.Errorf("nb5: at least one argument is required")
	}
	cmd := exec.CommandContext(ctx, n.Binary, args...)
	if n.WorkingDir != "" {
		cmd.Dir = n.WorkingDir
	}
	if len(n.Env) > 0 {
		cmd.Env = append(os.Environ(), n.Env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("nb5: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// RunActivity runs a single activity: `nb5 <driver/workload> key=val key=val ...`.
// activity may be a workload YAML path, a bundled workload name (e.g. "cql-iot"),
// or a driver short-name. Params are merged after global connection flags.
func (n *NB5Exec) RunActivity(ctx context.Context, activity string, params map[string]string) (string, error) {
	if activity == "" {
		return "", fmt.Errorf("nb5: activity is required")
	}
	args := []string{activity}
	args = append(args, n.connFlags()...)

	// deterministic order for reproducibility
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, k+"="+params[k])
	}
	args = append(args, n.ExtraArgs...)
	return n.Run(ctx, args...)
}

// Version returns `nb5 --version` output.
func (n *NB5Exec) Version(ctx context.Context) (string, error) { return n.Run(ctx, "--version") }

// ListWorkloads runs `nb5 --list-workloads`.
func (n *NB5Exec) ListWorkloads(ctx context.Context) (string, error) {
	return n.Run(ctx, "--list-workloads")
}

// ListDrivers runs `nb5 --list-drivers`.
func (n *NB5Exec) ListDrivers(ctx context.Context) (string, error) {
	return n.Run(ctx, "--list-drivers")
}

// ===== Phase helpers =====

// Phase identifies a standard NoSQLBench workflow phase.
type Phase string

const (
	PhaseSchema Phase = "schema" // run schema block (create keyspace / tables)
	PhaseRampup Phase = "rampup" // deterministic-key warmup write
	PhaseMain   Phase = "main"   // steady-state mixed workload
	PhaseRead   Phase = "read"   // read-only phase
	PhaseWrite  Phase = "write"  // write-only phase
)

// RunPhase runs a specific workload phase with the given cycles/threads.
// Example: nb5 cql-iot default.schema host=... (phase=schema → tags='block: schema')
// cycles may be "" (use workload default), a count like "1M", or a range "0..100k".
func (n *NB5Exec) RunPhase(ctx context.Context, workload string, phase Phase, cycles string, threads int, extra map[string]string) (string, error) {
	if workload == "" {
		return "", fmt.Errorf("nb5: workload is required")
	}
	if phase == "" {
		return "", fmt.Errorf("nb5: phase is required")
	}
	params := map[string]string{"tags": "block:" + string(phase)}
	if cycles != "" {
		params["cycles"] = cycles
	}
	if threads > 0 {
		params["threads"] = strconv.Itoa(threads)
	}
	for k, v := range extra {
		params[k] = v
	}
	return n.RunActivity(ctx, workload, params)
}

// ===== Scenario Builder =====

// NB5Scenario is a fluent builder that composes one nb5 invocation with
// connection flags, an activity target, and arbitrary params.
type NB5Scenario struct {
	exec     *NB5Exec
	activity string
	params   map[string]string
	tags     []string
	cycles   string
	threads  int
	rampup   string
	cyclerate string
	errors   string
	alias    string
	main     bool
	extras   []string // free-form "--flag" passthrough
}

// Scenario returns a new scenario builder bound to this exec wrapper.
func (n *NB5Exec) Scenario(activity string) *NB5Scenario {
	return &NB5Scenario{exec: n, activity: activity, params: map[string]string{}}
}

// Param sets a generic key=value activity parameter.
func (s *NB5Scenario) Param(k, v string) *NB5Scenario { s.params[k] = v; return s }

// Tag adds a tag filter (e.g. "block:main"). Multiple tags are joined with spaces.
func (s *NB5Scenario) Tag(t string) *NB5Scenario { s.tags = append(s.tags, t); return s }

// Phase is shorthand for Tag("block:<phase>").
func (s *NB5Scenario) Phase(p Phase) *NB5Scenario { return s.Tag("block:" + string(p)) }

// Cycles sets the cycles range ("1M", "0..100000", etc.).
func (s *NB5Scenario) Cycles(v string) *NB5Scenario { s.cycles = v; return s }

// Threads sets the thread count ("auto" is not supported here — use Param).
func (s *NB5Scenario) Threads(n int) *NB5Scenario { s.threads = n; return s }

// Rampup sets the rampup cycles range (usually matches cycles for write-phase).
func (s *NB5Scenario) Rampup(v string) *NB5Scenario { s.rampup = v; return s }

// CycleRate sets a fixed op-rate target (ops/sec). Example: "10000".
func (s *NB5Scenario) CycleRate(v string) *NB5Scenario { s.cyclerate = v; return s }

// Errors sets the errors modifier (count|warn|stop|retry|ignore).
func (s *NB5Scenario) Errors(v string) *NB5Scenario { s.errors = v; return s }

// Alias sets the activity alias (useful when running multiple concurrent activities).
func (s *NB5Scenario) Alias(v string) *NB5Scenario { s.alias = v; return s }

// Main flags this invocation as a `main` phase (tag=block:main) shortcut.
func (s *NB5Scenario) Main() *NB5Scenario { s.main = true; return s }

// Extra appends a raw CLI flag (e.g. "--report-summary-to", "stdout:60s").
func (s *NB5Scenario) Extra(args ...string) *NB5Scenario { s.extras = append(s.extras, args...); return s }

// Args assembles the final argv (excluding the binary).
func (s *NB5Scenario) Args() []string {
	args := []string{s.activity}
	args = append(args, s.exec.connFlags()...)

	if s.alias != "" {
		args = append(args, "alias="+s.alias)
	}
	if s.main && !hasBlockTag(s.tags) {
		s.tags = append(s.tags, "block:main")
	}
	if len(s.tags) > 0 {
		args = append(args, "tags="+strings.Join(s.tags, " "))
	}
	if s.cycles != "" {
		args = append(args, "cycles="+s.cycles)
	}
	if s.rampup != "" {
		args = append(args, "rampup-cycles="+s.rampup)
	}
	if s.cyclerate != "" {
		args = append(args, "cyclerate="+s.cyclerate)
	}
	if s.threads > 0 {
		args = append(args, "threads="+strconv.Itoa(s.threads))
	}
	if s.errors != "" {
		args = append(args, "errors="+s.errors)
	}
	// deterministic param ordering
	keys := make([]string, 0, len(s.params))
	for k := range s.params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, k+"="+s.params[k])
	}
	args = append(args, s.extras...)
	args = append(args, s.exec.ExtraArgs...)
	return args
}

// CommandLine returns the fully quoted CLI string (for logging / diagnostics).
func (s *NB5Scenario) CommandLine() string {
	parts := append([]string{s.exec.Binary}, s.Args()...)
	return strings.Join(parts, " ")
}

// Run executes the scenario.
func (s *NB5Scenario) Run(ctx context.Context) (string, error) {
	return s.exec.Run(ctx, s.Args()...)
}

func hasBlockTag(tags []string) bool {
	for _, t := range tags {
		if strings.HasPrefix(t, "block:") {
			return true
		}
	}
	return false
}

// ===== Result parsing =====

// NB5Summary is a lightweight parser for the stdout tail that nb5 emits at
// scenario completion. Only the headline metrics are extracted; for full
// metrics use nb5's built-in HDR / Prometheus / log exporters.
type NB5Summary struct {
	TotalCycles int64
	Duration    time.Duration
	OpsPerSec   float64
	Errors      int64
	Raw         string
}

// ParseSummary scans nb5 stdout for the common "cycles=... duration=... rate=..."
// summary line. Missing fields remain zero.
func ParseSummary(stdout string) NB5Summary {
	s := NB5Summary{Raw: stdout}
	for _, line := range strings.Split(stdout, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if v := extractKV(l, "cycles="); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				s.TotalCycles = n
			}
		}
		if v := extractKV(l, "errors="); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				s.Errors = n
			}
		}
		if v := extractKV(l, "rate="); v != "" {
			// rate values in nb5 are typically "X ops/s"
			trim := strings.TrimSuffix(strings.TrimSuffix(v, "ops/s"), " ")
			if f, err := strconv.ParseFloat(strings.TrimSpace(trim), 64); err == nil {
				s.OpsPerSec = f
			}
		}
		if v := extractKV(l, "duration="); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				s.Duration = d
			}
		}
	}
	return s
}

func extractKV(line, key string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(key):]
	// value ends at first whitespace or comma
	end := len(rest)
	for i, r := range rest {
		if r == ' ' || r == '\t' || r == ',' {
			end = i
			break
		}
	}
	return rest[:end]
}

// ===== Workload YAML helper =====

// WriteInlineWorkload writes a minimal NoSQLBench v5 workload YAML to the
// given path, for ad-hoc tests. The workload defines a single keyspace /
// table and the usual schema / rampup / main blocks. Returns the absolute
// path written. Callers typically use the returned path as the activity
// argument to RunActivity / Scenario.
func WriteInlineWorkload(path, keyspace, table string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("nb5: workload path is required")
	}
	if keyspace == "" || table == "" {
		return "", fmt.Errorf("nb5: keyspace and table are required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	yaml := fmt.Sprintf(inlineWorkloadTmpl,
		keyspace,                    // CREATE KEYSPACE
		keyspace, table,             // CREATE TABLE
		keyspace, table,             // rampup INSERT
		keyspace, table,             // main SELECT
		keyspace, table,             // main INSERT
	)
	if err := os.WriteFile(abs, []byte(yaml), 0o644); err != nil {
		return "", err
	}
	return abs, nil
}

const inlineWorkloadTmpl = `# Auto-generated by hidb.cassandra.NB5Exec.WriteInlineWorkload
description: Basic CQL key-value workload (hypgo inline)

scenarios:
  default:
    schema: run driver=cqld4 tags==block:schema threads===1 cycles===UNDEF
    rampup: run driver=cqld4 tags==block:rampup cycles===TEMPLATE(rampup-cycles,100000) threads=auto
    main:   run driver=cqld4 tags==block:main   cycles===TEMPLATE(main-cycles,100000)   threads=auto

bindings:
  seq_key:  Mod(TEMPLATE(keycount,1000000)); ToString() -> String
  rw_key:   Uniform(0,TEMPLATE(keycount,1000000)); ToString() -> String
  payload:  HashedLineToString(50); ToString() -> String

blocks:
  schema:
    params:
      prepared: false
    ops:
      create-keyspace: |
        CREATE KEYSPACE IF NOT EXISTS %s
        WITH replication = {'class':'SimpleStrategy','replication_factor':1};
      create-table: |
        CREATE TABLE IF NOT EXISTS %s.%s (
          key   text PRIMARY KEY,
          value text
        );

  rampup:
    ops:
      insert: |
        INSERT INTO %s.%s (key, value) VALUES ({seq_key}, {payload});

  main:
    params:
      ratio: 5
    ops:
      select: |
        SELECT * FROM %s.%s WHERE key = {rw_key};
      insert: |
        INSERT INTO %s.%s (key, value) VALUES ({rw_key}, {payload});
`
