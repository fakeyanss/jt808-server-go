package sqlcore

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const mediaNodeColumns = `node_id, instance_id, internal_url, push_ip, push_tcp_port, push_udp_port,
	playback_base_url, endpoint_capacity, endpoints_used, max_bitrate_kbps, inbound_bitrate_kbps,
	active_channels, active_tcp_conns, cpu_percent, memory_bytes, status,
	started_at, last_seen_at, expires_at`

type mediaNodeRepository struct {
	store *Store
}

// MediaNodeRepository 返回媒体节点注册表。
func (s *Store) MediaNodeRepository() repository.MediaNodeRepository {
	return &mediaNodeRepository{store: s}
}

func (r *mediaNodeRepository) Register(ctx context.Context, node *repository.MediaNode, now time.Time) error {
	if node == nil || node.NodeID == "" || node.InstanceID == "" {
		return fmt.Errorf("register media node: node id and instance id are required")
	}
	node.StartedAt = now
	node.LastSeenAt = now
	node.ExpiresAt = now.Add(repository.MediaNodeTTL)
	node.Status = "online"
	// 新实例注册意味着旧进程的会话已经消失，端点占用从零重新计数。
	node.EndpointsUsed = 0
	_, err := r.store.db.ExecContext(ctx, r.store.stmts.UpsertMediaNode(),
		node.NodeID, node.InstanceID, node.InternalURL, node.PushIP,
		int64(node.PushTCPPort), int64(node.PushUDPPort), node.PlaybackBaseURL,
		node.EndpointCapacity, node.EndpointsUsed, node.MaxBitrateKbps, node.InboundBitrateKbps,
		node.ActiveChannels, node.ActiveTCPConns, node.CPUPercent, node.MemoryBytes,
		node.Status, node.StartedAt.UnixMilli(), node.LastSeenAt.UnixMilli(), node.ExpiresAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("register media node %s: %w", node.NodeID, err)
	}
	return nil
}

