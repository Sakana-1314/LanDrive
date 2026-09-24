-- 0003：文件夹（每人一棵树）+ 分享关系。
--
-- 两个设计要点先说清楚：
--
-- 1) files.folder_id 用 NOT NULL DEFAULT 0（0 表示根目录），而不是可空列。
--    原因：MySQL/MariaDB 的唯一索引把多个 NULL 视为互不相同，若用可空列做
--    (folder_id, original_name) 唯一键，根目录下的重名文件根本拦不住。
--    代价是 folder_id 不能加外键（0 不是合法的 folders.id），
--    因此目录归属的完整性与级联清理由 internal/folders 服务在事务里保证，
--    并由一致性扫描兜底。这个取舍是有意的，不是漏写约束。
--    同理不用「生成列 + 唯一索引」：那在 MySQL 8 与 MariaDB 11 上的写法有差异，
--    也超出 go-mysql-server 的支持范围，会让本地验证失效。
--
-- 1b) 刻意**不加** (owner_id, folder_id, original_name) 唯一键。上传一直允许同名
--    （落盘名是 id+ext，不冲突），若在这里加唯一键，历史上真有同名文件的库会
--    在启动迁移时直接失败 —— 那是"服务起不来"级别的风险，远大于收益。
--    同名改由上传路径自动改名处理（"报表.xlsx" → "报表 (2).xlsx"），
--    既是用户预期的行为，也不需要迁移期去重。
--
-- 2) shares 指向的是**记录**（file_id / folder_id）而不是磁盘路径字符串。
--    于是「文件改名/移动后链接仍可用」（路径由记录解析）、
--    「文件被删除则提示已删除」（记录还在但状态是 trashed）、
--    「彻底清除后链接失效」（记录没了，靠外键 CASCADE 一并删掉分享）三者同时成立。

-- 目录：每人一棵树。path 是该目录相对"用户根目录"的规范化路径（如 "报表/2026"），
-- 不含工号前缀、不含首尾斜杠。磁盘上对应 users/<工号>/<path>。
CREATE TABLE IF NOT EXISTS folders (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_id   BIGINT UNSIGNED NOT NULL COMMENT '属主',
  parent_id  BIGINT UNSIGNED NULL COMMENT '上级目录，NULL 表示根目录下的一级目录',
  name       VARCHAR(255) NOT NULL COMMENT '目录名（单层，不含斜杠）',
  -- path 用**软删除**：删除目录时不删行，而是 status='deleted' 并把 path 追加
  -- ":<id>" 墓碑后缀。这样做的原因有两个：
  --   1. 分享指向目录记录。若直接删行，外键级联会把分享一起删掉，收链接的人
  --      只会看到"链接无效"，而真相是"文件夹已被删除" —— 需求要求区分这两者。
  --   2. uk_folders_owner_path 是 (owner_id, path) 唯一键。不追加后缀的话，
  --      删掉"报表"后就再也无法新建同名目录了。
  -- 后缀用 ':' 是安全的：SanitizeFolderName 会剥掉冒号，用户造不出含冒号的路径。
  path       VARCHAR(512) NOT NULL COMMENT '相对用户根目录的路径，如 报表/2026',
  status     VARCHAR(16) NOT NULL DEFAULT 'active' COMMENT 'active / deleted',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  -- 同一个人的同一路径只能有一个目录（改名的级联更新必须保持这条成立）
  UNIQUE KEY uk_folders_owner_path (owner_id, path),
  KEY idx_folders_owner_parent (owner_id, parent_id),
  KEY idx_folders_owner_status (owner_id, status),
  CONSTRAINT fk_folders_owner  FOREIGN KEY (owner_id)  REFERENCES users (id)   ON DELETE CASCADE,
  CONSTRAINT fk_folders_parent FOREIGN KEY (parent_id) REFERENCES folders (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci COMMENT '文件夹';

-- 文件归属到目录。存量文件 folder_id = 0（根目录），行为与改动前完全一致。
ALTER TABLE files
  ADD COLUMN folder_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '所属文件夹，0 表示根目录';

-- 索引拆成独立语句：一个 ALTER 带多个子句在部分 MySQL 兼容实现上不支持。
ALTER TABLE files ADD KEY idx_files_owner_folder (owner_id, folder_id, status);

-- 上传会话要记住目标文件夹：init 时确定，complete 时据此写入 files.folder_id。
-- 不在 complete 时重新问客户端，是因为那会给"上传到 A、完成时改口指向 B"留缝隙。
ALTER TABLE upload_sessions
  ADD COLUMN folder_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '目标文件夹，0=根目录';

-- 分享链接。
-- token 是 URL 凭证，用 crypto/rand 生成 32 位十六进制，不可预测；
-- 免登录接口只认 token，不接受任何 id/path 参数（避免越权枚举）。
CREATE TABLE IF NOT EXISTS shares (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  token       CHAR(32) NOT NULL COMMENT 'URL 凭证',
  owner_id    BIGINT UNSIGNED NOT NULL COMMENT '创建者',
  target_type VARCHAR(8) NOT NULL COMMENT 'file / folder',
  file_id     BIGINT UNSIGNED NULL COMMENT 'target_type=file 时指向文件',
  folder_id   BIGINT UNSIGNED NULL COMMENT 'target_type=folder 时指向目录',
  expire_days INT NULL COMMENT '有效期天数；NULL 表示永久',
  expires_at  DATETIME NULL COMMENT '到期时刻；NULL 表示永久',
  view_count  BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '被成功访问次数',
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_shares_token (token),
  KEY idx_shares_owner (owner_id),
  KEY idx_shares_file (file_id),
  KEY idx_shares_folder (folder_id),
  KEY idx_shares_expires (expires_at),
  CONSTRAINT fk_shares_owner  FOREIGN KEY (owner_id)  REFERENCES users   (id) ON DELETE CASCADE,
  CONSTRAINT fk_shares_file   FOREIGN KEY (file_id)   REFERENCES files   (id) ON DELETE CASCADE,
  CONSTRAINT fk_shares_folder FOREIGN KEY (folder_id) REFERENCES folders (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci COMMENT '分享链接';
