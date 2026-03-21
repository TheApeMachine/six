package huggingface

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDatasetDownloadShardReturnsErrorInsteadOfPanickingOnCacheMiss(t *testing.T) {
	t.Parallel()

	dataset := New(
		DatasetWithRepo("bad\nrepo"),
	)

	shard := fmt.Sprintf("missing-%d.parquet", time.Now().UnixNano())
	cachePath := filepath.Join(
		os.TempDir(),
		"six_hf_"+strings.ReplaceAll(dataset.repo+"_"+shard, "/", "_"),
	)

	_ = os.Remove(cachePath)

	Convey("Given a missing cache entry", t, func() {
		Convey("It should return an error instead of panicking", func() {
			var shardErr error

			So(func() {
				_, _, shardErr = dataset.downloadShard(shard, "main")
			}, ShouldNotPanic)

			So(shardErr, ShouldNotBeNil)
		})
	})
}
