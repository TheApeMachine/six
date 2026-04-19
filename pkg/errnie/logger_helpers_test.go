package errnie

import (
	"io/fs"
	"syscall"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestKeyvalsToFields(t *testing.T) {
	t.Parallel()

	Convey("keyvalsToFields returns nil for empty input", t, func() {
		So(keyvalsToFields(nil), ShouldBeNil)
		So(keyvalsToFields([]any{}), ShouldBeNil)
	})

	Convey("keyvalsToFields maps string pairs to zap fields", t, func() {
		fields := keyvalsToFields([]any{"k1", 1, "k2", "v2"})

		So(len(fields), ShouldEqual, 2)
	})

	Convey("keyvalsToFields handles odd-length slices", t, func() {
		fields := keyvalsToFields([]any{"orphan"})

		So(len(fields), ShouldEqual, 1)
	})

	Convey("keyvalsToFields stringifies non-string keys", t, func() {
		fields := keyvalsToFields([]any{42, "val"})

		So(len(fields), ShouldEqual, 1)
	})
}

func TestZapSyncFailureIgnorable(t *testing.T) {
	t.Parallel()

	Convey("sync EINVAL on /dev/stderr is ignorable", t, func() {
		err := &fs.PathError{Op: "sync", Path: "/dev/stderr", Err: syscall.EINVAL}

		So(zapSyncFailureIgnorable(err), ShouldBeTrue)
	})

	Convey("non-sync operations are not ignorable", t, func() {
		err := &fs.PathError{Op: "open", Path: "/dev/stderr", Err: syscall.EINVAL}

		So(zapSyncFailureIgnorable(err), ShouldBeFalse)
	})

	Convey("sync with non-ignorable errno is not dropped", t, func() {
		err := &fs.PathError{Op: "sync", Path: "/dev/stderr", Err: syscall.EIO}

		So(zapSyncFailureIgnorable(err), ShouldBeFalse)
	})
}

func TestApplyElasticsearchEnvOverrides(t *testing.T) {
	Convey("ELASTICSEARCH_ENABLED forces shipping on", t, func() {
		t.Setenv("ELASTICSEARCH_ENABLED", "true")

		es := ElasticsearchConfig{Enabled: false}

		applyElasticsearchEnvOverrides(&es)

		So(es.Enabled, ShouldBeTrue)
	})

	Convey("ELASTICSEARCH_ENABLED can force shipping off", t, func() {
		t.Setenv("ELASTICSEARCH_ENABLED", "0")

		es := ElasticsearchConfig{Enabled: true}

		applyElasticsearchEnvOverrides(&es)

		So(es.Enabled, ShouldBeFalse)
	})

	Convey("ELASTIC_PASSWORD overrides the config password", t, func() {
		t.Setenv("ELASTIC_PASSWORD", "secret")

		es := ElasticsearchConfig{}

		applyElasticsearchEnvOverrides(&es)

		So(es.Password, ShouldEqual, "secret")
	})
}
