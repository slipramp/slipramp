package handler

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	oidcLoginStartClientLimit  = 10
	oidcLoginStartClientWindow = time.Minute
	oidcLoginStartGlobalLimit  = 512
	oidcLoginStartGlobalWindow = 10 * time.Minute
)

type oidcLoginStartLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	global  []time.Time
	clients map[string][]time.Time
}

func newOIDCLoginStartLimiter() *oidcLoginStartLimiter {
	return &oidcLoginStartLimiter{
		now:     time.Now,
		clients: map[string][]time.Time{},
	}
}

func (l *oidcLoginStartLimiter) allow(client string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now().UTC()
	l.global = startsAfter(l.global, now.Add(-oidcLoginStartGlobalWindow))
	for existingClient, starts := range l.clients {
		starts = startsAfter(starts, now.Add(-oidcLoginStartClientWindow))
		if len(starts) == 0 {
			delete(l.clients, existingClient)
			continue
		}
		l.clients[existingClient] = starts
	}

	retryAfter := time.Duration(0)
	if len(l.global) >= oidcLoginStartGlobalLimit {
		retryAfter = l.global[0].Add(oidcLoginStartGlobalWindow).Sub(now)
	}
	clientStarts := l.clients[client]
	if len(clientStarts) >= oidcLoginStartClientLimit {
		clientRetryAfter := clientStarts[0].Add(oidcLoginStartClientWindow).Sub(now)
		if clientRetryAfter > retryAfter {
			retryAfter = clientRetryAfter
		}
	}
	if retryAfter > 0 {
		return false, retryAfter
	}

	l.global = append(l.global, now)
	l.clients[client] = append(clientStarts, now)

	return true, 0
}

func startsAfter(starts []time.Time, cutoff time.Time) []time.Time {
	for index, startedAt := range starts {
		if startedAt.After(cutoff) {
			return starts[index:]
		}
	}

	return nil
}

func limitOIDCLoginStarts(next http.Handler, limiter *oidcLoginStartLimiter, trustedProxyHops int) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet ||
			(request.URL.Path != "/api/v1/auth/login" && request.URL.Path != "/api/v1/auth/native/login") {
			next.ServeHTTP(writer, request)
			return
		}

		client := requestClientAddress(request, trustedProxyHops)
		allowed, retryAfter := limiter.allow(client)
		if !allowed {
			writer.Header().Set("Retry-After", retryAfterHeader(retryAfter))
			WriteProblem(writer, http.StatusTooManyRequests, "Too many authentication attempts. Try again later.")
			return
		}

		next.ServeHTTP(writer, request)
	})
}

func requestClientAddress(request *http.Request, trustedProxyHops int) string {
	remoteAddress := normalizedRemoteAddress(request.RemoteAddr)
	if trustedProxyHops <= 0 {
		return remoteAddress
	}

	remainingHops := trustedProxyHops
	forwardedValues := request.Header.Values("X-Forwarded-For")
	for _, value := range slices.Backward(forwardedValues) {
		for {
			separator := strings.LastIndexByte(value, ',')
			remainingHops--
			if remainingHops == 0 {
				clientAddress := strings.TrimSpace(value[separator+1:])
				parsed, err := netip.ParseAddr(clientAddress)
				if err != nil {
					return remoteAddress
				}

				return parsed.String()
			}
			if separator < 0 {
				break
			}
			value = value[:separator]
		}
	}

	return remoteAddress
}

func normalizedRemoteAddress(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		remoteAddress = host
	}
	parsed, err := netip.ParseAddr(remoteAddress)
	if err != nil {
		return "unknown"
	}

	return parsed.String()
}

func retryAfterHeader(retryAfter time.Duration) string {
	seconds := max(int64(1), (retryAfter.Nanoseconds()+int64(time.Second)-1)/int64(time.Second))

	return strconv.FormatInt(seconds, 10)
}
