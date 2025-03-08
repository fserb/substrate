package substrate

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

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

	if s.watcher.cmd == nil {
		r.URL.Path = origPath
		return next.ServeHTTP(w, r)
	}

	if s.Prefix != "" {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, s.Prefix)
	}

	s.log.Debug("Proxying request",
		zap.String("orig", origPath),
		zap.String("path", r.URL.Path))
	s.proxy.SetHost(fmt.Sprintf("http://localhost:%d", s.watcher.Port))

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

