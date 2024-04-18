package main

import (
	"fmt"
	"os"
	"path"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
)

// This file is copied and sightly updated from https://github.com/zeromicro/go-zero
// @copyright original authors.

const (
	// DefaultMemProfileRate is the default memory profiling rate.
	// See also http://golang.org/pkg/runtime/#pkg-variables
	DefaultMemProfileRate = 4096

	timeFormat       = "20060102_150405"
	goroutineProfile = "goroutine"
	debugLevel       = 2
)

// started is non zero if a profile is running.
var started uint32

// Profiler represents an active profiling session.
type Profiler struct {
	dataDir string

	// closers holds cleanup functions that run after each profile
	closers []func()

	// stopped records if a call to profile.Stop has been made
	stopped uint32
}

func (p *Profiler) close() {
	for _, closer := range p.closers {
		closer()
	}
}

func (p *Profiler) startBlockProfile() {
	fn := p.createDumpFile("block")
	f, err := os.Create(fn)
	if err != nil {
		glog.Errorf("pprof: could not create block profile %q: %v", fn, err)
		return
	}

	runtime.SetBlockProfileRate(1)
	glog.Infof("pprof: block profiling enabled, %s", fn)
	p.closers = append(p.closers, func() {
		pprof.Lookup("block").WriteTo(f, 0)
		f.Close()
		runtime.SetBlockProfileRate(0)
		glog.Infof("pprof: block profiling disabled, %s", fn)
	})
}

func (p *Profiler) startCpuProfile() {
	fn := p.createDumpFile("cpu")
	f, err := os.Create(fn)
	if err != nil {
		glog.Errorf("pprof: could not create cpu profile %q: %v", fn, err)
		return
	}

	glog.Infof("pprof: cpu profiling enabled, %s", fn)
	pprof.StartCPUProfile(f)
	p.closers = append(p.closers, func() {
		pprof.StopCPUProfile()
		f.Close()
		glog.Infof("pprof: cpu profiling disabled, %s", fn)
	})
}

func (p *Profiler) startMemProfile() {
	fn := p.createDumpFile("mem")
	f, err := os.Create(fn)
	if err != nil {
		glog.Errorf("pprof: could not create memory profile %q: %v", fn, err)
		return
	}

	old := runtime.MemProfileRate
	runtime.MemProfileRate = DefaultMemProfileRate
	glog.Infof("pprof: memory profiling enabled (rate %d), %s", runtime.MemProfileRate, fn)
	p.closers = append(p.closers, func() {
		pprof.Lookup("heap").WriteTo(f, 0)
		f.Close()
		runtime.MemProfileRate = old
		glog.Infof("pprof: memory profiling disabled, %s", fn)
	})
}

func (p *Profiler) startMutexProfile() {
	fn := p.createDumpFile("mutex")
	f, err := os.Create(fn)
	if err != nil {
		glog.Errorf("pprof: could not create mutex profile %q: %v", fn, err)
		return
	}

	runtime.SetMutexProfileFraction(1)
	glog.Infof("pprof: mutex profiling enabled, %s", fn)
	p.closers = append(p.closers, func() {
		if mp := pprof.Lookup("mutex"); mp != nil {
			mp.WriteTo(f, 0)
		}
		f.Close()
		runtime.SetMutexProfileFraction(0)
		glog.Infof("pprof: mutex profiling disabled, %s", fn)
	})
}

func (p *Profiler) startThreadCreateProfile() {
	fn := p.createDumpFile("threadcreate")
	f, err := os.Create(fn)
	if err != nil {
		glog.Errorf("pprof: could not create threadcreate profile %q: %v", fn, err)
		return
	}

	glog.Infof("pprof: threadcreate profiling enabled, %s", fn)
	p.closers = append(p.closers, func() {
		if mp := pprof.Lookup("threadcreate"); mp != nil {
			mp.WriteTo(f, 0)
		}
		f.Close()
		glog.Infof("pprof: threadcreate profiling disabled, %s", fn)
	})
}

func (p *Profiler) startTraceProfile() {
	fn := p.createDumpFile("trace")
	f, err := os.Create(fn)
	if err != nil {
		glog.Errorf("pprof: could not create trace output file %q: %v", fn, err)
		return
	}

	if err := trace.Start(f); err != nil {
		glog.Errorf("pprof: could not start trace: %v", err)
		return
	}

	glog.Infof("pprof: trace enabled, %s", fn)
	p.closers = append(p.closers, func() {
		trace.Stop()
		glog.Infof("pprof: trace disabled, %s", fn)
	})
}

// Stop stops the profile and flushes any unwritten data.
func (p *Profiler) Stop() {
	if !atomic.CompareAndSwapUint32(&p.stopped, 0, 1) {
		// someone has already called close
		return
	}
	p.close()
	atomic.StoreUint32(&started, 0)
}

// StartProfiler starts a new profiling session.
// The caller should call the Stop method on the value returned
// to cleanly stop profiling.
func StartProfiler(dataDir string) *Profiler {
	prof := &Profiler{
		dataDir: dataDir,
	}

	prof.startCpuProfile()
	prof.startMemProfile()
	prof.startMutexProfile()
	prof.startBlockProfile()
	prof.startTraceProfile()
	prof.startThreadCreateProfile()

	return prof
}

func (p *Profiler) createDumpFile(kind string) string {
	return path.Join(p.dataDir, fmt.Sprintf("%s-%s.pprof", kind, time.Now().Format(timeFormat)))
}

func (p *Profiler) dumpGoroutines() {
	dumpFile := path.Join(p.dataDir, fmt.Sprintf("goroutines-%s.dump", time.Now().Format(timeFormat)))
	glog.Infof("Got dump goroutine signal, dumping goroutine profile to %s", dumpFile)
	if f, err := os.Create(dumpFile); err != nil {
		glog.Errorf("Failed to dump goroutine profile, error: %v", err)
	} else {
		defer f.Close()
		if err := pprof.Lookup(goroutineProfile).WriteTo(f, debugLevel); err != nil {
			glog.Errorf("Failed to write goroutine profile to %s, error: %v", dumpFile, err)
		}
	}
}