func (r *mediaNodeRepository) Heartbeat(ctx context.Context, nodeID, instanceID string, load repository.MediaNodeLoad, now time.Time) (bool, error) {
	nowMilli := now.UnixMilli()
	expiresAt := now.Add(repository.MediaNodeTTL).UnixMilli()
	res, err := r.store.db.ExecContext(ctx,
		`UPDATE media_nodes SET last_seen_at = ?, expires_at = ?, status = 'online',
			inbound_bitrate_kbps = ?, active_channels = ?, active_tcp_conns = ?,
			cpu_percent = ?, memory_bytes = ?
		 WHERE node_id = ? AND instance_id = ?`,
		nowMilli, expiresAt, load.InboundBitrateKbps, load.ActiveChannels, load.ActiveTCPConns,
		load.CPUPercent, load.MemoryBytes, nodeID, instanceID)
	if err != nil {
		return false, fmt.Errorf("heartbeat media node %s: %w", nodeID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 {
		return true, nil
	}
	// 写入与更新落在同一毫秒时 MySQL 可能返回 0 行受影响，用一次存在性查询兜底。
	var exists int
	err = r.store.db.QueryRowContext(ctx,
		`SELECT 1 FROM media_nodes WHERE node_id = ? AND instance_id = ?`, nodeID, instanceID).Scan(&exists)
	if err == stdsql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *mediaNodeRepository) Deregister(ctx context.Context, nodeID, instanceID string, now time.Time) error {
	_, err := r.store.db.ExecContext(ctx,
		`UPDATE media_nodes SET status = 'offline', expires_at = ?
		 WHERE node_id = ? AND instance_id = ?`, now.UnixMilli(), nodeID, instanceID)
	if err != nil {
		return fmt.Errorf("deregister media node %s: %w", nodeID, err)
	}
	return nil
}

func (r *mediaNodeRepository) Get(ctx context.Context, nodeID string) (*repository.MediaNode, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	row := r.store.readDB.QueryRowContext(ctx,
		`SELECT `+mediaNodeColumns+` FROM media_nodes WHERE node_id = ?`, nodeID)
	node, err := scanMediaNode(row)
	if err == stdsql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get media node %s: %w", nodeID, err)
	}
	return node, nil
}

func (r *mediaNodeRepository) ListOnline(ctx context.Context, now time.Time) ([]*repository.MediaNode, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	rows, err := r.store.readDB.QueryContext(ctx,
		`SELECT `+mediaNodeColumns+` FROM media_nodes
		 WHERE status = 'online' AND expires_at > ? ORDER BY node_id`, now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("list media nodes: %w", err)
	}
	defer rows.Close()
	out := make([]*repository.MediaNode, 0)
	for rows.Next() {
		node, scanErr := scanMediaNode(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func (r *mediaNodeRepository) Reserve(ctx context.Context, nodeID, instanceID string, slots int64, now time.Time) (bool, error) {
	if slots <= 0 {
		slots = 1
	}
	// 条件更新即占位：额度不足、节点过期或实例已被顶替时不会写入任何行。
	res, err := r.store.db.ExecContext(ctx,
		`UPDATE media_nodes SET endpoints_used = endpoints_used + ?
		 WHERE node_id = ? AND instance_id = ? AND status = 'online' AND expires_at > ?
		   AND (endpoint_capacity <= 0 OR endpoints_used + ? <= endpoint_capacity)`,
		slots, nodeID, instanceID, now.UnixMilli(), slots)
	if err != nil {
		return false, fmt.Errorf("reserve media endpoint on %s: %w", nodeID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *mediaNodeRepository) Release(ctx context.Context, nodeID, instanceID string, slots int64, now time.Time) error {
	if slots <= 0 {
		slots = 1
	}
	_, err := r.store.db.ExecContext(ctx,
		`UPDATE media_nodes SET endpoints_used =
			CASE WHEN endpoints_used > ? THEN endpoints_used - ? ELSE 0 END
		 WHERE node_id = ? AND instance_id = ?`,
		slots, slots, nodeID, instanceID)
	if err != nil {
		return fmt.Errorf("release media endpoint on %s: %w", nodeID, err)
	}
	return nil
}

func (r *mediaNodeRepository) Stats(ctx context.Context, now time.Time) (repository.MediaNodeStats, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	var stats repository.MediaNodeStats
	err := r.store.readDB.QueryRowContext(ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN status = 'online' AND expires_at > ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'offline' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'online' AND expires_at <= ? THEN 1 ELSE 0 END), 0)
		 FROM media_nodes`, now.UnixMilli(), now.UnixMilli()).
		Scan(&stats.Online, &stats.Offline, &stats.Expired)
	if err != nil {
		return repository.MediaNodeStats{}, fmt.Errorf("media node stats: %w", err)
	}
	return stats, nil
}

func scanMediaNode(row interface{ Scan(...any) error }) (*repository.MediaNode, error) {
	var (
		node       repository.MediaNode
		pushTCP    int64
		pushUDP    int64
		startedAt  int64
		lastSeenAt int64
		expiresAt  int64
	)
	if err := row.Scan(
		&node.NodeID, &node.InstanceID, &node.InternalURL, &node.PushIP, &pushTCP, &pushUDP,
		&node.PlaybackBaseURL, &node.EndpointCapacity, &node.EndpointsUsed, &node.MaxBitrateKbps,
		&node.InboundBitrateKbps, &node.ActiveChannels, &node.ActiveTCPConns,
		&node.CPUPercent, &node.MemoryBytes, &node.Status,
		&startedAt, &lastSeenAt, &expiresAt,
	); err != nil {
		return nil, err
	}
	node.PushTCPPort = uint16(pushTCP)
	node.PushUDPPort = uint16(pushUDP)
	node.StartedAt = timeFromUnixMilli(startedAt)
	node.LastSeenAt = timeFromUnixMilli(lastSeenAt)
	node.ExpiresAt = timeFromUnixMilli(expiresAt)
	return &node, nil
}

var _ repository.MediaNodeRepository = (*mediaNodeRepository)(nil)
