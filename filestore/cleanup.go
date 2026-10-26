package filestore

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/storage"
)

// 清理任务的配置项（秒 / 条）
const (
	cleanupEnabledEnv     = "FILE_CLEANUP_ENABLED"
	cleanupIntervalEnv    = "FILE_CLEANUP_INTERVAL_SECONDS"
	cleanupBatchEnv       = "FILE_CLEANUP_BATCH_SIZE"
	cleanupGraceEnv       = "FILE_CLEANUP_GRACE_SECONDS"
	cleanupRetentionEnv   = "FILE_CLEANUP_RETENTION_SECONDS"
	cleanupRetryWindowEnv = "FILE_CLEANUP_RETRY_WINDOW_SECONDS"
)

const (
	// defaultCleanupInterval 两轮清理之间的间隔
	defaultCleanupInterval = 10 * time.Minute
	// defaultCleanupBatchSize 单个子任务每轮处理的记录上限
	defaultCleanupBatchSize = 1000
	// defaultCleanupGrace 过期后延迟多久才删对象，避开正在进行的读取代理
	defaultCleanupGrace = 15 * time.Minute
	// defaultCleanupRetention 对象删除后记录保留多久。
	// 期间读取接口继续返回 410，与 API 文档的承诺一致。
	defaultCleanupRetention = 7 * 24 * time.Hour
	// defaultCleanupRetryWindow 失败记录的最长重试窗口。
	// 应用停机超过这个时长，期间过期的对象会残留，需要靠日志与扫桶排查。
	defaultCleanupRetryWindow = 7 * 24 * time.Hour
	// defaultCleanupRoundTimeout 单轮总超时，防止存储变慢时一轮拖到下一轮
	defaultCleanupRoundTimeout = 5 * time.Minute

	// cleanupFailureLogLimit 单轮内逐条打印失败详情的上限，避免存储故障时刷屏
	cleanupFailureLogLimit = 5
)

// CleanupConfig 过期文件清理配置。
type CleanupConfig struct {
	// Enabled 是否启用清理
	Enabled bool
	// Interval 两轮之间的间隔
	Interval time.Duration
	// BatchSize 每轮最多处理的记录数（对象删除与记录回收各自计算）
	BatchSize int
	// Grace 过期后的宽限期
	Grace time.Duration
	// Retention 对象删除后记录的保留期
	Retention time.Duration
	// RetryWindow 删除失败后的最长重试窗口
	RetryWindow time.Duration
	// RoundTimeout 单轮总超时
	RoundTimeout time.Duration
}

// LoadCleanupConfig 从环境变量读取清理配置。
// 与 LoadConfig 一致：非法值按默认处理并打日志，不阻止服务启动。
func LoadCleanupConfig() CleanupConfig {
	return CleanupConfig{
		Enabled:      boolEnv(cleanupEnabledEnv, true),
		Interval:     durationEnv(cleanupIntervalEnv, defaultCleanupInterval),
		BatchSize:    intEnv(cleanupBatchEnv, defaultCleanupBatchSize),
		Grace:        durationEnv(cleanupGraceEnv, defaultCleanupGrace),
		Retention:    durationEnv(cleanupRetentionEnv, defaultCleanupRetention),
		RetryWindow:  durationEnv(cleanupRetryWindowEnv, defaultCleanupRetryWindow),
		RoundTimeout: defaultCleanupRoundTimeout,
	}
}

// Cleaner 过期文件清理任务。
//
// 只以 files 表为准：先删对象存储里的对象，再在保留期后删记录。
// 桶里额外列对象不会被处理（例如上传成功但进程在落库前退出留下的孤儿），
// 这类残留需要另做扫桶对账，不在本任务范围内。
type Cleaner struct {
	cfg CleanupConfig

	// 依赖可在同包测试中替换
	now         func() time.Time
	listPending func(now time.Time, grace, retryWindow time.Duration, limit int) ([]model.File, error)
	markDeleted func(id int, at time.Time) error
	purgeFiles  func(now time.Time, grace, retention time.Duration, limit int) (int64, error)
	newStore    func(serviceType, serviceAddress string) (storage.Store, error)
}

// NewCleaner 创建清理任务
func NewCleaner(cfg CleanupConfig) *Cleaner {
	// 防止直接调用方漏填：零值间隔会让 ticker panic，零值超时会让每轮立即超时。
	if cfg.Interval <= 0 {
		cfg.Interval = defaultCleanupInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultCleanupBatchSize
	}
	if cfg.Grace < 0 {
		cfg.Grace = defaultCleanupGrace
	}
	if cfg.Retention <= 0 {
		cfg.Retention = defaultCleanupRetention
	}
	if cfg.RetryWindow <= 0 {
		cfg.RetryWindow = defaultCleanupRetryWindow
	}
	if cfg.RoundTimeout <= 0 {
		cfg.RoundTimeout = defaultCleanupRoundTimeout
	}

	return &Cleaner{
		cfg:         cfg,
		now:         time.Now,
		listPending: model.ListFilesPendingObjectDeletion,
		markDeleted: model.MarkFileObjectDeleted,
		purgeFiles:  model.PurgeFiles,
		newStore:    storage.FromEnv,
	}
}

