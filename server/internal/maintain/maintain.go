// Package maintain 实现生命周期维护任务：到期标记、物理清理、僵尸上传会话、
// 孤儿文件归档。
//
// 所有任务都先尝试获取数据库命名锁（GET_LOCK），因此多副本部署时同一时刻
// 只有一个实例真正执行，其余实例直接跳过。
package maintain

import (
	"context"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"lan-drive/internal/settings"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
)

// 锁名与任务周期。
const (
	lockName         = "lanfs_maintain"
	lockWaitSeconds  = 0
	defaultBatchSize = 500
	orphanDirRel     = "tmp/orphans"
	failedDirRel     = "tmp/failed"
)

// Service 维护任务集合。
type Service struct {
	store     *store.Store
	st        *storage.Storage
	set       *settings.Service
	batchSize int
}

// New 构造维护服务。
func New(s *store.Store, st *storage.Storage, set *settings.Service) *Service {
	return &Service{store: s, st: st, set: set, batchSize: defaultBatchSize}
}

// RunDaily 执行每日任务：孤儿扫描。
func (s *Service) RunDaily(ctx context.Context) {
	s.withLock(ctx, "daily", func(ctx context.Context) {
		s.ScanOrphans(ctx)
	})
}

// RunExpire 执行到期标记：active 且 expires_at <= now → trashed。
// 返回本次标记的文件数。
func (s *Service) RunExpire(ctx context.Context) int {
	n := 0
	s.withLock(ctx, "expire", func(ctx context.Context) {
		cfg := s.set.Get()
		now := time.Now().UTC().Truncate(time.Second)
		expired, err := s.store.ExpireFiles(ctx, now, cfg.TrashDays)
		if err != nil {
			slog.Error("到期标记失败", "error", err)
			return
		}
		n = len(expired)
		if n > 0 {
			slog.Info("到期文件已标记删除", "count", n, "trash_days", cfg.TrashDays)
		}
	})
	return n
}

// RunPurge 执行物理清理：trashed 且 purge_at <= now → 删磁盘文件 + 删数据库记录。
// 返回本次彻底删除的文件数。
func (s *Service) RunPurge(ctx context.Context) int {
	n := 0
	s.withLock(ctx, "purge", func(ctx context.Context) {
		now := time.Now().UTC().Truncate(time.Second)
		list, err := s.store.ListPurgeable(ctx, now, s.batchSize)
		if err != nil {
			slog.Error("查询待清理文件失败", "error", err)
			return
		}
		for _, f := range list {
			// 先删磁盘再删记录：删除失败则保留记录，下轮重试，
			// 避免出现「记录没了但文件还在」的幽灵文件。
			if err := s.st.Remove(f.RelPath); err != nil {
				slog.Error("删除磁盘文件失败，保留记录待重试", "file_id", f.ID, "rel_path", f.RelPath, "error", err)
				continue
			}
			if err := s.store.DeleteFileRow(ctx, f.ID); err != nil {
				slog.Error("删除文件记录失败", "file_id", f.ID, "error", err)
				continue
			}
			s.pruneUserDir(f.RelPath)
			n++
		}
		if n > 0 {
			slog.Info("回收站文件已彻底删除", "count", n)
		}
	})
	return n
}

// RunCleanStaleUploads 清理僵尸上传会话与已结束会话。
// 未活动超过 24 小时的 uploading 会话连分片一起删除。
func (s *Service) RunCleanStaleUploads(ctx context.Context) int {
	n := 0
	s.withLock(ctx, "uploads", func(ctx context.Context) {
		now := time.Now().UTC()
		stale, err := s.store.ListStaleUploads(ctx, now.Add(-24*time.Hour), s.batchSize)
		if err != nil {
			slog.Error("查询僵尸上传会话失败", "error", err)
			return
		}
		for _, u := range stale {
			if err := s.st.RemoveAll(storage.ChunkDirRel(u.ID)); err != nil {
				slog.Error("删除分片目录失败", "upload_id", u.ID, "error", err)
				continue
			}
			if err := s.store.DeleteUploadSession(ctx, u.ID); err != nil {
				slog.Error("删除上传会话失败", "upload_id", u.ID, "error", err)
				continue
			}
			n++
		}
		// 已结束的会话保留 24 小时用于 complete 幂等返回与排障，之后回收会话行。
		finished, err := s.store.ListFinishedSessions(ctx, now.Add(-24*time.Hour), s.batchSize)
		if err != nil {
			slog.Error("查询已结束上传会话失败", "error", err)
			return
		}
		for _, u := range finished {
			if err := s.store.DeleteUploadSession(ctx, u.ID); err != nil {
				slog.Error("回收上传会话失败", "upload_id", u.ID, "error", err)
				continue
			}
			n++
		}
		if n > 0 {
			slog.Info("上传会话已清理", "count", n)
		}
	})
	return n
}

// OrphanReport 描述一次一致性扫描的结果。
type OrphanReport struct {
	Orphans   []string  `json:"orphans"` // 磁盘有、数据库无
	Missing   []string  `json:"missing"` // 数据库有、磁盘无
	Invalid   []string  `json:"invalid"` // 数据库中存在非法相对路径
	ScannedAt time.Time `json:"scanned_at"`
	Total     int       `json:"total_files_on_disk"`
}

