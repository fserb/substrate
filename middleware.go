package substrate

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

type statusCodeResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCodeResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	if statusCode == 515 {
		return
	}
	w.ResponseWriter.WriteHeader(statusCode)
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

	// Check if this path is in the bypass cache
	if _, found := s.watcher.statusCache.Get(origPath); found {
		return next.ServeHTTP(w, r)
	}

	if s.watcher.cmd == nil {
		r.URL.Path = origPath
		return next.ServeHTTP(w, r)
	}

	<-s.watcher.isReady

	if s.Prefix != "" {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, s.Prefix)
	}

	s.log.Debug("Proxying request",
		zap.String("orig", origPath),
		zap.String("path", r.URL.Path),
		zap.Int("port", s.watcher.Port))
	s.proxy.SetHost(fmt.Sprintf("localhost:%d", s.watcher.Port))

	var scheme string
	if r.TLS == nil {
		scheme = "http"
	} else {
		scheme = "https"
	}
	r.Header.Set("X-Forwarded-Path", r.RequestURI)
	r.Header.Set("X-Forwarded-BaseURL", fmt.Sprintf("%s://%s", scheme, r.Host))

	// Use our custom response writer to capture the status code
	statusWriter := &statusCodeResponseWriter{ResponseWriter: w}
	err = s.proxy.ServeHTTP(statusWriter, r, next)

	if statusWriter.status == 515 {
		s.watcher.statusCache.Add(origPath, true)

		r.URL.Path = origPath
		clear(w.Header())
		return next.ServeHTTP(w, r)
	}

	return err
}

