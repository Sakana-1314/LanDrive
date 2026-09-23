-- 0001_init.sql —— 局域网文件助手 初始 schema
--
-- 约定：
--   * 字符集统一 utf8mb4 / utf8mb4_unicode_ci
--   * 所有时间列存 UTC（DSN 固定 parseTime=true&loc=UTC）
--   * 只使用 MySQL 8.0 与 MariaDB 11 都支持的标准 DDL，
--     不使用 JSON 列 / 函数索引 / CHECK 约束 / CTE 写入
--   * 磁盘相对路径全部以 '/' 分隔，且都相对 LANDRIVE_DATA_DIR

CREATE TABLE IF NOT EXISTS users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  employee_no VARCHAR(32) NOT NULL COMMENT '工号，登录名，创建后不可修改',
  name VARCHAR(64) NOT NULL COMMENT '姓名，仅展示，允许重名',
  password_hash VARCHAR(100) NOT NULL COMMENT 'bcrypt',
  role VARCHAR(16) NOT NULL DEFAULT 'user' COMMENT 'admin / user',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  dir_rel VARCHAR(255) NOT NULL COMMENT '用户目录相对数据根目录的路径',
  last_login_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_employee_no (employee_no),
  KEY idx_users_role (role)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci COMMENT '账号';

CREATE TABLE IF NOT EXISTS files (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_id BIGINT UNSIGNED NOT NULL,
  original_name VARCHAR(255) NOT NULL COMMENT '原始文件名，仅存数据库',
  ext VARCHAR(32) NOT NULL DEFAULT '' COMMENT '规范化扩展名，小写带点，无扩展名为空串',
  size_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
  mime VARCHAR(128) NOT NULL DEFAULT 'application/octet-stream',
  sha256 CHAR(64) NOT NULL DEFAULT '',
  rel_path VARCHAR(512) NOT NULL COMMENT '相对数据根目录的路径，磁盘文件名 = id + ext',
  status VARCHAR(16) NOT NULL DEFAULT 'active' COMMENT 'active / trashed',
  expires_at DATETIME NOT NULL COMMENT '到期标记删除的时间点',
  deleted_at DATETIME NULL COMMENT '进入回收站的时刻',
  purge_at DATETIME NULL COMMENT '物理删除的时刻',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_files_rel_path (rel_path),
  KEY idx_files_owner_status (owner_id, status),
  KEY idx_files_status_expires (status, expires_at),
  KEY idx_files_purge (purge_at),
  KEY idx_files_ext (ext),
  KEY idx_files_created (created_at),
  CONSTRAINT fk_files_owner FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE RESTRICT
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci COMMENT '文件';

CREATE TABLE IF NOT EXISTS upload_sessions (
  id CHAR(32) NOT NULL COMMENT 'uuid hex（无连字符）',
  owner_id BIGINT UNSIGNED NOT NULL,
  original_name VARCHAR(255) NOT NULL,
  ext VARCHAR(32) NOT NULL DEFAULT '',
  size_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
  chunk_size INT UNSIGNED NOT NULL DEFAULT 0,
  total_chunks INT UNSIGNED NOT NULL DEFAULT 0,
  received_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
  status VARCHAR(16) NOT NULL DEFAULT 'uploading' COMMENT 'uploading / done / aborted',
  dir_rel VARCHAR(255) NOT NULL COMMENT '目标目录（当前即属主目录）',
  sha256 CHAR(64) NOT NULL DEFAULT '' COMMENT '客户端声明的整文件校验值，可空',
  file_id BIGINT UNSIGNED NULL COMMENT '合并成功后生成的文件 ID，用于 complete 幂等返回',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_upload_owner_status (owner_id, status),
  KEY idx_upload_updated (updated_at),
  CONSTRAINT fk_upload_owner FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci COMMENT '分片上传会话';

CREATE TABLE IF NOT EXISTS upload_chunks (
  upload_id CHAR(32) NOT NULL,
  idx INT UNSIGNED NOT NULL COMMENT '分片序号，从 0 开始',
  size_bytes INT UNSIGNED NOT NULL DEFAULT 0,
  rel_path VARCHAR(512) NOT NULL COMMENT '分片文件相对数据根目录的路径',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (upload_id, idx),
  CONSTRAINT fk_chunk_upload FOREIGN KEY (upload_id) REFERENCES upload_sessions (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci COMMENT '上传分片';

CREATE TABLE IF NOT EXISTS settings (
  `key` VARCHAR(64) NOT NULL,
  `value` TEXT NOT NULL,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`key`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci COMMENT '系统配置';

CREATE TABLE IF NOT EXISTS op_logs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NULL,
  employee_no VARCHAR(32) NOT NULL DEFAULT '',
  action VARCHAR(32) NOT NULL,
  target_type VARCHAR(32) NOT NULL DEFAULT '',
  target_id VARCHAR(64) NOT NULL DEFAULT '',
  detail VARCHAR(512) NOT NULL DEFAULT '',
  ip VARCHAR(45) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_logs_created (created_at),
  KEY idx_logs_action (action),
  KEY idx_logs_user (user_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci COMMENT '操作日志';
