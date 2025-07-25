//nolint:gosec
package wrapper

import (
	"context"
	go_http "net/http"
)

//go:generate mockgen -source=http.go -destination=../mocks/http.go -package=mocks -mock_names=HTTP=MockHTTP
type HTTP interface {
	Get(url string) (*go_http.Response, error)
}

type http struct{}

func NewHTTP() HTTP {
	return http{}
}

func (h http) Get(url string) (*go_http.Response, error) {
	return go_http.Get(url)
}

//go:generate mockgen -source=http.go -destination=../mocks/http.go -package=mocks -mock_names=HTTPServer=MockHTTPServer
type HTTPServer interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
}

type httpServer struct {
	server *go_http.Server
}

func NewHTTPServer(server *go_http.Server) HTTPServer {
	return &httpServer{server: server}
}

func (h *httpServer) ListenAndServe() error {
	return h.server.ListenAndServe()
}

func (h *httpServer) Shutdown(ctx context.Context) error {
	return h.server.Shutdown(ctx)
}
