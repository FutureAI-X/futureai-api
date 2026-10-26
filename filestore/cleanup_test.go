package filestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FutureAI-X/futureai-api/model"
	"github.com/FutureAI-X/futureai-api/storage"
)

// newTestCleaner 构造一个依赖全部可替换的清理任务。
func newTestCleaner(store *fakeStore, files []model.File) (*Cleaner, *map[int]time.Time, *int64) {
	marked := map[int]time.Time{}
	var purgeCalls int64

	cleaner := &Cleaner{
		cfg: CleanupConfig{
			BatchSize:    10,
			Grace:        15 * time.Minute,
			Retention:    7 * 24 * time.Hour,
			RetryWindow:  7 * 24 * time.Hour,
			RoundTimeout: time.Minute,
		},
		now: func() time.Time { return testNow },
		listPending: func(time.Time, time.Duration, time.Duration, int) ([]model.File, error) {
			return files, nil
		},
		markDeleted: func(id int, at time.Time) error {
			marked[id] = at
			return nil
		},
		purgeFiles: func(time.Time, time.Duration, time.Duration, int) (int64, error) {
			purgeCalls++
			return 0, nil
		},
		newStore: func(serviceType, serviceAddress string) (storage.Store, error) {
			if serviceType == storage.ServiceTencentCOS {
				return store, nil
			}
			return nil, storage.ErrNotSupported
		},
	}
	return cleaner, &marked, &purgeCalls
}

func TestCleanerDeletesObjectsAndMarksRecords(t *testing.T) {
	store := &fakeStore{}
	files := []model.File{
		{ID: 1, ServiceType: storage.ServiceTencentCOS, ServiceAddress: "https://bucket.example.com", FileKey: "png/a.png"},
		{ID: 2}, // 历史记录：没有存储信息，没有可删对象
		{ID: 3, ServiceType: "OTHER", FileKey: "x/y"},
	}
	cleaner, marked, _ := newTestCleaner(store, files)

	cleaner.cleanOnce(context.Background())

	if len(store.deletes) != 1 || store.deletes[0] != "png/a.png" {
		t.Errorf("删除的对象 = %v, want [png/a.png]", store.deletes)
	}
	if len(*marked) != 3 {
		t.Errorf("标记数量 = %d, want 3: %v", len(*marked), *marked)
	}
	for id, at := range *marked {
		if !at.Equal(testNow) {
			t.Errorf("记录 %d 的标记时间 = %s, want %s", id, at, testNow)
		}
	}
}

func TestCleanerKeepsRecordWhenDeleteFails(t *testing.T) {
	store := &fakeStore{deleteErr: errors.New("boom")}
	files := []model.File{
		{ID: 1, ServiceType: storage.ServiceTencentCOS, FileKey: "png/a.png"},
	}
	cleaner, marked, _ := newTestCleaner(store, files)

	cleaner.cleanOnce(context.Background())

	if len(*marked) != 0 {
		t.Errorf("删除失败时不应标记记录: %v", *marked)
	}
}

func TestCleanerStopsWhenStorageNotConfigured(t *testing.T) {
	files := []model.File{
		{ID: 1, ServiceType: storage.ServiceTencentCOS, FileKey: "png/a.png"},
		{ID: 2, ServiceType: storage.ServiceTencentCOS, FileKey: "png/b.png"},
	}
	cleaner, marked, _ := newTestCleaner(&fakeStore{}, files)
	attempts := 0
	cleaner.newStore = func(string, string) (storage.Store, error) {
		attempts++
		return nil, storage.ErrNotConfigured
	}

	cleaner.cleanOnce(context.Background())

	if attempts != 1 {
		t.Errorf("存储未配置时应中止本轮，尝试次数 = %d, want 1", attempts)
	}
	if len(*marked) != 0 {
		t.Errorf("存储未配置时不应标记记录: %v", *marked)
	}
}

func TestLoadCleanupConfigDefaults(t *testing.T) {
	for _, name := range []string{
		cleanupEnabledEnv, cleanupIntervalEnv, cleanupBatchEnv,
		cleanupGraceEnv, cleanupRetentionEnv, cleanupRetryWindowEnv,
	} {
		t.Setenv(name, "")
	}

	cfg := LoadCleanupConfig()
	if !cfg.Enabled {
		t.Error("默认应启用清理")
	}
	if cfg.Interval != 10*time.Minute || cfg.BatchSize != 1000 {
		t.Errorf("Interval/BatchSize = %s/%d", cfg.Interval, cfg.BatchSize)
	}
	if cfg.Grace != 15*time.Minute {
		t.Errorf("Grace = %s, want 15m", cfg.Grace)
	}
	if cfg.Retention != 7*24*time.Hour || cfg.RetryWindow != 7*24*time.Hour {
		t.Errorf("Retention/RetryWindow = %s/%s", cfg.Retention, cfg.RetryWindow)
	}
}

func TestLoadCleanupConfigInvalidFallsBack(t *testing.T) {
	t.Setenv(cleanupEnabledEnv, "maybe")
	t.Setenv(cleanupIntervalEnv, "abc")
	t.Setenv(cleanupBatchEnv, "0")
	t.Setenv(cleanupGraceEnv, "-1")
	t.Setenv(cleanupRetentionEnv, "x")
	t.Setenv(cleanupRetryWindowEnv, "")

	cfg := LoadCleanupConfig()
	if !cfg.Enabled {
		t.Error("非法布尔值应回落默认 true")
	}
	if cfg.Interval != 10*time.Minute || cfg.BatchSize != 1000 || cfg.Grace != 15*time.Minute {
		t.Errorf("非法数值应回落默认: %+v", cfg)
	}
	if cfg.Retention != 7*24*time.Hour || cfg.RetryWindow != 7*24*time.Hour {
		t.Errorf("非法时长应回落默认: %+v", cfg)
	}
}

func TestLoadCleanupConfigDisabled(t *testing.T) {
	t.Setenv(cleanupEnabledEnv, "false")
	if LoadCleanupConfig().Enabled {
		t.Error("FILE_CLEANUP_ENABLED=false 应关闭清理")
	}
}

func TestCleanerStillPurgesWhenListFails(t *testing.T) {
	cleaner, _, purgeCalled := newTestCleaner(&fakeStore{}, nil)
	cleaner.listPending = func(time.Time, time.Duration, time.Duration, int) ([]model.File, error) {
		return nil, errors.New("db down")
	}

	cleaner.cleanOnce(context.Background())

	if *purgeCalled != 1 {
		t.Errorf("对象查询失败不应影响记录回收，调用次数 = %d", *purgeCalled)
	}
}
