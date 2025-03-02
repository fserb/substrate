package substrate

import (
	"runtime"
	"runtime/debug"
	"time"
)

// DebugInfo contains information about the substrate system
type DebugInfo struct {
	// Version      string            `json:"version"`
	BuildInfo    *debug.BuildInfo  `json:"build_info,omitempty"`
	GoVersion    string            `json:"go_version"`
	GOOS         string            `json:"goos"`
	GOARCH       string            `json:"goarch"`
	NumGoroutine int               `json:"num_goroutine"`
	NumCPU       int               `json:"num_cpu"`
	Uptime       string            `json:"uptime"`
	StartTime    time.Time         `json:"start_time"`
	MemStats     runtime.MemStats  `json:"mem_stats"`
	Watchers     map[string]string `json:"watchers,omitempty"`
}

var startTime = time.Now()

func GetDebugInfo(s *Server) *DebugInfo {
	info := &DebugInfo{
		// Version:      "1.0.0",
		GoVersion:    runtime.Version(),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		NumGoroutine: runtime.NumGoroutine(),
		NumCPU:       runtime.NumCPU(),
		Uptime:       time.Since(startTime).String(),
		StartTime:    startTime,
		Watchers:     make(map[string]string),
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if ok {
		info.BuildInfo = buildInfo
	}

	runtime.ReadMemStats(&info.MemStats)

	// Get watcher info
	if s != nil && s.watchers != nil {
		for key, watcher := range s.watchers {
			if watcher.IsReady() {
				info.Watchers[key] = watcher.Root
			} else {
				info.Watchers[key] = watcher.Root + " (not ready)"
			}
		}
	}

	return info
}
