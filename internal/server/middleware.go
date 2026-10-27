package server

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type IPRateLimiter struct {
	lock     sync.Mutex
	limiters map[string]*rate.Limiter
	rate     rate.Limit
	burst    int
}

func NewIPRateLimiter(requestPerSecond float64, burst int) *IPRateLimiter {
	return &IPRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(requestPerSecond),
		burst:    burst,
	}
}

func (limiter *IPRateLimiter) getLimiterForIP(ipAddress string) *rate.Limiter {
	limiter.lock.Lock()
	defer limiter.lock.Unlock()

	ipLimiter, exists := limiter.limiters[ipAddress]
	if !exists {
		ipLimiter = rate.NewLimiter(limiter.rate, limiter.burst)
		limiter.limiters[ipAddress] = ipLimiter
	}
	return ipLimiter
}

func RateLimitMiddleware(limiter *IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				clientIP = r.RemoteAddr
			}

			ipLimiter := limiter.getLimiterForIP(clientIP)
			if !ipLimiter.Allow() {
				WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests, please slow down")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (recorder *statusRecorder) WriteHeader(code int) {
	recorder.statusCode = code
	recorder.ResponseWriter.WriteHeader(code)
}

func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()
			recorder := &statusRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(recorder, r)

			duration := time.Since(startTime)
			logger.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", recorder.statusCode),
				slog.Duration("duration", duration),
			)
		})
	}
}

func RecoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic recovered", slog.String("error", fmt.Sprintf("%v", recovered)))
					WriteError(w, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