// RunCleaner 启动清理循环，直到 ctx 被取消。
func RunCleaner(ctx context.Context, cfg CleanupConfig) {
	NewCleaner(cfg).Run(ctx)
}

// Run 启动清理循环，直到 ctx 被取消。
// 与对账循环一样先跑一轮，把停机期间积压的文件接过来。
func (c *Cleaner) Run(ctx context.Context) {
	common.SysLogf("[FileCleaner] 过期文件清理循环已启动（间隔 %s，每轮上限 %d，宽限 %s，保留 %s，重试窗口 %s）",
		c.cfg.Interval, c.cfg.BatchSize, c.cfg.Grace, c.cfg.Retention, c.cfg.RetryWindow)

	c.cleanOnce(ctx)

	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			common.SysLogf("[FileCleaner] 过期文件清理循环已停止")
			return
		case <-ticker.C:
			c.cleanOnce(ctx)
		}
	}
}

// cleanOnce 执行一轮清理：删除过期对象，再回收保留期满的记录。
func (c *Cleaner) cleanOnce(parent context.Context) {
	start := c.now()
	ctx, cancel := context.WithTimeout(parent, c.cfg.RoundTimeout)
	defer cancel()

	var (
		listed        int
		deleted       int
		skipped       int
		failed        int
		storageBroken bool
		listErr       error
	)

	files, err := c.listPending(start, c.cfg.Grace, c.cfg.RetryWindow, c.cfg.BatchSize)
	if err != nil {
		listErr = err
		common.SysErrorf("[FileCleaner] 查询待删除对象失败: %v", err)
	} else {
		listed = len(files)
		for i, f := range files {
			if ctx.Err() != nil {
				common.SysErrorf("[FileCleaner] 本轮超时，剩余 %d 条留待下一轮", len(files)-i)
				break
			}

			skip, delErr := c.deleteObject(ctx, f)
			if delErr != nil {
				failed++
				if failed <= cleanupFailureLogLimit {
					common.SysErrorf("[FileCleaner] 删除对象失败: id=%d, key=%s, err=%v", f.ID, f.FileKey, delErr)
				}
				// 存储未配置时后面每一条都会以同样方式失败，没必要继续打日志
				if errors.Is(delErr, storage.ErrNotConfigured) {
					storageBroken = true
					break
				}
				continue
			}

			// 对象已删（或本来就没有可删的对象），打标记。
			// 标记失败时保持未标记状态，下一轮会重删对象——Delete 是幂等的。
			if err := c.markDeleted(f.ID, c.now()); err != nil {
				failed++
				if failed <= cleanupFailureLogLimit {
					common.SysErrorf("[FileCleaner] 标记对象已删除失败: id=%d, err=%v", f.ID, err)
				}
				continue
			}

			if skip {
				skipped++
			} else {
				deleted++
			}
		}
	}

	purged, purgeErr := c.purgeFiles(start, c.cfg.Grace, c.cfg.Retention, c.cfg.BatchSize)
	if purgeErr != nil {
		common.SysErrorf("[FileCleaner] 回收过期记录失败: %v", purgeErr)
	}

	// 空闲时不打日志，避免每轮都刷一行
	if listed > 0 || purged > 0 || listErr != nil || purgeErr != nil {
		common.SysLogf("[FileCleaner] 本轮完成：待删对象 %d，已删 %d，跳过 %d，失败 %d，回收记录 %d（耗时 %s）",
			listed, deleted, skipped, failed, purged, c.now().Sub(start))
	}
	if storageBroken {
		common.SysErrorf("[FileCleaner] 对象存储未配置，本轮剩余对象删除已跳过")
	}
}

// deleteObject 删除记录对应的对象。
//
// skip 为 true 表示这条记录没有可删除的对象（历史记录未登记存储信息、
// 或使用了本服务不认识的服务类型），调用方仍应标记它，
// 让记录在保留期后正常回收。
func (c *Cleaner) deleteObject(ctx context.Context, f model.File) (skip bool, err error) {
	serviceType := strings.TrimSpace(f.ServiceType)
	key := strings.TrimSpace(f.FileKey)
	if serviceType == "" || key == "" {
		return true, nil
	}

	store, err := c.newStore(serviceType, f.ServiceAddress)
	if err != nil {
		if errors.Is(err, storage.ErrNotSupported) {
			return true, nil
		}
		return false, err
	}

	if err := store.Delete(ctx, key); err != nil {
		return false, err
	}
	return false, nil
}

// durationEnv 读取以秒为单位的时长配置，非法值回落默认并打日志。
func durationEnv(name string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		common.SysErrorf("[FileCleaner] %s 配置无效(%q)，按默认 %s 处理", name, raw, def)
		return def
	}
	return time.Duration(seconds) * time.Second
}

// intEnv 读取正整数配置，非法值回落默认并打日志。
func intEnv(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		common.SysErrorf("[FileCleaner] %s 配置无效(%q)，按默认 %d 处理", name, raw, def)
		return def
	}
	return value
}

// boolEnv 读取布尔配置，接受 1/0、true/false、yes/no、on/off。
func boolEnv(name string, def bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch raw {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		common.SysErrorf("[FileCleaner] %s 配置无效(%q)，按默认 %v 处理", name, raw, def)
		return def
	}
}
