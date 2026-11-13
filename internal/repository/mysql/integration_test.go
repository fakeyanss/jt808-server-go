package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// 本文件是 MySQL 真实实例的集成验证，全部用例在未设置 JT808_TEST_MYSQL_DSN 时跳过。
//
// 约定的运行方式：测试库必须是独立的空库（例如 jt808_test），
// 用例会在开始时清空表，结束时由调用方负责 drop database 或保留空 schema。

// mysqlTestDSN 返回集成测试连接串；未配置时跳过。
func mysqlTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("JT808_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JT808_TEST_MYSQL_DSN is not set; skipping MySQL integration test")
	}
	return dsn
}

// 协调表使用 INSERT IGNORE + 条件 UPDATE，MySQL 方言必须单独验证。
func TestCoordinationStoreOnRealMySQL(t *testing.T) {
	ctx := context.Background()
	dsn := mysqlTestDSN(t)
	raw := openRaw(t, dsn)
	dropAllTables(t, raw)
	_, err := Migrate(dsn)
	require.NoError(t, err)
	store, err := Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	coord := store.CoordinationStore()
	now := time.Now()

	acquired, err := coord.Acquire(ctx, "cleanup", "replica-a", now, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = coord.Acquire(ctx, "cleanup", "replica-b", now, time.Minute)
	require.NoError(t, err)
	require.False(t, acquired)
	acquired, err = coord.Acquire(ctx, "cleanup", "replica-a", now.Add(30*time.Second), time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = coord.Acquire(ctx, "cleanup", "replica-b", now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	first, err := coord.MarkOnce(ctx, "auto-9208:phone:alarm", now, time.Minute)
	require.NoError(t, err)
	require.True(t, first)
	second, err := coord.MarkOnce(ctx, "auto-9208:phone:alarm", now.Add(30*time.Second), time.Minute)
	require.NoError(t, err)
	require.False(t, second)
	third, err := coord.MarkOnce(ctx, "auto-9208:phone:alarm", now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.True(t, third)
}

// 节点注册表全部时间由 CURRENT_TIMESTAMP(3) 计算，MySQL 方言必须单独验证。
func TestGatewayNodeRepositoryOnRealMySQL(t *testing.T) {
	ctx := context.Background()
	dsn := mysqlTestDSN(t)
	requireMigrated(t, dsn)
	db := openRaw(t, dsn)
	repo := openStore(t, dsn).GatewayNodeRepository()

	const gatewayID = "gw-it"
	_, err := db.Exec(`DELETE FROM gateway_nodes WHERE gateway_id = ?`, gatewayID)
	require.NoError(t, err)

	require.NoError(t, repo.Register(ctx, gatewayID, "inst-a", "http://gw-it:9001"))
	node, err := repo.Get(ctx, gatewayID)
	require.NoError(t, err)
	require.NotNil(t, node)
	require.Equal(t, "inst-a", node.InstanceID)
	require.Equal(t, "http://gw-it:9001", node.DownlinkURL)
	require.Equal(t, "online", node.Status)
	require.WithinDuration(t, time.Now(), node.StartedAt, time.Minute)
	require.WithinDuration(t, time.Now().Add(30*time.Second), node.ExpiresAt, time.Minute)

	ok, err := repo.Heartbeat(ctx, gatewayID, "inst-a")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = repo.Heartbeat(ctx, gatewayID, "inst-stale")
	require.NoError(t, err)
	require.False(t, ok)

	// 新实例注册后旧实例心跳必须被围栏，同 gateway_id 的旧目录行被清理。
	const phone = "13800000997"
	_, err = db.Exec(`DELETE FROM session_directory WHERE phone = ?`, phone)
	require.NoError(t, err)
	dir := openStore(t, dsn).SessionDirectoryRepository()
	_, err = dir.Claim(ctx, phone, repository.SessionOwner{
		GatewayID: gatewayID, InstanceID: "inst-a", SessionID: "sess-1",
	})
	require.NoError(t, err)
	require.NoError(t, repo.Register(ctx, gatewayID, "inst-b", "http://gw-it:9001"))
	record, err := dir.Get(ctx, phone)
	require.NoError(t, err)
	require.Nil(t, record)
	ok, err = repo.Heartbeat(ctx, gatewayID, "inst-a")
	require.NoError(t, err)
	require.False(t, ok)

	node, err = repo.Get(ctx, gatewayID)
	require.NoError(t, err)
	require.Equal(t, "inst-b", node.InstanceID)

	require.NoError(t, repo.Deregister(ctx, gatewayID, "inst-b"))
	node, err = repo.Get(ctx, gatewayID)
	require.NoError(t, err)
	require.Equal(t, "offline", node.Status)

	stats, err := repo.Stats(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, stats.Offline, int64(1))

	// 仍标记 online 但已过期的节点单独计数。
	_, err = db.Exec(`UPDATE gateway_nodes
		SET status = 'online', expires_at = DATE_SUB(CURRENT_TIMESTAMP(3), INTERVAL 1 SECOND)
		WHERE gateway_id = ?`, gatewayID)
	require.NoError(t, err)
	stats, err = repo.Stats(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, stats.Expired, int64(1))

	missing, err := repo.Get(ctx, "gw-it-missing")
	require.NoError(t, err)
	require.Nil(t, missing)
}

// 会话目录同样只用数据库时间；claim 幂等与 generation 递增必须在真库验证。
func TestSessionDirectoryOnRealMySQL(t *testing.T) {
	ctx := context.Background()
	dsn := mysqlTestDSN(t)
	requireMigrated(t, dsn)
	db := openRaw(t, dsn)
	dir := openStore(t, dsn).SessionDirectoryRepository()

	const phone = "13800000999"
	_, err := db.Exec(`DELETE FROM session_directory WHERE phone = ?`, phone)
	require.NoError(t, err)

	ownerA := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-1"}
	generation, err := dir.Claim(ctx, phone, ownerA)
	require.NoError(t, err)
	require.Equal(t, uint64(1), generation)

	generation, err = dir.Claim(ctx, phone, ownerA)
	require.NoError(t, err)
	require.Equal(t, uint64(1), generation, "同一 owner 重复 claim 不得递增")

	record, err := dir.Get(ctx, phone)
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, ownerA, record.SessionOwner)
	require.WithinDuration(t, time.Now(), record.ClaimedAt, time.Minute)

	ownerB := repository.SessionOwner{GatewayID: "gw-1", InstanceID: "inst-a", SessionID: "sess-2"}
	generation, err = dir.Claim(ctx, phone, ownerB)
	require.NoError(t, err)
	require.Equal(t, uint64(2), generation)

	valid, err := dir.Validate(ctx, phone, ownerA, 1)
	require.NoError(t, err)
	require.False(t, valid)
	valid, err = dir.Validate(ctx, phone, ownerB, 2)
	require.NoError(t, err)
	require.True(t, valid)
	valid, err = dir.Validate(ctx, phone, ownerB, 1)
	require.NoError(t, err)
	require.False(t, valid)

	require.NoError(t, dir.Release(ctx, phone, ownerA, 1))
	record, err = dir.Get(ctx, phone)
	require.NoError(t, err)
	require.NotNil(t, record)

	require.NoError(t, dir.Release(ctx, phone, ownerB, 2))
	record, err = dir.Get(ctx, phone)
	require.NoError(t, err)
	require.Nil(t, record)
}

// Route 的联表判定用 CURRENT_TIMESTAMP(3)，MySQL 方言必须单独验证。
func TestSessionDirectoryRouteOnRealMySQL(t *testing.T) {
	ctx := context.Background()
	dsn := mysqlTestDSN(t)
	requireMigrated(t, dsn)
	db := openRaw(t, dsn)
	store := openStore(t, dsn)
	dir := store.SessionDirectoryRepository()
	nodes := store.GatewayNodeRepository()

	const (
		gatewayID = "gw-route-it"
		phone     = "13800000998"
	)
	_, err := db.Exec(`DELETE FROM session_directory WHERE phone = ?`, phone)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM gateway_nodes WHERE gateway_id = ?`, gatewayID)
	require.NoError(t, err)

	owner := repository.SessionOwner{GatewayID: gatewayID, InstanceID: "inst-a", SessionID: "sess-1"}
	_, err = dir.Claim(ctx, phone, owner)
	require.NoError(t, err)

	// 节点未注册。
	_, ok, err := dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, nodes.Register(ctx, gatewayID, "inst-a", "http://gw-route-it:9100"))
	url, ok, err := dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "http://gw-route-it:9100", url)

	_, ok, err = dir.Route(ctx, phone, "sess-stale")
	require.NoError(t, err)
	require.False(t, ok)

	// 节点过期由数据库时间裁决。
	_, err = db.Exec(`UPDATE gateway_nodes
		SET expires_at = DATE_SUB(CURRENT_TIMESTAMP(3), INTERVAL 1 SECOND)
		WHERE gateway_id = ?`, gatewayID)
	require.NoError(t, err)
	_, ok, err = dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.False(t, ok)

	// 目录指向的实例与节点当前实例不一致时不可路由。
	require.NoError(t, nodes.Register(ctx, gatewayID, "inst-b", "http://gw-route-it:9100"))
	_, ok, err = dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, nodes.Deregister(ctx, gatewayID, "inst-b"))
	_, ok, err = dir.Route(ctx, phone, "")
	require.NoError(t, err)
	require.False(t, ok)
}

// openRaw 打开一个独立的原始连接池，用于观察/构造 SQL 层状态。
func openRaw(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	require.NoError(t, db.PingContext(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// dropAllTables 清空当前库，保证迁移用例从“从未迁移”开始。
func dropAllTables(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	rows, err := db.QueryContext(ctx,
		`SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()`)
	require.NoError(t, err)
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	for _, name := range tables {
		_, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS `"+name+"`")
		require.NoError(t, err)
	}
}

// requireMigrated 保证库已迁移到当前内置版本，供非基线的用例复用。
func requireMigrated(t *testing.T, dsn string) {
	t.Helper()
	_, err := Migrate(dsn)
	require.NoError(t, err)
}

// TestMigrateBaselineOnRealMySQL 覆盖需求 1：真实 MySQL 执行 v1.0.0.sql、版本落库、重复执行幂等。
func TestMigrateBaselineOnRealMySQL(t *testing.T) {
	dsn := mysqlTestDSN(t)
	require.Equal(t, "1.0.0", ExpectedVersion().String())

	db := openRaw(t, dsn)
	dropAllTables(t, db)

	applied, err := Migrate(dsn)
	require.NoError(t, err)
	require.Equal(t, 1, applied, "空库应只应用一个 v1.0.0 基线")

	var versions []string
	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	require.NoError(t, err)
	for rows.Next() {
		var v string
		require.NoError(t, rows.Scan(&v))
		versions = append(versions, v)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Equal(t, []string{"1.0.0"}, versions)

	// 重复执行：不重复应用，也不再写版本行。
	applied, err = Migrate(dsn)
	require.NoError(t, err)
	require.Equal(t, 0, applied, "已应用的版本不得重复执行")
	var versionCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&versionCount))
	require.Equal(t, 1, versionCount)

	// 表清单：23 张业务表，device_runtime 已在基线内删除。
	expectedTables := []string{
		"alarms", "attachment_files", "command_outbox", "command_serial", "dedup_keys",
		"device_params", "devices", "gateway_nodes", "locations", "media_capture_items",
		"media_captures", "media_files", "media_resource_lists", "media_search_results",
		"media_upload_notices", "media_upload_tasks", "region_items", "schema_migrations",
		"session_directory", "task_leases", "terminal_catalogs", "terminal_properties", "upgrade_results",
	}
	actual := listTables(t, db)
	require.Equal(t, expectedTables, actual)
	require.NotContains(t, actual, "device_runtime")

	// 引擎与字符集：全部 InnoDB + utf8mb4。
	rows, err = db.Query(`SELECT table_name, engine, table_collation
		FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY table_name`)
	require.NoError(t, err)
	checked := 0
	for rows.Next() {
		var name, engine, collation string
		require.NoError(t, rows.Scan(&name, &engine, &collation))
		require.Equal(t, "InnoDB", engine, "table %s", name)
		require.True(t, strings.HasPrefix(collation, "utf8mb4"), "table %s collation %s", name, collation)
		checked++
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Equal(t, len(expectedTables), checked)

	// 迁移完成后 Open 的版本校验必须通过。
	store, err := Open(dsn)
	require.NoError(t, err)
	require.NoError(t, store.Close())
}

// TestCommandSerialOnRealMySQL 覆盖需求 2：播种、占用、回绕跳过在途流水号，以及 FOR UPDATE 的串行效果。
func TestCommandSerialOnRealMySQL(t *testing.T) {
	dsn := mysqlTestDSN(t)
	requireMigrated(t, dsn)
	db := openRaw(t, dsn)
	store := openStore(t, dsn)
	repo := store.CommandRepository()
	ctx := context.Background()

	const phone = "13800000001"
	_, err := db.Exec(`DELETE FROM command_serial WHERE phone = ?`, phone)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM command_outbox WHERE phone = ?`, phone)
	require.NoError(t, err)

	// 播种：从该 phone 已存在的最大 serial + 1 开始。
	insertOutboxRow(t, db, "serial-seed-1", phone, 0x8103, 100, repository.CommandStatusAcked)
	insertOutboxRow(t, db, "serial-seed-2", phone, 0x8103, 250, repository.CommandStatusAcked)
	serial, err := repo.NextSerial(ctx, phone, 0x8103)
	require.NoError(t, err)
	require.Equal(t, uint16(251), serial, "播种应取 MAX(serial)+1")

	serial, err = repo.NextSerial(ctx, phone, 0x8103)
	require.NoError(t, err)
	require.Equal(t, uint16(252), serial, "游标应逐次推进")

	// 回绕：游标指向 65535，且该 msgId 的 65535 仍 pending，必须跳过并回到 1。
	_, err = db.Exec(`UPDATE command_serial SET next_serial = 65535 WHERE phone = ?`, phone)
	require.NoError(t, err)
	insertOutboxRow(t, db, "serial-pending-65535", phone, 0x8103, 65535, repository.CommandStatusPending)
	serial, err = repo.NextSerial(ctx, phone, 0x8103)
	require.NoError(t, err)
	require.Equal(t, uint16(1), serial, "回绕后应跳过同 msgId 的在途流水号 65535")

	// FOR UPDATE 的行锁证据：另一连接 NOWAIT 必须拿不到锁。
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	var locked int64
	require.NoError(t, tx.QueryRowContext(ctx,
		`SELECT next_serial FROM command_serial WHERE phone = ? FOR UPDATE`, phone).Scan(&locked))
	other := openRaw(t, dsn)
	var blocked int64
	lockErr := other.QueryRowContext(ctx,
		`SELECT next_serial FROM command_serial WHERE phone = ? FOR UPDATE NOWAIT`, phone).Scan(&blocked)
	require.Error(t, lockErr, "持有 FOR UPDATE 行锁时另一连接不应拿到锁")
	t.Logf("NOWAIT while row locked: %v", lockErr)
	require.NoError(t, tx.Rollback())

	// 并发串行：所有分配出的流水号必须互不重复。
	const workers = 16
	const perWorker = 40
	var (
		mu   sync.Mutex
		seen = make(map[uint16]bool, workers*perWorker)
		dups []uint16
	)
	errCh := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < perWorker; i++ {
				s, err := repo.NextSerial(ctx, phone, 0x8103)
				if err != nil {
					errCh <- err
					return
				}
				mu.Lock()
				if seen[s] {
					dups = append(dups, s)
				}
				seen[s] = true
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err, "并发 NextSerial 不应报错（含死锁）")
	}
	require.Empty(t, dups, "并发 NextSerial 出现重复流水号: %v", dups)
	require.Len(t, seen, workers*perWorker)
}

// TestOutboxClaimIsExclusiveAcrossInstances 覆盖需求 3：两个独立连接池并发 ClaimPending 不重复领取。
func TestOutboxClaimIsExclusiveAcrossInstances(t *testing.T) {
	dsn := mysqlTestDSN(t)
	requireMigrated(t, dsn)
	db := openRaw(t, dsn)
	const phone = "13800000002"
	// ClaimPending 是全局 outbox 扫描，不带 phone 条件；清空整表避免拾取前序用例遗留的 pending 行。
	_, err := db.Exec(`DELETE FROM command_outbox`)
	require.NoError(t, err)

	instances := make([]*Backend, 2)
	for i := range instances {
		instances[i] = openStore(t, dsn)
	}
	require.True(t, instances[0].DB() != instances[1].DB(), "两个实例必须是独立连接池")

	const total = 60
	for i := 0; i < total; i++ {
		_, created, err := instances[0].CommandRepository().Enqueue(context.Background(), &repository.DownlinkCommand{
			CommandID:      fmt.Sprintf("claim-%03d", i),
			IdempotencyKey: fmt.Sprintf("claim-key-%03d", i),
			Phone:          phone,
			MsgID:          0x8103,
			Serial:         uint16(i + 1),
			Payload:        []byte{byte(i)},
		})
		require.NoError(t, err)
		require.True(t, created)
	}

	var (
		mu         sync.Mutex
		claimed    = make(map[string]int)
		duplicates []string
	)
	record := func(owner string, cmds []*repository.DownlinkCommand) {
		mu.Lock()
		defer mu.Unlock()
		for _, cmd := range cmds {
			claimed[cmd.CommandID]++
			if claimed[cmd.CommandID] > 1 {
				duplicates = append(duplicates, cmd.CommandID)
			}
			if cmd.LeaseOwner != owner {
				duplicates = append(duplicates, "lease-owner:"+cmd.CommandID)
			}
		}
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			inst := instances[w%len(instances)]
			owner := fmt.Sprintf("control-%d", w)
			for round := 0; round < 30; round++ {
				cmds, err := inst.CommandRepository().ClaimPending(ctx, repository.CommandClaim{
					Owner: owner,
					Now:   time.Now(),
					Limit: 5,
					Lease: time.Minute,
				})
				if err != nil {
					require.NoError(t, err)
					return
				}
				if len(cmds) == 0 {
					return
				}
				record(owner, cmds)
			}
		}(w)
	}
	wg.Wait()
	require.Empty(t, duplicates, "同一命令被重复领取或租约归属错误: %v", duplicates)
	require.Len(t, claimed, total, "所有待投命令都应被领取一次")

	var leased int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM command_outbox WHERE phone = ? AND lease_owner <> ''`, phone).Scan(&leased))
	require.Equal(t, total, leased)
}

// TestOutboxIdempotencyOnRealMySQL 覆盖需求 4：重复幂等键返回既有记录，不报错。
func TestOutboxIdempotencyOnRealMySQL(t *testing.T) {
	dsn := mysqlTestDSN(t)
	requireMigrated(t, dsn)
	db := openRaw(t, dsn)
	store := openStore(t, dsn)
	repo := store.CommandRepository()
	ctx := context.Background()

	const phone = "13800000003"
	const key = "same-idempotency-key"
	_, err := db.Exec(`DELETE FROM command_outbox WHERE phone = ?`, phone)
	require.NoError(t, err)

	first, created, err := repo.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID: "idem-first", IdempotencyKey: key, Phone: phone, MsgID: 0x8103, Serial: 1,
	})
	require.NoError(t, err)
	require.True(t, created)

	second, created, err := repo.Enqueue(ctx, &repository.DownlinkCommand{
		CommandID: "idem-second", IdempotencyKey: key, Phone: phone, MsgID: 0x8103, Serial: 2,
	})
	require.NoError(t, err, "重复幂等键不应报错")
	require.False(t, created)
	require.Equal(t, first.CommandID, second.CommandID)

	// 并发竞争：同一新键只允许一条 created=true，其余返回同一 command_id。
	const raceKey = "race-idempotency-key"
	const workers = 8
	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		ids          []string
		createdCount int
	)
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd, created, err := repo.Enqueue(ctx, &repository.DownlinkCommand{
				CommandID:      fmt.Sprintf("race-%d", i),
				IdempotencyKey: raceKey,
				Phone:          phone,
				MsgID:          0x8103,
				Serial:         uint16(100 + i),
			})
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			ids = append(ids, cmd.CommandID)
			if created {
				createdCount++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	require.Equal(t, 1, createdCount, "并发首次入队只应有一条 created=true")
	sort.Strings(ids)
	for _, id := range ids[1:] {
		require.Equal(t, ids[0], id, "并发幂等冲突必须返回同一 command_id")
	}

	var rowsCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM command_outbox WHERE phone = ? AND idempotency_key IN (?, ?)`,
		phone, key, raceKey).Scan(&rowsCount))
	require.Equal(t, 2, rowsCount, "两个幂等键各只落一行")
}