// ScanStorage 做只读一致性扫描（管理端可手动触发，不做任何修改）。
func (s *Service) ScanStorage(ctx context.Context) (*OrphanReport, error) {
	rep := &OrphanReport{ScannedAt: time.Now().UTC(), Orphans: []string{}, Missing: []string{}, Invalid: []string{}}

	known, err := s.store.AllRelPaths(ctx)
	if err != nil {
		return nil, err
	}
	// 分片文件由 upload_chunks 管理，是合法存在，不能算孤儿。
	chunkPaths, err := s.store.AllChunkRelPaths(ctx)
	if err != nil {
		return nil, err
	}

	// 数据库里路径非法或磁盘缺失。
	for rel := range known {
		if _, err := storage.SafeRel(rel); err != nil {
			rep.Invalid = append(rep.Invalid, rel)
			continue
		}
		if !s.st.Exists(rel) {
			rep.Missing = append(rep.Missing, rel)
		}
	}

	// 磁盘上的孤儿文件（只扫 users/ 与 tmp/，跳过归档目录）。
	for _, prefix := range []string{"users", "tmp"} {
		err := s.st.WalkFiles(prefix, func(rel string, _ os.FileInfo) error {
			rep.Total++
			if strings.HasPrefix(rel, orphanDirRel+"/") || strings.HasPrefix(rel, failedDirRel+"/") {
				return nil
			}
			if strings.Contains(rel, ".merging") || strings.HasSuffix(rel, ".tmp") {
				return nil // 合并中间态，不算孤儿
			}
			if _, ok := known[rel]; ok {
				return nil
			}
			if _, ok := chunkPaths[rel]; ok {
				return nil // 正在进行的上传分片
			}
			rep.Orphans = append(rep.Orphans, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return rep, nil
}

// ScanOrphans 每日扫描并把孤儿文件移入 tmp/orphans（不直接删除，保留 7 天）。
func (s *Service) ScanOrphans(ctx context.Context) int {
	moved := 0
	known, err := s.store.AllRelPaths(ctx)
	if err != nil {
		slog.Error("读取文件路径失败", "error", err)
		return 0
	}
	for _, prefix := range []string{"users"} {
		err := s.st.WalkFiles(prefix, func(rel string, _ os.FileInfo) error {
			if _, ok := known[rel]; ok {
				return nil
			}
			if strings.Contains(rel, ".merging") || strings.HasSuffix(rel, ".tmp") {
				return nil
			}
			// 归档路径保留原相对结构，便于管理员核对。
			target := path.Join(orphanDirRel, time.Now().UTC().Format("20060102"), rel)
			if err := s.st.Move(rel, target); err != nil {
				slog.Error("归档孤儿文件失败", "rel_path", rel, "error", err)
				return nil
			}
			moved++
			return nil
		})
		if err != nil {
			slog.Error("孤儿文件扫描失败", "error", err)
		}
	}
	if moved > 0 {
		slog.Info("孤儿文件已归档", "count", moved)
	}
	// 清理过期的归档与合并残留。
	s.cleanupArchive(ctx)
	return moved
}

// cleanupArchive 删除归档超过 7 天的孤儿文件与超过 1 天的分片/合并残留。
func (s *Service) cleanupArchive(ctx context.Context) {
	now := time.Now().UTC()
	_ = s.st.WalkFiles(orphanDirRel, func(rel string, info os.FileInfo) error {
		if now.Sub(info.ModTime()) > 7*24*time.Hour {
			_ = s.st.Remove(rel)
		}
		return nil
	})
	_ = s.st.WalkFiles("tmp/chunks", func(rel string, info os.FileInfo) error {
		if now.Sub(info.ModTime()) > 48*time.Hour {
			_ = s.st.Remove(rel)
		}
		return nil
	})
}

// CleanupUserDir 删除用户目录（删除账号时调用）。
//
// 接相对路径而不是 userID：存量数据的目录可能是早期的 users/<id>，
// 直接用数据库里记录的值才能又准又安全地删对地方。
func (s *Service) CleanupUserDir(dirRel string) error {
	if dirRel == "" {
		// 早期数据可能没写 dir_rel；此时按工号推算无处可依，直接跳过。
		return nil
	}
	return s.st.RemoveAll(dirRel)
}

// pruneUserDir 删除文件后清理其所在的空用户目录。
func (s *Service) pruneUserDir(relPath string) {
	idx := strings.LastIndex(relPath, "/")
	if idx <= 0 {
		return
	}
	dir := relPath[:idx]
	if !strings.HasPrefix(dir, "users/") || strings.Count(dir, "/") != 1 {
		return
	}
	_ = s.st.PruneEmptyDirs(dir)
}

// withLock 在数据库命名锁保护下执行任务；未取到锁说明其他实例正在执行，直接跳过。
func (s *Service) withLock(ctx context.Context, task string, fn func(context.Context)) {
	ok, release, err := s.store.TryLock(ctx, lockName, lockWaitSeconds)
	if err != nil {
		slog.Error("获取维护锁失败", "task", task, "error", err)
		return
	}
	if !ok {
		slog.Debug("其他实例正在执行维护任务，跳过", "task", task)
		return
	}
	defer release()
	fn(ctx)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
