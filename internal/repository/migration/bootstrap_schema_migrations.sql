-- 迁移元数据表：由迁移工具在执行版本化 SQL 前首次创建。
-- 它必须先于任何版本存在（否则无法记录已应用的版本），因此单独作为引导脚本，
-- 不参与版本编号。服务启动只读取该表，不创建它。
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at BIGINT NOT NULL
);