// TestAttachmentWatermarkOnRealMySQL 覆盖需求 5：并发写水位不倒退、不丢更高的更新。
func TestAttachmentWatermarkOnRealMySQL(t *testing.T) {
	dsn := mysqlTestDSN(t)
	requireMigrated(t, dsn)
	db := openRaw(t, dsn)
	store := openStore(t, dsn)
	repo := store.AttachmentFileRepository()
	ctx := context.Background()

	const phone = "13800000004"
	_, err := db.Exec(`DELETE FROM attachment_files WHERE phone = ?`, phone)
	require.NoError(t, err)
	key := repository.AttachmentFileKey{Source: repository.AttachmentSourceSubia, Phone: phone, AlarmNo: "A1", FileName: "f1.bin"}

	save := func(watermark int64, status string) {
		_, err := repo.Save(ctx, &repository.AttachmentFile{
			Source: key.Source, Phone: phone, AlarmNo: key.AlarmNo, FileName: key.FileName,
			TotalSize: 10_000, ReceivedBytes: watermark, Watermark: watermark,
			Status: status,
		})
		require.NoError(t, err)
	}

	// 先写高水位，再让一个迟到的旧快照落地：水位不得回退。
	save(5000, repository.AttachmentStatusReceiving)
	save(1000, repository.AttachmentStatusReceiving)
	got, err := repo.Get(ctx, key)
	require.NoError(t, err)
	require.GreaterOrEqual(t, got.Watermark, int64(5000), "迟到旧快照不得把水位改小")
	require.GreaterOrEqual(t, got.ReceivedBytes, int64(5000))

	// 并发：每个 goroutine 写自己严格递增的绝对快照，最终必须是全局最大值。
	const workers = 8
	const rounds = 40
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 1; r <= rounds; r++ {
				// 各 worker 的值区间互不重叠，全局最大值 = (rounds-1)*1000 + (workers-1)。
				value := int64((r-1)*1000 + w)
				if _, err := repo.Save(ctx, &repository.AttachmentFile{
					Source: key.Source, Phone: phone, AlarmNo: key.AlarmNo, FileName: key.FileName,
					TotalSize: 100_000, ReceivedBytes: value, Watermark: value,
					Status: repository.AttachmentStatusReceiving,
				}); err != nil {
					errCh <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	maxValue := int64((rounds-1)*1000 + (workers - 1))
	got, err = repo.Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, maxValue, got.Watermark, "最终水位必须是并发写入中的最大值")
	require.Equal(t, maxValue, got.ReceivedBytes)
}

// TestUniqueIndexKeyLengths 覆盖需求 6：列出唯一索引，按 utf8mb4 4 字节/字符估算并断言不超过 3072。
func TestUniqueIndexKeyLengths(t *testing.T) {
	dsn := mysqlTestDSN(t)
	requireMigrated(t, dsn)
	db := openRaw(t, dsn)

	rows, err := db.Query(`SELECT s.table_name, s.index_name, s.seq_in_index,
			c.column_name, c.data_type, c.character_maximum_length
		FROM information_schema.statistics s
		JOIN information_schema.columns c
		  ON c.table_schema = s.table_schema AND c.table_name = s.table_name AND c.column_name = s.column_name
		WHERE s.table_schema = DATABASE() AND s.non_unique = 0
		ORDER BY s.table_name, s.index_name, s.seq_in_index`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	type indexKey struct {
		table, index string
		cols         []string
		bytes        int64
	}
	indexes := map[string]*indexKey{}
	order := make([]string, 0)
	for rows.Next() {
		var table, index, column, dataType string
		var seq int
		var maxLen sql.NullInt64
		require.NoError(t, rows.Scan(&table, &index, &seq, &column, &dataType, &maxLen))
		id := table + "." + index
		entry, ok := indexes[id]
		if !ok {
			entry = &indexKey{table: table, index: index}
			indexes[id] = entry
			order = append(order, id)
		}
		entry.cols = append(entry.cols, fmt.Sprintf("%s(%s)", column, dataType))
		entry.bytes += columnKeyBytes(dataType, maxLen)
	}
	require.NoError(t, rows.Err())

	sort.Strings(order)
	multiVarchar := 0
	for _, id := range order {
		entry := indexes[id]
		t.Logf("unique index %-40s %5d bytes  cols=[%s]", id, entry.bytes, strings.Join(entry.cols, ", "))
		require.LessOrEqual(t, entry.bytes, int64(3072),
			"unique index %s exceeds InnoDB 3072-byte limit", id)
		varcharCols := 0
		for _, col := range entry.cols {
			if strings.Contains(col, "(varchar)") {
				varcharCols++
			}
		}
		if varcharCols >= 2 {
			multiVarchar++
		}
	}
	require.NotZero(t, multiVarchar, "至少应存在一个含多个 VARCHAR 列的唯一索引，用例才有意义")

	var pageSize int64
	require.NoError(t, db.QueryRow(`SELECT @@innodb_page_size`).Scan(&pageSize))
	t.Logf("innodb_page_size=%d, utf8mb4 估算 4 bytes/char, InnoDB 索引上限 3072 bytes", pageSize)
}

// columnKeyBytes 按 InnoDB 索引键长度口径估算单列占用（utf8mb4 = 4 bytes/char）。
func columnKeyBytes(dataType string, maxLen sql.NullInt64) int64 {
	switch strings.ToLower(dataType) {
	case "bigint":
		return 8
	case "int", "integer":
		return 4
	case "smallint":
		return 2
	case "mediumint":
		return 3
	case "tinyint":
		return 1
	case "char", "varchar":
		if maxLen.Valid {
			return maxLen.Int64 * 4
		}
		return 0
	default:
		// text/blob 不能直接作为完整索引列；schema 中不应出现。
		return 0
	}
}

// TestBackendUsesSinglePool 覆盖需求 7：MySQL 读写同池，不含 SQLite 双池/PRAGMA/BEGIN IMMEDIATE 痕迹。
func TestBackendUsesSinglePool(t *testing.T) {
	dsn := mysqlTestDSN(t)
	requireMigrated(t, dsn)
	store := openStore(t, dsn)

	require.Equal(t, "mysql", store.Backend())
	require.True(t, store.DB() == store.ReadDB(), "MySQL 必须读写共用同一个连接池")
	require.Equal(t, 10, store.DB().Stats().MaxOpenConnections, "MySQL 使用普通连接池（tunePool=10），不是写池 1")
	require.Equal(t, " FOR UPDATE", store.Statements().LockCommandSerialRow())

	for _, migration := range Plan().Migrations() {
		for _, stmt := range migration.Stmts {
			upper := strings.ToUpper(stmt)
			for _, forbidden := range []string{"BEGIN IMMEDIATE", "QUERY_ONLY", "JOURNAL_MODE", "PRAGMA", "_TXCLOCK"} {
				require.NotContains(t, upper, forbidden,
					"MySQL 迁移语句不应包含 SQLite 特有内容 %q: %s", forbidden, stmt)
			}
		}
	}
	t.Logf("mysql pool: maxOpen=%d, readDB==db=true, lockClause=%q",
		store.DB().Stats().MaxOpenConnections, store.Statements().LockCommandSerialRow())
}

// openStore 打开一个 MySQL 后端并在用例结束时关闭。
func openStore(t *testing.T, dsn string) *Backend {
	t.Helper()
	store, err := Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// listTables 返回当前库全部表名（升序）。
func listTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT table_name FROM information_schema.tables
		WHERE table_schema = DATABASE() ORDER BY table_name`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	return tables
}

// insertOutboxRow 直接写入一条 outbox 记录，用于构造播种与回绕场景。
func insertOutboxRow(t *testing.T, db *sql.DB, commandID, phone string, msgID, serial uint16, status repository.CommandStatus) {
	t.Helper()
	now := time.Now().UnixMilli()
	_, err := db.Exec(`INSERT INTO command_outbox
		(command_id, idempotency_key, phone, msg_id, serial, payload, status, attempts, last_error,
		 created_at, updated_at, last_attempt_at, next_attempt_at, expires_at, lease_owner, lease_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, '', ?, ?, 0, ?, 0, '', 0)`,
		commandID, commandID, phone, int64(msgID), int64(serial), []byte{1}, string(status),
		now, now, now)
	require.NoError(t, err)
}
