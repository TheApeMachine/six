package workflow

import (
	"io"

	"github.com/theapemachine/six/pkg/errnie"
)

type Seeder struct {
	seed io.ReadCloser
}

func NewSeeder(seed io.ReadCloser) *Seeder {
	return &Seeder{seed: seed}
}

func (seeder *Seeder) Read(p []byte) (n int, err error) {
	n, err = seeder.seed.Read(p)
	errnie.Trace("workflow.seeder.Read", "n", n, "err", err)
	return n, err
}

func (seeder *Seeder) Write(p []byte) (n int, err error) {
	errnie.Trace("workflow.seeder.Write", "len", len(p))
	return len(p), nil
}

func (s *Seeder) Close() error {
	return s.seed.Close()
}
