package sqlcore

// Statements 是各 SQL 后端必须显式提供的方言语句集。
//
// 方法集合只覆盖 SQLite 与 MySQL 语法确实不同的语句；可移植的 SELECT/UPDATE
// 直接写在仓储实现里。新增方言差异时必须在此扩展方法，禁止在仓储实现里
// 用 `if backend == ...` 抹平。
type Statements interface {
	// SchemaMigrationsTableExistsQuery 返回判断 schema_migrations 是否存在的方言查询，
	// 结果集为单行整数计数。SQLite 查 sqlite_master，MySQL 查 information_schema。
	SchemaMigrationsTableExistsQuery() string
	// UpsertDevice 按 phone 整行覆盖设备静态档案。
	UpsertDevice() string
	// UpsertDeviceRegistration 只覆盖 0x0100 拥有的静态列，冲突时不触碰 imei/software_version。
	UpsertDeviceRegistration() string
	// UpsertDeviceParams 按 phone 覆盖终端参数集合。
	UpsertDeviceParams() string
	// UpsertUpgradeResult 按 (phone, 流水号) 覆盖升级结果。
	UpsertUpgradeResult() string
	// InsertCommandIgnoringDuplicate 在 (phone, idempotency_key) 冲突时静默忽略，
	// 由调用方按受影响行数区分首次入队与幂等返回。
	InsertCommandIgnoringDuplicate() string
	// EnsureCommandSerialRow 插入缺失的流水号游标行；行已存在时必须取得该行的排他锁，
	// 供随后的 SELECT ... FOR UPDATE 串行化分配。
	// 不能用“冲突时静默忽略”：重复键检查只加共享锁，后续锁升级会并发死锁。
	EnsureCommandSerialRow() string
	// LockCommandSerialRow 返回读取流水号游标的加锁子句。
	// SQLite 是单写者模型，返回空串；MySQL 返回 " FOR UPDATE"。
	LockCommandSerialRow() string
	// UpsertTerminalProperties 按 phone 覆盖终端属性档案。
	UpsertTerminalProperties() string
	// UpsertMediaResourceList 按 phone 覆盖 0x1205 资源列表快照。
	UpsertMediaResourceList() string
	// UpsertMediaUploadTask 按 (phone, 流水号) 覆盖 0x9206 上传任务。
	UpsertMediaUploadTask() string
	// UpsertMediaUploadNotice 按 (phone, 流水号) 覆盖 0x1206 上传完成通知。
	UpsertMediaUploadNotice() string
	// UpsertMediaFile 按 (phone, media_id) 覆盖 0x0801 文件元数据。
	UpsertMediaFile() string
	// UpsertMediaCapture 按 (phone, record_id) 覆盖 0x0805 拍摄应答。
	UpsertMediaCapture() string
	// UpsertAttachmentFile 按 (source, phone, alarm_no, file_name) 覆盖附件接收进度。
	UpsertAttachmentFile() string
	// InsertLocationsIgnoringDuplicates 生成 (phone, dedup_key) 冲突时忽略的批量插入。
	// placeholders 是调用方拼好的 "(?, ?, ...)" 列表，不含 VALUES 关键字。
	InsertLocationsIgnoringDuplicates(placeholders string) string
	// EnsureTaskLeaseRow 插入缺失的任务租约行；行已存在时静默忽略。
	// 参数为 (name, updated_at)，owner 为空、expires_at 为 0。
	EnsureTaskLeaseRow() string
	// EnsureDedupKeyRow 插入缺失的共享去重行；行已存在时静默忽略。
	// 参数为 (dedup_key, updated_at)，expires_at 为 0。
	EnsureDedupKeyRow() string
}
