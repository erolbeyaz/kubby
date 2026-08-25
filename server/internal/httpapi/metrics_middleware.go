package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/erolbeyaz/kubby/internal/metrics"
)

// observe records what each request cost.
//
// Labelled by chi's route pattern rather than by the path. `/clusters/{id}/object/*` is
// one series; `/clusters/9613.../object/pods` would be one series per cluster per kind
// per name, which is how a metrics endpoint takes down the Prometheus scraping it.
func observe(reg *metrics.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if reg == nil {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			reg.HTTPInFlight.Inc()
			defer reg.HTTPInFlight.Dec()

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			// Read after serving: chi fills the pattern in as it routes, so before the
			// handler runs it is empty.
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				// Anything that matched no route. Bucketed under one label so a scanner
				// probing random paths cannot mint a series per attempt.
				route = "unmatched"
			}

			reg.HTTPRequests.WithLabelValues(r.Method, route, strconv.Itoa(ww.Status())).Inc()
			reg.HTTPDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		})
	}
}

// metricsHandler serves the exposition format.
//
// Behind authentication and the metrics permission, not open. Kubby's own metrics name
// every cluster it talks to and how much of everything it holds; that is a map of the
// estate, and an unauthenticated /metrics hands it to anyone who can reach the port.
func metricsHandler(reg *metrics.Registry) http.HandlerFunc {
	if reg == nil {
		return func(w http.ResponseWriter, r *http.Request) {
			writeError(w, r, http.StatusNotFound, "metrics are not enabled")
		}
	}

	handler := promhttp.HandlerFor(reg.Gatherer(), promhttp.HandlerOpts{
		// A broken collector should not make the endpoint useless: serve what could be
		// gathered and report the rest rather than answering 500 with nothing.
		ErrorHandling: promhttp.ContinueOnError,
	})
	return handler.ServeHTTP
}

// guardMetrics admits a scraper holding the token, and anyone else only through the
// ordinary session chain.
//
// Written as one gate in front of two paths rather than as a flag threaded through the
// authentication middleware: an "already authorised" marker in the request context is a
// thing every other handler would then have to be trusted not to honour by accident.
//
// There is no unauthenticated path in either branch. Kubby's own metrics name every
// cluster it talks to and how much of everything it holds, which is a map of the estate.
func guardMetrics(token string, scrape http.Handler, session http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			offered := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			// Constant time: a token compared with == leaks its length and prefix to
			// anything that can time the response.
			if subtle.ConstantTimeCompare([]byte(offered), []byte(token)) == 1 {
				scrape.ServeHTTP(w, r)
				return
			}
		}
		session.ServeHTTP(w, r)
	}
}
