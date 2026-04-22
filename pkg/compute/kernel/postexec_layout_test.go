package kernel

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPostexecLayoutHeader(t *testing.T) {
	t.Parallel()

	Convey("postexec layout header matches the generated Value layout", t, func() {
		body, err := readPostexecLayoutHeader()

		So(err, ShouldBeNil)
		So(
			body,
			ShouldContainSubstring,
			"#define PROPERTIES_REFUTATION_TARGET_WORD (PROPERTIES_START_WORD + 1)",
		)
		So(
			body,
			ShouldContainSubstring,
			"#define PROPERTIES_TTL_WORD               (PROPERTIES_START_WORD + 3)",
		)
		So(
			body,
			ShouldContainSubstring,
			"#define PROPERTIES_NOISE_WORD             (PROPERTIES_START_WORD + 4)",
		)
		So(
			body,
			ShouldContainSubstring,
			"#define SCHEDULING_NEXT_PROGRAM_WORD      117",
		)
		So(primitive.PropertiesStartWord+1, ShouldEqual, 57)
		So(primitive.PropertiesStartWord+3, ShouldEqual, 59)
		So(primitive.PropertiesStartWord+4, ShouldEqual, 60)
	})
}

func readPostexecLayoutHeader() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}

	path := filepath.Clean(filepath.Join(filepath.Dir(file), "shared", "postexec_layout.h"))
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), nil
}
