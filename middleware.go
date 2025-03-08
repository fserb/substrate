package substrate

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func (s *SubstrateHandler) fileExists(path string) bool {
	isDir := false
	if strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
		isDir = true
	}

	info, err := fs.Stat(s.fs, path)
	if err != nil {
		return false
	}

	if isDir {
		return info.IsDir()
	}
	return !info.IsDir()
}

func (s *SubstrateHandler) matchPattern(path string, pattern routePattern) *string {
	if len(pattern) == 0 {
		return &path
	}
	match := path
	for i, p := range pattern {
		if p == "*" {
			if i == len(pattern)-1 {
				return &path
			}
			next := pattern[i+1]
			for {
				idx := strings.Index(match, next)
				if idx == -1 {
					return nil
				}
				out := s.matchPattern(match[idx+len(next):], pattern[i+2:])
				if out != nil {
					return out
				}
				match = match[idx:]
			}

		} else if p == "?" {
			next := ""
			if i != len(pattern)-1 {
				next = pattern[i+1]
			}

			// pattern: /a/?.md

		} else {
			if !strings.HasPrefix(match, p) {
				return nil
			}
			match = match[len(p):]
		}
	}
}

// matchRoute checks if a path matches any route pattern
// Returns true if the path matches, and the potentially modified path for file matching
func (s *SubstrateHandler) matchRoute(path string, watcher *Watcher) (bool, string) {
	// If no routes specified, match everything
	if len(watcher.Order.Routes) == 0 {
		// But still check avoid patterns
		if s.isPathAvoided(path, watcher) {
			return false, path
		}
		return true, path
	}

	// Check if path matches any route pattern
	matched := false
	matchedPath := path

	for _, pattern := range watcher.Order.compiledRoutes {
		if pattern.hasFileMatch {
			// For file matching patterns, we need to check if files exist
			if s.fileMatchesPattern(path, pattern) {
				matched = true
				// Find the actual file that matched
				matchedPath = s.findMatchedFilePath(path, pattern)
				break
			}
		} else if s.pathMatchesPattern(path, pattern) {
			matched = true
			break
		}
	}

	// If matched, check if it should be avoided
	if matched && s.isPathAvoided(path, watcher) {
		return false, path
	}

	return matched, matchedPath
}

func (s *SubstrateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	origPath := r.URL.Path
	r.URL.Path = caddyhttp.CleanPath(r.URL.Path, true)

	decodedPath, err := url.QueryUnescape(r.URL.Path)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return nil
	}
	r.URL.Path = decodedPath

	if !strings.HasPrefix(r.URL.Path, s.Prefix) {
		r.URL.Path = origPath
		return next.ServeHTTP(w, r)
	}

	if r.URL.Path == s.Prefix+"/substrate" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return nil
	}

	if s.watcher == nil {
		v := caddyhttp.GetVar(r.Context(), "root")
		root, ok := v.(string)
		if !ok {
			root = "."
		}
		root += s.Prefix

		s.log.Debug("Creating watcher for root directory", zap.String("root", root))
		s.watcher = s.app.GetWatcher(root)
		if s.watcher == nil {
			s.log.Error("Failed to create substrate watcher")
			http.Error(w, "Failed to create substrate", http.StatusInternalServerError)
			return nil
		}
	}

	if !s.watcher.WaitUntilReady(5 * time.Second) {
		http.Error(w, "Failed to create substrate", http.StatusInternalServerError)
		return nil
	}

	if s.watcher.cmd == nil {
		r.URL.Path = origPath
		return next.ServeHTTP(w, r)
	}

	if s.watcher.Order == nil {
		s.log.Error("Invalid order configuration")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil
	}

	if s.Prefix != "" {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, s.Prefix)
	}

	useProxy, matchedPath := s.matchRoute(r.URL.Path, s.watcher)

	if useProxy && s.watcher.Order.Host == "" {
		useProxy = false
	}

	if useProxy {
		r.URL.Path = matchedPath

		s.log.Debug("Proxying request",
			zap.String("orig", origPath),
			zap.String("path", r.URL.Path),
			zap.String("upstream", s.watcher.Order.Host))
		s.proxy.SetHost(s.watcher.Order.Host)

		var scheme string
		if r.TLS == nil {
			scheme = "http"
		} else {
			scheme = "https"
		}
		r.Header.Set("X-Forwarded-Path", r.RequestURI)
		r.Header.Set("X-Forwarded-BaseURL", fmt.Sprintf("%s://%s", scheme, r.Host))

		return s.proxy.ServeHTTP(w, r, next)
	}

	r.URL.Path = origPath
	return next.ServeHTTP(w, r)
}

