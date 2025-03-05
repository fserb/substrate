package substrate

import (
	"fmt"
	"io/fs"
	"net/http"
	"path"
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

func (s *SubstrateHandler) findBestResource(r *http.Request, watcher *Watcher) *string {
	if watcher == nil || watcher.Order == nil {
		return nil
	}

	root := s.watcher.Root

	reqPath := caddyhttp.CleanPath(r.URL.Path, true)

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
			bigPath := reqPath
			if !strings.HasSuffix(bigPath, m.ext) {
				bigPath += m.ext
			}
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
				if !strings.HasPrefix(result, "/") {
					result = "/" + result
				}
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

func (s *SubstrateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	fmt.Println("SUBSTRATE MW:", s.Prefix)
	if !strings.HasPrefix(r.URL.Path, s.Prefix) {
		return next.ServeHTTP(w, r)
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
		return next.ServeHTTP(w, r)
	}

	if s.watcher.Order == nil {
		s.log.Error("Invalid order configuration")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil
	}

	origPath := r.URL.Path
	if s.Prefix != "" {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, s.Prefix)
	}

	useProxy := s.matchPath(r, s.watcher)
	if !useProxy {
		match := s.findBestResource(r, s.watcher)
		if match != nil {
			useProxy = true
			r.URL.Path = *match
		}
	}

	if useProxy && s.watcher.Order.Host == "" {
		useProxy = false
	}

	if useProxy {
		s.log.Debug("Proxying request", zap.String("upstream", s.watcher.Order.Host))
		s.proxy.SetHost(s.watcher.Order.Host)
		// Add forwarding headers
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

