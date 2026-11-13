package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 写池必须硬编码为 1；只读池连接数按配置生效。
func TestOpenConfiguresDualPools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, path)

	store, err := Open(path, 2)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.Equal(t, 1, store.DB().Stats().MaxOpenConnections)
	require.Equal(t, 2, store.ReadDB().Stats().MaxOpenConnections)
}

// 连接级参数必须走 DSN，而不是对池里某个连接做 Exec。
func TestDSNDefinesConnectionLevelPragmas(t *testing.T) {
	write := writeDSN("/tmp/x.db")
	require.Contains(t, write, "_pragma=busy_timeout(5000)")
	require.Contains(t, write, "_pragma=journal_mode(WAL)")
	require.Contains(t, write, "_txlock=immediate")

	read := readDSN("/tmp/x.db")
	require.Contains(t, read, "mode=ro")
	require.Contains(t, read, "_pragma=query_only(1)")
	require.Contains(t, read, "_pragma=busy_timeout(5000)")
}

// 只读池由 DSN（mode=ro + query_only）强制只读，写入必须被 SQLite 拒绝。
// 逐个取出物理连接验证，确认不是碰巧只有一条连接只读。
func TestReadPoolRejectsWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, path)

	store, err := Open(path, 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	for i := 0; i < 4; i++ {
		conn, err := store.ReadDB().Conn(ctx)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx,
			`INSERT INTO devices (phone, id, plate, keepalive_ms, version_desc, protocol_version,
				auth_code, imei, software_version, updated_at) VALUES ('1', 'D', 'P', 1, 1, 1, 'A', '', '', 1)`)
		require.Error(t, err)
		require.Contains(t, err.Error(), "readonly")
		require.NoError(t, conn.Close())
	}
}

// WAL 下只读池不被写事务阻塞：写事务未提交时读查询仍能立即返回。
func TestReadPoolNotBlockedByWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, path)

	store, err := Open(path, 2)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	tx, err := store.DB().BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO devices (phone, id, plate, keepalive_ms, version_desc, protocol_version,
			auth_code, imei, software_version, updated_at) VALUES ('1', 'D', 'P', 1, 1, 1, 'A', '', '', 1)`)
	require.NoError(t, err)

	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var count int
	err = store.ReadDB().QueryRowContext(readCtx, `SELECT COUNT(*) FROM devices`).Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count, "read pool sees committed snapshot only")

	require.NoError(t, tx.Rollback())
}

// 并发写入必须串行成功，不允许出现 SQLITE_BUSY。
func TestConcurrentWritesSerialize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jt808.db")
	migrateSQLite(t, path)

	store, err := Open(path, 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			phone := fmt.Sprintf("188765%05d", index)
			errs <- store.DeviceRepository().Upsert(context.Background(), sampleDevice(phone))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	devices, err := store.DeviceRepository().List(context.Background())
	require.NoError(t, err)
	require.Len(t, devices, workers)
}
