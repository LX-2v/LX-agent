package main

import (
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

var retained [][]byte
var hotMutex sync.Mutex

func main() {
	if err := os.MkdirAll("testdata", 0755); err != nil {
		panic(err)
	}
	cpuProfile()
	allocationHotspot()
	writeProfile("testdata/heap.pprof", pprof.Lookup("heap"))
	mutexContention()
	writeProfile("testdata/mutex.pprof", pprof.Lookup("mutex"))
	goroutineLeak()
}

func cpuProfile() {
	f, err := os.Create("testdata/cpu.pprof")
	if err != nil {
		panic(err)
	}
	if err = pprof.StartCPUProfile(f); err != nil {
		panic(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cpuHotLoop()
	}
	pprof.StopCPUProfile()
	if err = f.Close(); err != nil {
		panic(err)
	}
}

func cpuHotLoop() {
	x := uint64(1)
	for i := 0; i < 100000; i++ {
		x = x*1664525 + 1013904223
	}
	if x == 0 {
		panic("unreachable")
	}
}

func allocationHotspot() {
	for i := 0; i < 128; i++ {
		retained = append(retained, make([]byte, 64<<10))
	}
	runtime.GC()
}

func mutexContention() {
	runtime.SetMutexProfileFraction(1)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				lockContention()
			}
		}()
	}
	wg.Wait()
}

func lockContention() { hotMutex.Lock(); time.Sleep(time.Millisecond); hotMutex.Unlock() }

func goroutineLeak() {
	block := make(chan struct{})
	for i := 0; i < 12; i++ {
		go func() {
			select {
			case <-block:
			}
		}()
	}
	time.Sleep(100 * time.Millisecond)
	f, err := os.Create("testdata/goroutines.txt")
	if err != nil {
		panic(err)
	}
	if err = pprof.Lookup("goroutine").WriteTo(f, 2); err != nil {
		panic(err)
	}
	if err = f.Close(); err != nil {
		panic(err)
	}
}

func writeProfile(path string, p *pprof.Profile) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	if err = p.WriteTo(f, 0); err != nil {
		panic(err)
	}
	if err = f.Close(); err != nil {
		panic(err)
	}
}
