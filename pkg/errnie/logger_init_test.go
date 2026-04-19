package errnie

import (
	"context"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
)

/*
TestInitLogger exercises the console-only configuration path. It mutates
process-wide logging state and must not run in parallel with other tests
that assume the default test logger.
*/
func TestInitLogger(t *testing.T) {
	Convey("InitLogger with Elasticsearch disabled wires stderr zap", t, func() {
		InitLogger(LoggingConfig{
			Logfile: false,
			Elasticsearch: ElasticsearchConfig{
				Enabled: false,
			},
		})

		So(Logger(), ShouldNotBeNil)

		Info("errnie.logger_init_test", "case", "info")
		Debug("errnie.logger_init_test", "case", "debug")
		Warn("errnie.logger_init_test", "case", "warn")

		So(Sync(), ShouldBeNil)

		logger := Logger()

		n, readErr := logger.Read(make([]byte, 16))

		So(n, ShouldEqual, 0)
		So(readErr, ShouldEqual, io.EOF)

		_, writeErr := logger.Write([]byte("x"))

		So(writeErr, ShouldNotBeNil)
		So(logger.Close(), ShouldBeNil)
	})

	Convey("Shutdown flushes without panicking", t, func() {
		So(Shutdown(context.Background()), ShouldBeNil)
	})
}

func TestInitLoggerFromViper(t *testing.T) {
	Convey("InitLoggerFromViper unmarshals the logging key", t, func() {
		viper.Set("logging.logfile", false)
		viper.Set("logging.elasticsearch.enabled", false)

		err := InitLoggerFromViper()

		So(err, ShouldBeNil)
		So(InitError(), ShouldBeNil)
	})
}
