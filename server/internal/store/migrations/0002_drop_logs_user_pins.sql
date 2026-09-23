-- 0002：移除审计日志、引入用户置顶。
--
-- 审计日志功能已整体下线（界面、接口、写入、裁剪全部移除），历史数据一并清除，
-- 因此这里直接 DROP 表而不是保留空表 —— 留一张永远不写的表只会让人误以为功能还在。
DROP TABLE IF EXISTS op_logs;

-- 用户置顶：每个账号各自维护自己关注的人员顺序。
-- 之所以按 (owner, target) 建表而不是在 users 上加一个全局标记位：
-- 置顶是"谁的界面"这件事，不同人看到的顺序可以不同。
CREATE TABLE IF NOT EXISTS user_pins (
  owner_user_id  BIGINT UNSIGNED NOT NULL COMMENT '执行置顶的账号',
  target_user_id BIGINT UNSIGNED NOT NULL COMMENT '被置顶的人员目录',
  created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (owner_user_id, target_user_id),
  KEY idx_user_pins_owner (owner_user_id),
  CONSTRAINT fk_user_pins_owner  FOREIGN KEY (owner_user_id)  REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_user_pins_target FOREIGN KEY (target_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci COMMENT '用户置顶';
