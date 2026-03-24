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
				_, shardErr = dataset.downloadShard(shard, "main")
			}, ShouldNotPanic)

			So(shardErr, ShouldNotBeNil)
		})
	})
}

func TestDatasetLabelAsText(t *testing.T) {
	t.Parallel()

	dataset := New(
		DatasetWithLabelAppend([]string{"neg", "pos"}),
	)

	Convey("Given a labeled dataset", t, func() {
		Convey("When labels are present it should resolve known indexes via labelAppend", func() {
			So(dataset.labelAsText(1, true), ShouldEqual, "pos")
		})

		Convey("When labels are out of range it should fall back to numeric index", func() {
			So(dataset.labelAsText(3, true), ShouldEqual, "3")
		})

		Convey("When label is absent it should return an empty value", func() {
			So(dataset.labelAsText(0, false), ShouldEqual, "")
		})
	})
}
