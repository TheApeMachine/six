package workflow

import "io"

type Seeder struct {
	seed io.ReadCloser
}

func NewSeeder(seed io.ReadCloser) *Seeder {
	return &Seeder{seed: seed}
}

func (s *Seeder) Read(p []byte) (n int, err error) {
	return s.seed.Read(p)
}

func (s *Seeder) Write(p []byte) (n int, err error) {
	return 0, nil
}

func (s *Seeder) Close() error {
	return s.seed.Close()
}
