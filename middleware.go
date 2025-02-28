package substrate

import (
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
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

func (s *SubstrateHandler) findBestResource(r *http.Request, watcher *Watcher) *string {
	if watcher == nil || watcher.Order == nil {
		return nil
	}

	v := caddyhttp.GetVar(r.Context(), "root")
	root, ok := v.(string)
	if !ok {
		root = "."
	}

	reqPath := caddyhttp.CleanPath(r.URL.Path, true)

	if s.fileExists(caddyhttp.SanitizedPathJoin(root, reqPath)) {
		return &reqPath
	}

	if watcher.Order.matchers != nil {
		for _, m := range watcher.Order.matchers {
			if !strings.HasPrefix(reqPath, m.path) {
				continue
			}

			bigPath := caddyhttp.CleanPath(reqPath+"/index"+m.ext, true)
			if s.fileExists(caddyhttp.SanitizedPathJoin(root, bigPath)) {
				return &bigPath
			}
		}

		for _, m := range watcher.Order.matchers {
			if !strings.HasPrefix(reqPath, m.path) {
				continue
			}
			bigPath := reqPath + m.ext
			if s.fileExists(caddyhttp.SanitizedPathJoin(root, bigPath)) {
				return &bigPath
			}
		}
	}

	if watcher.Order.CatchAll != nil && len(watcher.Order.CatchAll) > 0 {
		for _, ca := range watcher.Order.CatchAll {
			cad := path.Dir(ca)
			if !strings.HasPrefix(reqPath, cad) {
				continue
			}
			candidate := caddyhttp.SanitizedPathJoin(root, ca)
			if s.fileExists(candidate) {
				result := ca
				return &result
			}
		}
	}

	return nil
}

func (s *SubstrateHandler) matchPath(r *http.Request, watcher *Watcher) bool {
	if watcher == nil || watcher.Order == nil {
		return false
	}

	if watcher.Order.Paths == nil {
		return false
	}

	for _, p := range watcher.Order.Paths {
		if p == r.URL.Path {
			return true
		}
	}

	return false
}

func (s SubstrateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Get the root directory from the request context
	v := caddyhttp.GetVar(r.Context(), "root")
	root, ok := v.(string)
	if !ok {
		root = "."
	}

	// Get or create a watcher for this root if we don't already have one
	if s.watcher == nil {
		watcher := GetOrCreateWatcher(root, s.app)
		if watcher == nil {
			http.Error(w, "Failed to create substrate", http.StatusInternalServerError)
			return nil
		}
		s.watcher = watcher
	}

	// Wait for the watcher to be ready or determine it has no substrate
	// Use a 5 second timeout to avoid hanging indefinitely
	if !s.watcher.WaitUntilReady(5 * time.Second) {
		http.Error(w, "Failed to create substrate", http.StatusInternalServerError)
		return nil
	}

	useProxy := s.matchPath(r, s.watcher)

	if !useProxy {
		match := s.findBestResource(r, s.watcher)
		if match != nil {
			useProxy = true
			r.URL.Path = *match
		}
	}

	if useProxy {
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

	return next.ServeHTTP(w, r)
}
