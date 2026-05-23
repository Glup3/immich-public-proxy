package invalid

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/alangrainger/immich-public-proxy/internal/config"
)

type Handler struct {
	Config config.Config
}

func New(cfg config.Config) Handler {
	return Handler{Config: cfg}
}

func Log(message string) {
	fmt.Println(time.Now().Format(time.RFC3339) + " " + message)
}

func (h Handler) Respond(w http.ResponseWriter, defaultResponse int, logMessage string) {
	method := h.Config.IPP.CustomInvalidResponse
	if method == nil {
		h.drop(w, logMessage)
		return
	}
	if b, ok := method.(bool); ok && !b {
		method = defaultResponse
	}
	if logMessage != "" {
		logMessage = " - " + logMessage
	}

	switch value := method.(type) {
	case int:
		Log(fmt.Sprintf("Return status %d%s", value, logMessage))
		w.WriteHeader(value)
	case float64:
		status := int(value)
		Log(fmt.Sprintf("Return status %d%s", status, logMessage))
		w.WriteHeader(status)
	case string:
		if strings.HasPrefix(value, "http") {
			w.Header().Set("Location", value)
			w.WriteHeader(http.StatusFound)
			return
		}
		Log("Return status 404" + logMessage)
		w.WriteHeader(http.StatusNotFound)
	default:
		Log("Return status 404" + logMessage)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (h Handler) drop(w http.ResponseWriter, logMessage string) {
	if logMessage != "" {
		logMessage = " - " + logMessage
	}
	Log("Dropping connection" + logMessage)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		return
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
}
