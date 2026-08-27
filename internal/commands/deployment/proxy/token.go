package proxy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
	"golang.org/x/sync/singleflight"
)

const tokenRefreshBefore = 2 * time.Hour

type tokenManager struct {
	mu        sync.RWMutex
	token     string
	expires   time.Time
	lifecycle context.Context
	acquire   func(context.Context) (*ags.AcquireDeploymentTokenResponseParams, error)
	now       func() time.Time
	group     singleflight.Group
}

func newTokenManager(
	lifecycle context.Context,
	acquire func(context.Context) (*ags.AcquireDeploymentTokenResponseParams, error),
	now func() time.Time,
) *tokenManager {
	if now == nil {
		now = time.Now
	}
	if lifecycle == nil {
		lifecycle = context.Background()
	}
	return &tokenManager{lifecycle: lifecycle, acquire: acquire, now: now}
}

func (m *tokenManager) Token(ctx context.Context) (string, error) {
	if token, fresh := m.current(true); fresh {
		return token, nil
	}
	result := m.group.DoChan("deployment-token", func() (any, error) {
		// A caller can enter after another refresh has completed but before its
		// singleflight call begins, so recheck under the coalesced operation.
		if token, fresh := m.current(true); fresh {
			return token, nil
		}
		response, err := m.acquire(m.lifecycle)
		if err != nil {
			return m.fallback(err)
		}
		if response == nil || response.Token == nil || strings.TrimSpace(*response.Token) == "" {
			return m.fallback(fmt.Errorf("deployment token response did not contain a token"))
		}
		if response.ExpiresAt == nil {
			return m.fallback(fmt.Errorf("deployment token response did not contain an expiry"))
		}
		expires, err := time.Parse(time.RFC3339, *response.ExpiresAt)
		if err != nil {
			return m.fallback(fmt.Errorf("invalid deployment token expiry: %w", err))
		}
		if !expires.After(m.now()) {
			return m.fallback(fmt.Errorf("deployment token is already expired"))
		}
		token := *response.Token
		m.mu.Lock()
		m.token, m.expires = token, expires
		m.mu.Unlock()
		return token, nil
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case resolved := <-result:
		if resolved.Err != nil {
			return "", resolved.Err
		}
		token, _ := resolved.Val.(string)
		return token, nil
	}
}

func (m *tokenManager) current(requireFresh bool) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.token == "" || !m.expires.After(m.now()) {
		return "", false
	}
	if requireFresh && m.expires.Sub(m.now()) <= tokenRefreshBefore {
		return m.token, false
	}
	return m.token, true
}

func (m *tokenManager) fallback(refreshErr error) (string, error) {
	if token, valid := m.current(false); valid {
		return token, nil
	}
	return "", refreshErr
}
