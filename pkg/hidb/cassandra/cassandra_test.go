package cassandra

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gocql/gocql"
)

func TestKeyspaceSimple(t *testing.T) {
	got := (&CassandraDB{}).Keyspace("analytics").Simple(3).NoDefaults().CreateCQL()
	want := `CREATE KEYSPACE IF NOT EXISTS analytics WITH REPLICATION = {'class': 'SimpleStrategy', 'replication_factor': 3}`
	if got != want {
		t.Fatalf("simple keyspace CQL mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestKeyspaceDefaultsApplied(t *testing.T) {
	got := (&CassandraDB{}).Keyspace("ks").CreateCQL()
	if !strings.Contains(got, "SimpleStrategy") {
		t.Errorf("missing default replication: %s", got)
	}
	if !strings.Contains(got, "DURABLE_WRITES = true") {
		t.Errorf("missing default durable_writes: %s", got)
	}
}

func TestTableDefaultsApplied(t *testing.T) {
	got := (&CassandraDB{}).Table("t").
		Column("id", TypeUUID).
		PartitionKey("id").
		CreateCQL()
	for _, c := range []string{
		"'class': 'UnifiedCompactionStrategy'",
		"'sstable_compression': 'LZ4Compressor'",
		"'keys': 'ALL'",
		"gc_grace_seconds = 864000",
		"speculative_retry = '99p'",
	} {
		if !strings.Contains(got, c) {
			t.Errorf("missing default %q in:\n%s", c, got)
		}
	}
}

func TestTableNoDefaults(t *testing.T) {
	got := (&CassandraDB{}).Table("t").
		Column("id", TypeUUID).
		PartitionKey("id").
		NoDefaults().
		CreateCQL()
	if strings.Contains(got, "WITH") {
		t.Errorf("expected no WITH clause with NoDefaults:\n%s", got)
	}
}

func TestKeyspaceNetworkTopology(t *testing.T) {
	got := (&CassandraDB{}).Keyspace("ks").
		NetworkTopology(map[string]int{"dc1": 3, "dc2": 2}).
		DurableWrites(false).
		CreateCQL()
	if !strings.Contains(got, "'class': 'NetworkTopologyStrategy'") {
		t.Fatalf("missing NetworkTopologyStrategy in %s", got)
	}
	if !strings.Contains(got, "'dc1': 3") || !strings.Contains(got, "'dc2': 2") {
		t.Fatalf("missing dc mappings in %s", got)
	}
	if !strings.Contains(got, "DURABLE_WRITES = false") {
		t.Fatalf("missing durable_writes in %s", got)
	}
}

func TestKeyspaceAlter(t *testing.T) {
	got := (&CassandraDB{}).Keyspace("ks").Simple(5).AlterCQL()
	want := `ALTER KEYSPACE ks WITH REPLICATION = {'class': 'SimpleStrategy', 'replication_factor': 5}`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTableBasic(t *testing.T) {
	got := (&CassandraDB{}).Table("users").
		Column("id", TypeUUID).
		Column("name", TypeText).
		Column("email", TypeText).
		PartitionKey("id").
		CreateCQL()
	wantPrefix := "CREATE TABLE IF NOT EXISTS users ("
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("prefix mismatch:\n%s", got)
	}
	if !strings.Contains(got, "PRIMARY KEY (id)") {
		t.Fatalf("missing primary key:\n%s", got)
	}
}

func TestTableCompositeKeyAndOptions(t *testing.T) {
	cql := (&CassandraDB{}).Table("events").
		Column("tenant_id", TypeUUID).
		Column("bucket", TypeInt).
		Column("ts", TypeTimestamp).
		Column("payload", TypeText).
		PartitionKey("tenant_id", "bucket").
		ClusteringKey("ts").
		ClusteringOrder("ts", Desc).
		WithTTL(604800).
		WithComment("append-only log").
		WithCompaction(CompactionOptions{
			Class: CompactionTWCS,
			Extra: map[string]interface{}{
				"compaction_window_size": 1,
				"compaction_window_unit": "DAYS",
			},
		}).
		WithCompression(CompressionOptions{SSTableCompression: CompressionZstd, ChunkLengthKB: 16}).
		WithCDC(true).
		CreateCQL()
	checks := []string{
		"PRIMARY KEY ((tenant_id, bucket), ts)",
		"CLUSTERING ORDER BY (ts DESC)",
		"default_time_to_live = 604800",
		"comment = 'append-only log'",
		"'class': 'TimeWindowCompactionStrategy'",
		"'compaction_window_size': 1",
		"'compaction_window_unit': 'DAYS'",
		"'sstable_compression': 'ZstdCompressor'",
		"'chunk_length_in_kb': 16",
		"cdc = true",
	}
	for _, c := range checks {
		if !strings.Contains(cql, c) {
			t.Errorf("missing %q in:\n%s", c, cql)
		}
	}
}

func TestAlterTable(t *testing.T) {
	got := (&CassandraDB{}).AlterTable("users").
		AddColumn("bio", TypeText).
		DropColumn("legacy").
		RenameColumn("fullname", "full_name").
		CQL()
	want := "ALTER TABLE users ADD bio text;\nALTER TABLE users DROP legacy;\nALTER TABLE users RENAME fullname TO full_name;"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestIndexSAI(t *testing.T) {
	got := (&CassandraDB{}).Index("users_name_idx").
		On("users", "name").
		SAI().
		Option("case_sensitive", false).
		CreateCQL()
	if !strings.Contains(got, "USING 'sai'") {
		t.Fatalf("missing SAI clause:\n%s", got)
	}
	if !strings.Contains(got, "'case_sensitive': false") {
		t.Fatalf("missing option:\n%s", got)
	}
}

func TestIndexOnCollection(t *testing.T) {
	got := (&CassandraDB{}).Index("tags_idx").
		On("items", "tags").
		Target(IndexTargetKeys).
		CreateCQL()
	if !strings.Contains(got, "KEYS(tags)") {
		t.Fatalf("missing collection target:\n%s", got)
	}
}

func TestMaterializedView(t *testing.T) {
	got := (&CassandraDB{}).MaterializedView("users_by_email").
		FromTable("users").
		Select("*").
		WhereNotNull("email", "id").
		PartitionKey("email").
		ClusteringKey("id").
		CreateCQL()
	for _, c := range []string{
		"CREATE MATERIALIZED VIEW IF NOT EXISTS users_by_email",
		"SELECT *",
		"FROM users",
		"email IS NOT NULL",
		"id IS NOT NULL",
		"PRIMARY KEY (email, id)",
	} {
		if !strings.Contains(got, c) {
			t.Errorf("missing %q in:\n%s", c, got)
		}
	}
}

func TestUDT(t *testing.T) {
	got := (&CassandraDB{}).Type("address").
		Field("street", TypeText).
		Field("zip", TypeText).
		CreateCQL()
	if !strings.Contains(got, "CREATE TYPE IF NOT EXISTS address") {
		t.Fatalf("bad prefix: %s", got)
	}
	if !strings.Contains(got, "street text") || !strings.Contains(got, "zip text") {
		t.Fatalf("missing fields: %s", got)
	}
}

func TestUDFAndUDA(t *testing.T) {
	udf := (&CassandraDB{}).Function("state_add").
		Arg("s", TypeInt).
		Arg("val", TypeInt).
		Returns(TypeInt).
		Language("java").
		Body("return s + val;").
		Deterministic(true).
		CreateCQL()
	for _, c := range []string{"CREATE OR REPLACE FUNCTION", "RETURNS int", "DETERMINISTIC", "LANGUAGE java", "$$return s + val;$$"} {
		if !strings.Contains(udf, c) {
			t.Errorf("udf missing %q:\n%s", c, udf)
		}
	}
	uda := (&CassandraDB{}).Aggregate("my_sum").
		Arg(TypeInt).
		SFunc("state_add").
		StateType(TypeInt).
		InitCond("0").
		CreateCQL()
	for _, c := range []string{"CREATE OR REPLACE AGGREGATE", "SFUNC state_add", "STYPE int", "INITCOND 0"} {
		if !strings.Contains(uda, c) {
			t.Errorf("uda missing %q:\n%s", c, uda)
		}
	}
}

func TestVectorType(t *testing.T) {
	v := VectorFloat(384)
	if string(v) != "vector<float, 384>" {
		t.Fatalf("bad vector type: %s", v)
	}
}

type embedding struct {
	ID        gocql.UUID `cql:"id,pk"`
	CreatedAt time.Time  `cql:"created_at,ck,order=desc"`
	Title     string     `cql:"title"`
	Vector    []float32  `cql:"vector,type=vector<float, 384>"`
}

func (embedding) TableName() string { return "embeddings" }

func TestModelParse(t *testing.T) {
	info, err := ParseModel(&embedding{})
	if err != nil {
		t.Fatal(err)
	}
	if info.Table != "embeddings" {
		t.Fatalf("bad table name: %s", info.Table)
	}
	if len(info.PartitionKey) != 1 || info.PartitionKey[0] != "id" {
		t.Fatalf("bad partition key: %v", info.PartitionKey)
	}
	if len(info.Clustering) != 1 || info.Clustering[0] != "created_at" {
		t.Fatalf("bad clustering: %v", info.Clustering)
	}
	v, ok := info.FieldByColumn("vector")
	if !ok {
		t.Fatal("vector column missing")
	}
	if string(v.Type) != "vector<float, 384>" {
		t.Fatalf("bad vector type in model: %s", v.Type)
	}
}

func TestTableFromModel(t *testing.T) {
	tb, err := (&CassandraDB{}).TableFromModel(&embedding{})
	if err != nil {
		t.Fatal(err)
	}
	cql := tb.CreateCQL()
	for _, c := range []string{
		"CREATE TABLE IF NOT EXISTS embeddings",
		"id uuid",
		"created_at timestamp",
		"vector vector<float, 384>",
		"PRIMARY KEY (id, created_at)",
		"CLUSTERING ORDER BY (created_at DESC)",
	} {
		if !strings.Contains(cql, c) {
			t.Errorf("missing %q:\n%s", c, cql)
		}
	}
}

func TestInsertCQL(t *testing.T) {
	stmt, args := (&CassandraDB{}).Insert("users").
		Value("id", 1).
		Value("name", "alice").
		IfNotExists().
		TTL(3600).
		CQL()
	want := "INSERT INTO users (id, name) VALUES (?, ?) IF NOT EXISTS USING TTL 3600"
	if stmt != want {
		t.Fatalf("got %q want %q", stmt, want)
	}
	if len(args) != 2 {
		t.Fatalf("bad args count: %d", len(args))
	}
}

func TestUpdateCQL(t *testing.T) {
	stmt, args := (&CassandraDB{}).Update("users").
		Set("name", "bob").
		WhereEq("id", 1).
		If("name = ?", "alice").
		CQL()
	want := "UPDATE users SET name = ? WHERE id = ? IF name = ?"
	if stmt != want {
		t.Fatalf("got %q want %q", stmt, want)
	}
	if len(args) != 3 {
		t.Fatalf("bad args count: %d", len(args))
	}
}

func TestUpdateCounter(t *testing.T) {
	stmt, _ := (&CassandraDB{}).Update("hits").
		Increment("count", 1).
		WhereEq("page", "/home").
		CQL()
	if !strings.Contains(stmt, "count = count + ?") {
		t.Fatalf("bad counter increment: %s", stmt)
	}
}

func TestDeleteCQL(t *testing.T) {
	stmt, _ := (&CassandraDB{}).Delete("users").
		Columns("email").
		WhereEq("id", 1).
		IfExists().
		CQL()
	want := "DELETE email FROM users WHERE id = ? IF EXISTS"
	if stmt != want {
		t.Fatalf("got %q want %q", stmt, want)
	}
}

func TestSelectCQL(t *testing.T) {
	stmt, args := (&CassandraDB{}).Select("users", "id", "name").
		WhereEq("id", 1).
		OrderBy("created_at", Desc).
		Limit(10).
		AllowFiltering().
		CQL()
	want := "SELECT id, name FROM users WHERE id = ? ORDER BY created_at DESC LIMIT 10 ALLOW FILTERING"
	if stmt != want {
		t.Fatalf("got %q want %q", stmt, want)
	}
	if len(args) != 1 {
		t.Fatalf("bad args count: %d", len(args))
	}
}

func TestSelectANN(t *testing.T) {
	stmt, _ := (&CassandraDB{}).Select("embeddings", "id").
		ANNOf("vector", []float32{0.1, 0.2, 0.3}, 5).
		CQL()
	if !strings.Contains(stmt, "ORDER BY vector ANN OF [0.1, 0.2, 0.3] LIMIT 5") {
		t.Fatalf("bad ANN: %s", stmt)
	}
}

func TestSelectIn(t *testing.T) {
	stmt, args := (&CassandraDB{}).Select("users", "*").
		WhereIn("id", 1, 2, 3).
		CQL()
	if !strings.Contains(stmt, "id IN (?, ?, ?)") {
		t.Fatalf("bad IN clause: %s", stmt)
	}
	if len(args) != 3 {
		t.Fatalf("bad args count: %d", len(args))
	}
}

func TestCollectionsAndFrozen(t *testing.T) {
	list := List(TypeText)
	set := Set(TypeInt)
	m := Map(TypeText, TypeInt)
	tup := Tuple(TypeText, TypeInt)
	fr := Frozen(m)
	if string(list) != "list<text>" ||
		string(set) != "set<int>" ||
		string(m) != "map<text, int>" ||
		string(tup) != "tuple<text, int>" ||
		string(fr) != "frozen<map<text, int>>" {
		t.Fatalf("collection types wrong: %s %s %s %s %s", list, set, m, tup, fr)
	}
}

func TestSplitStatements(t *testing.T) {
	script := "CREATE TABLE a (id int PRIMARY KEY); INSERT INTO a VALUES (1); "
	stmts := splitStatements(script)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d: %v", len(stmts), stmts)
	}
}

func TestSplitStatementsRespectsDollarQuotedBody(t *testing.T) {
	udf := "CREATE FUNCTION f(a int) RETURNS NULL ON NULL INPUT RETURNS int LANGUAGE java AS $$return a; return 1;$$; SELECT * FROM x"
	stmts := splitStatements(udf)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d:\n%v", len(stmts), stmts)
	}
}

func TestParseConsistency(t *testing.T) {
	cases := map[string]gocql.Consistency{
		"one":          gocql.One,
		"quorum":       gocql.Quorum,
		"all":          gocql.All,
		"local_quorum": gocql.LocalQuorum,
		// 空字串現在 fallback 為 LocalOne（修正 Critical-1：舊行為悄悄變 Quorum 害單節點 dev 卡死）
		"": gocql.LocalOne,
	}
	for in, want := range cases {
		if got := parseConsistency(in); got != want {
			t.Errorf("parseConsistency(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	if quoteIdent("simple") != "simple" {
		t.Fatal("lowercase should not be quoted")
	}
	if quoteIdent("CaseSensitive") != `"CaseSensitive"` {
		t.Fatal("uppercase must be quoted")
	}
	if quoteIdent(`bad"name`) != `"bad""name"` {
		t.Fatal("quotes must be escaped")
	}
}

func TestCompactionOptionsToCQL(t *testing.T) {
	c := CompactionOptions{
		Class: CompactionUnified,
		Extra: map[string]interface{}{
			"scaling_parameters": "T4",
			"target_sstable_size": "1GiB",
		},
	}
	got := c.ToCQL()
	for _, s := range []string{
		"'class': 'UnifiedCompactionStrategy'",
		"'scaling_parameters': 'T4'",
		"'target_sstable_size': '1GiB'",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in %q", s, got)
		}
	}
}

type user struct {
	ID    int    `cql:"id,pk"`
	Name  string `cql:"name"`
	Email string `cql:"email,omitempty"`
}

func (user) TableName() string { return "users" }

func TestOmitEmpty(t *testing.T) {
	// Just verifies that ParseModel captures the omitempty flag.
	info, err := ParseModel(&user{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range info.Fields {
		if f.Name == "email" {
			found = true
			if !f.OmitEmpty {
				t.Fatal("email should be omitempty")
			}
		}
	}
	if !found {
		t.Fatal("email field not found")
	}
}

func TestTriggerCreateCQL(t *testing.T) {
	got := (&CassandraDB{}).Trigger("audit").
		On("logs.events").
		Using("com.example.triggers.AuditTrigger").
		CreateCQL()
	want := `CREATE TRIGGER IF NOT EXISTS audit ON logs.events USING 'com.example.triggers.AuditTrigger'`
	if got != want {
		t.Fatalf("trigger create CQL mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestTriggerCreateCQLWithoutIfNotExists(t *testing.T) {
	got := (&CassandraDB{}).Trigger("audit").
		IfNotExists(false).
		On("events").
		Using("com.example.AuditTrigger").
		CreateCQL()
	want := `CREATE TRIGGER audit ON events USING 'com.example.AuditTrigger'`
	if got != want {
		t.Fatalf("trigger create CQL mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestTriggerDropCQL(t *testing.T) {
	got := (&CassandraDB{}).Trigger("audit").
		On("events").
		Keyspace("logs").
		DropCQL(true)
	want := `DROP TRIGGER IF EXISTS audit ON logs.events`
	if got != want {
		t.Fatalf("trigger drop CQL mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestRoleCreateCQL(t *testing.T) {
	got := (&CassandraDB{}).Role("alice").
		Password("s3cret").
		Login(true).
		Superuser(false).
		CreateCQL()
	want := `CREATE ROLE IF NOT EXISTS alice WITH PASSWORD = 's3cret' AND SUPERUSER = false AND LOGIN = true`
	if got != want {
		t.Fatalf("create role CQL mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestRoleAlterAndDropCQL(t *testing.T) {
	alter := (&CassandraDB{}).Role("bob").Password("newpw").AlterCQL()
	if alter != `ALTER ROLE bob WITH PASSWORD = 'newpw'` {
		t.Fatalf("alter CQL mismatch: %s", alter)
	}
	drop := (&CassandraDB{}).Role("bob").DropCQL(true)
	if drop != `DROP ROLE IF EXISTS bob` {
		t.Fatalf("drop CQL mismatch: %s", drop)
	}
}

func TestGrantCQL(t *testing.T) {
	stmt, err := GrantCQL(PermSelect, TableResource("logs.events"), "reader")
	if err != nil {
		t.Fatal(err)
	}
	want := `GRANT SELECT ON TABLE logs.events TO reader`
	if stmt != want {
		t.Fatalf("grant CQL mismatch:\n got: %s\nwant: %s", stmt, want)
	}
}

func TestGrantAllKeyspaces(t *testing.T) {
	stmt, err := GrantCQL(PermAll, AllKeyspaces(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	want := `GRANT ALL PERMISSIONS ON ALL KEYSPACES TO admin`
	if stmt != want {
		t.Fatalf("grant all CQL mismatch:\n got: %s\nwant: %s", stmt, want)
	}
}

func TestRevokeCQL(t *testing.T) {
	stmt, err := RevokeCQL(PermModify, KeyspaceResource("analytics"), "writer")
	if err != nil {
		t.Fatal(err)
	}
	want := `REVOKE MODIFY ON KEYSPACE analytics FROM writer`
	if stmt != want {
		t.Fatalf("revoke CQL mismatch:\n got: %s\nwant: %s", stmt, want)
	}
}

func TestListPermissionsCQL(t *testing.T) {
	if got := ListPermissionsCQL(""); got != "LIST ALL PERMISSIONS" {
		t.Fatalf("list all perms: %s", got)
	}
	if got := ListPermissionsCQL("alice"); got != "LIST ALL PERMISSIONS OF alice" {
		t.Fatalf("list perms of role: %s", got)
	}
}

func TestResourceValidation(t *testing.T) {
	if _, err := (Resource{Kind: ResTable}).cql(); err == nil {
		t.Fatal("expected error for TABLE without Name")
	}
	if _, err := (Resource{Kind: "NOPE"}).cql(); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestDescribeCQL(t *testing.T) {
	cases := []struct {
		target DescribeTarget
		name   string
		want   string
	}{
		{DescKeyspaces, "", "DESCRIBE KEYSPACES"},
		{DescKeyspace, "analytics", `DESCRIBE KEYSPACE analytics`},
		{DescTable, "logs.events", `DESCRIBE TABLE logs.events`},
		{DescType, "ks.address", `DESCRIBE TYPE ks.address`},
		{DescMaterialized, "ks.mv", `DESCRIBE MATERIALIZED VIEW ks.mv`},
		{DescFullSchema, "", "DESCRIBE FULL SCHEMA"},
	}
	for _, c := range cases {
		if got := DescribeCQL(c.target, c.name); got != c.want {
			t.Errorf("DescribeCQL(%s,%q) = %q; want %q", c.target, c.name, got, c.want)
		}
	}
}

func TestRenderTableDDL(t *testing.T) {
	tbl := TableInfo{Keyspace: "logs", Name: "events", Comment: "append log"}
	cols := []ColumnInfo{
		{Column: "tenant_id", Kind: "partition_key", Position: 0, Type: "uuid"},
		{Column: "bucket", Kind: "partition_key", Position: 1, Type: "int"},
		{Column: "ts", Kind: "clustering", Position: 0, Type: "timestamp", ClusteringOrder: "desc"},
		{Column: "payload", Kind: "regular", Type: "text"},
	}
	got := RenderTableDDL(tbl, cols)
	for _, c := range []string{
		"CREATE TABLE logs.events",
		"tenant_id uuid",
		"bucket int",
		"ts timestamp",
		"payload text",
		"PRIMARY KEY ((tenant_id, bucket), ts)",
		"CLUSTERING ORDER BY (ts DESC)",
		"comment = 'append log'",
	} {
		if !strings.Contains(got, c) {
			t.Errorf("missing %q in:\n%s", c, got)
		}
	}
}

func TestRenderTableDDLSinglePK(t *testing.T) {
	tbl := TableInfo{Name: "users"}
	cols := []ColumnInfo{
		{Column: "id", Kind: "partition_key", Position: 0, Type: "uuid"},
		{Column: "name", Kind: "regular", Type: "text"},
	}
	got := RenderTableDDL(tbl, cols)
	if !strings.Contains(got, "PRIMARY KEY (id)") {
		t.Fatalf("expected single PK parens-free:\n%s", got)
	}
}

func TestNodetoolNilSessionGuards(t *testing.T) {
	db := &CassandraDB{}
	ctx := context.Background()
	if _, err := db.LocalInfo(ctx); err == nil {
		t.Fatal("expected error on nil session")
	}
	if _, err := db.Peers(ctx); err == nil {
		t.Fatal("expected error on nil session")
	}
	if _, err := db.ThreadPools(ctx); err == nil {
		t.Fatal("expected error on nil session")
	}
	if _, err := db.CompactionTasks(ctx); err == nil {
		t.Fatal("expected error on nil session")
	}
	if _, err := db.Clients(ctx); err == nil {
		t.Fatal("expected error on nil session")
	}
	if _, err := db.Settings(ctx); err == nil {
		t.Fatal("expected error on nil session")
	}
	if _, err := db.Caches(ctx); err == nil {
		t.Fatal("expected error on nil session")
	}
	if _, err := db.TableSizeEstimates(ctx, "ks", "t"); err == nil {
		t.Fatal("expected error on nil session")
	}
	if _, err := db.RawSystemViewRow(ctx, "clients", nil); err == nil {
		t.Fatal("expected error on nil session")
	}
}

func TestTableSizeEstimatesRequiresKsTable(t *testing.T) {
	db := &CassandraDB{config: Config{}}
	if _, err := db.TableSizeEstimates(context.Background(), "", ""); err == nil {
		t.Fatal("expected error when keyspace and table are missing")
	}
}

func TestNodetoolExecConnFlags(t *testing.T) {
	n := &NodetoolExec{
		Binary:   "nodetool",
		Host:     "10.0.0.1",
		Port:     7199,
		Username: "cass",
		Password: "pw",
	}
	args := n.connFlags()
	joined := strings.Join(args, " ")
	for _, s := range []string{"-h 10.0.0.1", "-p 7199", "-u cass", "-pw pw"} {
		if !strings.Contains(joined, s) {
			t.Errorf("missing %q in %q", s, joined)
		}
	}
}

func TestNodetoolExecEmptySubcommand(t *testing.T) {
	n := NewNodetool("")
	if _, err := n.Run(context.Background(), ""); err == nil {
		t.Fatal("expected error on empty subcommand")
	}
}

func TestNodetoolRefreshRequiresArgs(t *testing.T) {
	n := NewNodetool("")
	if err := n.Refresh(context.Background(), "", ""); err == nil {
		t.Fatal("expected error when keyspace and table are missing")
	}
}

func TestNodetoolMoveRequiresToken(t *testing.T) {
	n := NewNodetool("")
	if err := n.Move(context.Background(), ""); err == nil {
		t.Fatal("expected error when token is missing")
	}
}

func TestNodetoolBinaryNotFound(t *testing.T) {
	n := &NodetoolExec{Binary: "__nodetool_does_not_exist__"}
	if _, err := n.Run(context.Background(), "version"); err == nil {
		t.Fatal("expected error when binary is missing")
	}
}

func TestSchemaDiffAddAndDrop(t *testing.T) {
	old := &Schema{Keyspaces: []KeyspaceSchema{
		{
			Name: "app",
			Tables: []TableSchema{
				{Keyspace: "app", Name: "users", Columns: []ColumnSchema{
					{Name: "id", Kind: "partition_key", Type: "uuid"},
					{Name: "email", Kind: "regular", Type: "text"},
				}},
				{Keyspace: "app", Name: "legacy"},
			},
		},
	}}
	new := &Schema{Keyspaces: []KeyspaceSchema{
		{
			Name: "app",
			Tables: []TableSchema{
				{Keyspace: "app", Name: "users", Columns: []ColumnSchema{
					{Name: "id", Kind: "partition_key", Type: "uuid"},
					{Name: "email", Kind: "regular", Type: "varchar"}, // altered
					{Name: "bio", Kind: "regular", Type: "text"},      // added
				}},
				// legacy dropped
				{Keyspace: "app", Name: "events"}, // added
			},
		},
		{Name: "analytics"}, // added keyspace
	}}

	changes := SchemaDiff(old, new)
	kinds := map[SchemaChangeKind]int{}
	for _, c := range changes {
		kinds[c.Kind]++
	}
	if kinds[ChangeAddColumn] != 1 {
		t.Errorf("want 1 add_column, got %d", kinds[ChangeAddColumn])
	}
	if kinds[ChangeAlterColumn] != 1 {
		t.Errorf("want 1 alter_column, got %d", kinds[ChangeAlterColumn])
	}
	if kinds[ChangeDropTable] != 1 {
		t.Errorf("want 1 drop_table, got %d", kinds[ChangeDropTable])
	}
	if kinds[ChangeAddTable] < 1 {
		t.Errorf("want ≥1 add_table, got %d", kinds[ChangeAddTable])
	}
	if kinds[ChangeAddKeyspace] != 1 {
		t.Errorf("want 1 add_keyspace, got %d", kinds[ChangeAddKeyspace])
	}
}

func TestSchemaDiffAlterKeyspace(t *testing.T) {
	old := &Schema{Keyspaces: []KeyspaceSchema{
		{Name: "app", Replication: map[string]string{"class": "SimpleStrategy", "replication_factor": "3"}, DurableWrites: true},
	}}
	new := &Schema{Keyspaces: []KeyspaceSchema{
		{Name: "app", Replication: map[string]string{"class": "SimpleStrategy", "replication_factor": "5"}, DurableWrites: true},
	}}
	changes := SchemaDiff(old, new)
	found := false
	for _, c := range changes {
		if c.Kind == ChangeAlterKeyspace && c.Keyspace == "app" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected alter_keyspace, got %+v", changes)
	}
}

func TestSchemaMarshalJSON(t *testing.T) {
	s := &Schema{
		CapturedAt: time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC),
		Keyspaces: []KeyspaceSchema{
			{Name: "app", Tables: []TableSchema{{Keyspace: "app", Name: "users"}}},
		},
	}
	b, err := s.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, want := range []string{`"captured_at"`, `"keyspaces"`, `"app"`, `"users"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in JSON:\n%s", want, out)
		}
	}
}

func TestColumnSchemaSortOrder(t *testing.T) {
	cols := []ColumnSchema{
		{Name: "payload", Kind: "regular"},
		{Name: "ts", Kind: "clustering", Position: 0},
		{Name: "bucket", Kind: "partition_key", Position: 1},
		{Name: "tenant_id", Kind: "partition_key", Position: 0},
		{Name: "is_active", Kind: "static"},
	}
	sortColumnsSchema(cols)
	want := []string{"tenant_id", "bucket", "ts", "is_active", "payload"}
	for i, c := range cols {
		if c.Name != want[i] {
			t.Errorf("position %d: got %s want %s", i, c.Name, want[i])
		}
	}
}

func TestIntrospectNilSession(t *testing.T) {
	if _, err := (&CassandraDB{}).Introspect(context.Background(), IntrospectOptions{}); err == nil {
		t.Fatal("expected error on nil session")
	}
}

func TestMemTracerCollectsSessionIDs(t *testing.T) {
	tr := NewMemTracer()
	if _, ok := tr.Last(); ok {
		t.Fatal("expected no sessions initially")
	}
	id := gocql.TimeUUID()
	tr.Trace(id[:])
	got, ok := tr.Last()
	if !ok || got != id {
		t.Fatalf("expected last=%s, got %s ok=%v", id, got, ok)
	}
	if s := tr.Sessions(); len(s) != 1 || s[0] != id {
		t.Fatalf("unexpected sessions: %v", s)
	}
	tr.Reset()
	if _, ok := tr.Last(); ok {
		t.Fatal("expected empty after Reset")
	}
}

func TestMemTracerIgnoresInvalidID(t *testing.T) {
	tr := NewMemTracer()
	tr.Trace([]byte{1, 2, 3})
	if _, ok := tr.Last(); ok {
		t.Fatal("expected invalid id to be ignored")
	}
}

func TestGetTraceNilSession(t *testing.T) {
	if _, err := (&CassandraDB{}).GetTrace(context.Background(), gocql.TimeUUID()); err == nil {
		t.Fatal("expected error on nil session")
	}
}

func TestTraceFormatContainsHeader(t *testing.T) {
	id := gocql.TimeUUID()
	tr := Trace{
		Session: TraceSession{
			SessionID:      id,
			Command:        "QUERY",
			Coordinator:    "10.0.0.1",
			DurationMicros: 1500,
		},
		Events: []TraceEvent{
			{Source: "10.0.0.1", SourceElapsed: 500, Activity: "Parsing", Thread: "Native-1"},
			{Source: "10.0.0.1", SourceElapsed: 1400, Activity: "Done", Thread: "Native-1"},
		},
	}
	out := tr.Format()
	if !strings.Contains(out, id.String()) || !strings.Contains(out, "Parsing") || !strings.Contains(out, "Done") {
		t.Fatalf("unexpected format output: %s", out)
	}
	if tr.Duration() != 1500*time.Microsecond {
		t.Fatalf("duration mismatch: %s", tr.Duration())
	}
	if tr.TotalElapsed() != 1400*time.Microsecond {
		t.Fatalf("total elapsed mismatch: %s", tr.TotalElapsed())
	}
}

func TestNB5ScenarioArgsDeterministic(t *testing.T) {
	nb := &NB5Exec{Binary: "nb5", Host: "10.0.0.1", LocalDC: "dc1", Driver: "cqld4"}
	s := nb.Scenario("cql-iot").
		Phase(PhaseMain).
		Cycles("1M").
		Threads(32).
		CycleRate("10000").
		Param("rampup-cycles", "100k").
		Param("keycount", "1000000").
		Errors("count").
		Extra("--report-summary-to", "stdout:60s")

	args := s.Args()
	// activity first
	if args[0] != "cql-iot" {
		t.Fatalf("activity must be first arg, got %v", args)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"driver=cqld4", "host=10.0.0.1", "localdc=dc1",
		"tags=block:main", "cycles=1M", "cyclerate=10000",
		"threads=32", "errors=count",
		"keycount=1000000", "rampup-cycles=100k",
		"--report-summary-to", "stdout:60s",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
}

func TestNB5RunActivityRequiresName(t *testing.T) {
	nb := &NB5Exec{Binary: "nb5"}
	if _, err := nb.RunActivity(context.Background(), "", nil); err == nil {
		t.Fatal("expected error on empty activity")
	}
}

func TestNB5RunPhaseValidates(t *testing.T) {
	nb := &NB5Exec{Binary: "nb5"}
	if _, err := nb.RunPhase(context.Background(), "", PhaseMain, "", 0, nil); err == nil {
		t.Fatal("expected workload-required error")
	}
	if _, err := nb.RunPhase(context.Background(), "cql-iot", "", "", 0, nil); err == nil {
		t.Fatal("expected phase-required error")
	}
}

func TestNB5ParseSummary(t *testing.T) {
	out := `
Setting up scenario...
Running activity...
Scenario finished: cycles=1000000 errors=0 rate=48231.5 ops/s duration=20.7s
`
	s := ParseSummary(out)
	if s.TotalCycles != 1000000 {
		t.Errorf("cycles: got %d", s.TotalCycles)
	}
	if s.Errors != 0 {
		t.Errorf("errors: got %d", s.Errors)
	}
	if s.OpsPerSec < 48000 || s.OpsPerSec > 49000 {
		t.Errorf("rate: got %v", s.OpsPerSec)
	}
	if s.Duration != 20700*time.Millisecond {
		t.Errorf("duration: got %s", s.Duration)
	}
}

func TestNB5WriteInlineWorkload(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/workload.yaml"
	abs, err := WriteInlineWorkload(path, "app", "kv")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"scenarios:", "blocks:", "app.kv", "schema:", "rampup:", "main:"} {
		if !strings.Contains(body, want) {
			t.Errorf("workload missing %q", want)
		}
	}
}

func TestParseConsistencyStrict(t *testing.T) {
	for _, in := range []string{"any", "ONE", "Local-One", "local_one", "LOCAL_QUORUM"} {
		if _, err := parseConsistencyStrict(in); err != nil {
			t.Errorf("expected %q to parse, got %v", in, err)
		}
	}
	if cl, _ := parseConsistencyStrict(""); cl != gocql.LocalOne {
		t.Errorf("empty string should default to LocalOne, got %v", cl)
	}
	if _, err := parseConsistencyStrict("typo_one"); err == nil {
		t.Error("expected error on typo_one")
	}
}

func TestParseConsistencyFallbackLocalOne(t *testing.T) {
	// 舊 API：拼錯不再回 Quorum，改回 LocalOne
	if got := parseConsistency("typo"); got != gocql.LocalOne {
		t.Errorf("expected LocalOne fallback, got %v", got)
	}
}

func TestInitStrictConsistencyError(t *testing.T) {
	c := &CassandraDB{config: Config{
		Hosts:             []string{"127.0.0.1"},
		Consistency:       "typo_consistency",
		StrictConsistency: true,
	}}
	if err := c.init(); err == nil {
		t.Fatal("expected init() to error on unknown consistency under strict mode")
	}
}

func TestInitAppliesRetryAndReconnectDefaults(t *testing.T) {
	c := &CassandraDB{config: Config{Hosts: []string{"127.0.0.1"}}}
	if err := c.init(); err != nil {
		t.Fatal(err)
	}
	if c.cluster.RetryPolicy == nil {
		t.Error("RetryPolicy not set")
	}
	if c.cluster.ReconnectionPolicy == nil {
		t.Error("ReconnectionPolicy not set")
	}
	rp, ok := c.cluster.RetryPolicy.(*gocql.ExponentialBackoffRetryPolicy)
	if !ok || rp.NumRetries != 3 {
		t.Errorf("expected default NumRetries=3, got %#v", c.cluster.RetryPolicy)
	}
}

func TestInitDisableRetryAndReconnect(t *testing.T) {
	c := &CassandraDB{config: Config{
		Hosts:        []string{"127.0.0.1"},
		NumRetries:   -1,
		ReconnectMax: -1,
	}}
	if err := c.init(); err != nil {
		t.Fatal(err)
	}
	if c.cluster.RetryPolicy != nil {
		t.Error("RetryPolicy should be disabled when NumRetries=-1")
	}
	if c.cluster.ReconnectionPolicy != nil {
		t.Error("ReconnectionPolicy should be disabled when ReconnectMax=-1")
	}
}

func TestInitTLSEnabled(t *testing.T) {
	c := &CassandraDB{config: Config{
		Hosts: []string{"127.0.0.1"},
		TLS: TLSConfig{
			Enabled:    true,
			CertFile:   "/tmp/c.pem",
			KeyFile:    "/tmp/c.key",
			CaFile:     "/tmp/ca.pem",
			ServerName: "cass.local",
		},
	}}
	if err := c.init(); err != nil {
		t.Fatal(err)
	}
	if c.cluster.SslOpts == nil {
		t.Fatal("SslOpts should be set when TLS.Enabled=true")
	}
	if c.cluster.SslOpts.CertPath != "/tmp/c.pem" {
		t.Errorf("CertPath mismatch: %s", c.cluster.SslOpts.CertPath)
	}
}

func TestInitTLSCertWithoutKeyError(t *testing.T) {
	c := &CassandraDB{config: Config{
		Hosts: []string{"127.0.0.1"},
		TLS:   TLSConfig{Enabled: true, CertFile: "/tmp/c.pem"},
	}}
	if err := c.init(); err == nil {
		t.Fatal("expected error: cert without key")
	}
}

func TestCloseIdempotent(t *testing.T) {
	c := &CassandraDB{}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close should be no-op, got %v", err)
	}
	if !c.closed {
		t.Error("closed flag should be true")
	}
}

func TestCloseConcurrent(t *testing.T) {
	c := &CassandraDB{}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = c.Close() }()
	}
	wg.Wait()
}

func TestConnectAfterCloseRejected(t *testing.T) {
	c := &CassandraDB{config: Config{Hosts: []string{"127.0.0.1"}}}
	_ = c.init()
	_ = c.Close()
	if err := c.Connect(); err == nil {
		t.Fatal("Connect() after Close() should error")
	}
}

func TestInitMapHostsInterfaceSlice(t *testing.T) {
	c := &CassandraDB{}
	err := c.Init(map[string]interface{}{
		"hosts":              []interface{}{"1.1.1.1", "2.2.2.2"},
		"keyspace":           "ks",
		"consistency":        "local_quorum",
		"connect_timeout":    "3s",
		"timeout":            int64(5_000_000_000), // 5s
		"num_retries":        2,
		"reconnect_interval": "500ms",
		"strict_consistency": false,
		"tls": map[string]interface{}{
			"enabled":              true,
			"cert_file":            "/c.pem",
			"key_file":             "/c.key",
			"insecure_skip_verify": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.config.Hosts) != 2 || c.config.Hosts[0] != "1.1.1.1" {
		t.Errorf("hosts not parsed: %v", c.config.Hosts)
	}
	if c.config.ConnectTimeout != 3*time.Second {
		t.Errorf("connect_timeout: %v", c.config.ConnectTimeout)
	}
	if c.config.Timeout != 5*time.Second {
		t.Errorf("timeout: %v", c.config.Timeout)
	}
	if c.config.NumRetries != 2 {
		t.Errorf("num_retries: %d", c.config.NumRetries)
	}
	if !c.config.TLS.Enabled || c.config.TLS.CertFile != "/c.pem" {
		t.Errorf("tls block: %+v", c.config.TLS)
	}
}

func TestVectorMarshalRoundTrip(t *testing.T) {
	v := []float32{0.1, -0.5, 1.5, 0, 3.14159}
	blob, err := MarshalVectorFloat32(v, len(v))
	if err != nil {
		t.Fatal(err)
	}
	if len(blob) != 4*len(v) {
		t.Fatalf("blob len: got %d want %d", len(blob), 4*len(v))
	}
	got, err := UnmarshalVectorFloat32(blob, len(v))
	if err != nil {
		t.Fatal(err)
	}
	for i := range v {
		if got[i] != v[i] {
			t.Errorf("idx %d: got %v want %v", i, got[i], v[i])
		}
	}
}

func TestVectorDimMismatch(t *testing.T) {
	if _, err := MarshalVectorFloat32([]float32{1, 2, 3}, 4); err == nil {
		t.Error("expected length mismatch error")
	}
	if _, err := UnmarshalVectorFloat32(make([]byte, 4), 2); err == nil {
		t.Error("expected blob length mismatch error")
	}
	if _, err := MarshalVectorFloat32(nil, 0); err == nil {
		t.Error("expected dim<=0 error")
	}
}
