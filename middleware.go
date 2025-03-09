package substrate

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

// statusCodeResponseWriter is a custom http.ResponseWriter that captures the status code
// but only buffers the response if the status code is 515.
type statusCodeResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	is515       bool
}

// WriteHeader captures the status code and determines if we should buffer the response
func (w *statusCodeResponseWriter) WriteHeader(statusCode int) {
	fmt.Println("WriteHeader", statusCode)
	if !w.wroteHeader {
		w.status = statusCode
		w.wroteHeader = true

		// Only buffer if status code is 515
		if statusCode == 515 {
			w.is515 = true
			// Don't actually write the header to the underlying writer
			return
		}
	}

	// For all other status codes, pass through to the original writer
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write implements the http.ResponseWriter interface
func (w *statusCodeResponseWriter) Write(b []byte) (int, error) {
	fmt.Println("Write", string(b))
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}

	// If status is 515, discard the write
	if w.is515 {
		return len(b), nil // Pretend we wrote it
	}

	// Otherwise pass through to the original writer
	return w.ResponseWriter.Write(b)
}

// Hijack implements the http.Hijacker interface
func (w *statusCodeResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	fmt.Println("Hijack")
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Flush implements the http.Flusher interface
func (w *statusCodeResponseWriter) Flush() {
	fmt.Println("Flush")
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok && !w.is515 {
		flusher.Flush()
	}
}

// Status returns the HTTP status code
func (w *statusCodeResponseWriter) Status() int {
	fmt.Println("Status")
	if !w.wroteHeader {
		return http.StatusOK
	}
	return w.status
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
	// customWriter := &statusCodeResponseWriter{ResponseWriter: w}
	return s.proxy.ServeHTTP(w, r, next)

	// If status code is 515, reset the response and call next handler
	// if customWriter.Status() == 515 {
	// 	s.log.Debug("Received status 515, falling back to next handler")
	// 	r.URL.Path = origPath
	// 	return next.ServeHTTP(w, r)
	// }
	//
	// return err
}

