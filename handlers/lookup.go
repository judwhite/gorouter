package handlers

import (
	"context"
	"net/http"
	"strings"

	"fmt"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

const (
	CfInstanceIdHeader = "X-CF-InstanceID"
	CfAppInstance      = "X-CF-APP-INSTANCE"
)

type lookupHandler struct {
	registry registry.Registry
	reporter metrics.CombinedReporter
	logger   logger.Logger
}

// NewLookup creates a handler responsible for looking up a route.
func NewLookup(registry registry.Registry, rep metrics.CombinedReporter, logger logger.Logger) negroni.Handler {
	return &lookupHandler{
		registry: registry,
		reporter: rep,
		logger:   logger,
	}
}

func (l *lookupHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	pool := l.lookup(r)
	if pool == nil {
		l.handleMissingRoute(rw, r)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), "RoutePool", pool))
	next(rw, r)
}

func (l *lookupHandler) handleMissingRoute(rw http.ResponseWriter, r *http.Request) {
	l.reporter.CaptureBadRequest()
	l.logger.Info("unknown-route")

	rw.Header().Set("X-Cf-RouterError", "unknown_route")

	writeStatus(
		rw,
		http.StatusNotFound,
		fmt.Sprintf("Requested route ('%s') does not exist.", r.Host),
		r.Context().Value("AccessLogRecord"),
		l.logger,
	)
}

func (l *lookupHandler) lookup(r *http.Request) *route.Pool {
	requestPath := r.URL.EscapedPath()

	uri := route.Uri(hostWithoutPort(r) + requestPath)
	appInstanceHeader := r.Header.Get(router_http.CfAppInstance)

	if appInstanceHeader != "" {
		appID, appIndex, err := validateCfAppInstance(appInstanceHeader)

		if err != nil {
			l.logger.Error("invalid-app-instance-header", zap.Error(err))
			return nil
		}

		return l.registry.LookupWithInstance(uri, appID, appIndex)
	}

	return l.registry.Lookup(uri)
}

func validateCfAppInstance(appInstanceHeader string) (string, string, error) {
	appDetails := strings.Split(appInstanceHeader, ":")
	if len(appDetails) != 2 {
		return "", "", fmt.Errorf("Incorrect %s header : %s", CfAppInstance, appInstanceHeader)
	}

	if appDetails[0] == "" || appDetails[1] == "" {
		return "", "", fmt.Errorf("Incorrect %s header : %s", CfAppInstance, appInstanceHeader)
	}

	return appDetails[0], appDetails[1], nil
}
