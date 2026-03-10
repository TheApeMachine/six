package task

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	tools "github.com/theapemachine/six/experiment"
)

/*
Profiler captures a CPU profile and a post-run heap snapshot for a single
experiment run.  Profiles land in <paper>/profiles/<slug>_cpu.pprof and
<paper>/profiles/<slug>_mem.pprof so they can be inspected independently
with go tool pprof without cross-contamination between experiments.
*/
type Profiler struct {
	slug    string
	dir     string
	cpuFile *os.File
	start   time.Time
}

/*
NewProfiler creates a Profiler for the given experiment.
It immediately opens the CPU-profile file and starts sampling.
Returns an error when the profiles directory cannot be created or the
profile file cannot be opened; the caller should log the error and skip
profiling rather than abort the run.
*/
func NewProfiler(experiment tools.PipelineExperiment) (*Profiler, error) {
	slug := tools.Slugify(experiment.Name())
	dir := filepath.Join(PaperDir(experiment.Section()), "..", "..", "profiles")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("profiler mkdir %s: %w", dir, err)
	}

	cpuPath := filepath.Join(dir, slug+"_cpu.pprof")

	cpuFile, err := os.Create(cpuPath)
	if err != nil {
		return nil, fmt.Errorf("profiler create %s: %w", cpuPath, err)
	}

	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		cpuFile.Close()
		return nil, fmt.Errorf("profiler start cpu: %w", err)
	}

	return &Profiler{
		slug:    slug,
		dir:     dir,
		cpuFile: cpuFile,
		start:   time.Now(),
	}, nil
}

/*
Stop ends CPU sampling and writes the heap snapshot.
It is safe to call on a nil receiver (no-op), so callers can defer Stop()
unconditionally even when NewProfiler returned an error.
*/
func (prof *Profiler) Stop() {
	if prof == nil {
		return
	}

	pprof.StopCPUProfile()
	prof.cpuFile.Close()

	memPath := filepath.Join(prof.dir, prof.slug+"_mem.pprof")

	memFile, err := os.Create(memPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "profiler: could not create %s: %v\n", memPath, err)
		return
	}

	defer memFile.Close()

	runtime.GC()

	if err := pprof.WriteHeapProfile(memFile); err != nil {
		fmt.Fprintf(os.Stderr, "profiler: heap write failed: %v\n", err)
	}

	fmt.Printf(
		"\n  [pprof] %s  cpu=%s  mem=%s  wall=%s\n",
		prof.slug,
		filepath.Join("profiles", prof.slug+"_cpu.pprof"),
		filepath.Join("profiles", prof.slug+"_mem.pprof"),
		time.Since(prof.start).Round(time.Millisecond),
	)
}
