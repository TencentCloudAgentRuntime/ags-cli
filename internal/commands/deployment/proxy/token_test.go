package proxy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ags "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ags/v20250920"
)

func TestTokenManagerIsLazyAndRefreshesAtTwoHours(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	calls := 0
	manager := newTokenManager(context.Background(), func(context.Context) (*ags.AcquireDeploymentTokenResponseParams, error) {
		calls++
		token := "dpt_fresh"
		expires := now.Add(24 * time.Hour).Format(time.RFC3339)
		return &ags.AcquireDeploymentTokenResponseParams{Token: &token, ExpiresAt: &expires}, nil
	}, func() time.Time { return now })
	if calls != 0 {
		t.Fatal("token acquired eagerly")
	}
	if token, err := manager.Token(context.Background()); err != nil || token != "dpt_fresh" || calls != 1 {
		t.Fatalf("first Token() = (%q, %v), calls=%d", token, err, calls)
	}
	if _, err := manager.Token(context.Background()); err != nil || calls != 1 {
		t.Fatalf("fresh token should be reused, err=%v calls=%d", err, calls)
	}
	now = now.Add(22 * time.Hour)
	if _, err := manager.Token(context.Background()); err != nil || calls != 2 {
		t.Fatalf("token at refresh threshold should refresh, err=%v calls=%d", err, calls)
	}
}

func TestTokenManagerCoalescesConcurrentRefresh(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	manager := newTokenManager(context.Background(), func(context.Context) (*ags.AcquireDeploymentTokenResponseParams, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		token, expires := "dpt_shared", now.Add(24*time.Hour).Format(time.RFC3339)
		return &ags.AcquireDeploymentTokenResponseParams{Token: &token, ExpiresAt: &expires}, nil
	}, func() time.Time { return now })

	const goroutines = 12
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			token, err := manager.Token(context.Background())
			if err == nil && token != "dpt_shared" {
				err = errors.New("unexpected token")
			}
			errs <- err
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls.Load())
	}
}

func TestTokenManagerFallsBackOnlyToUnexpiredToken(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	calls := 0
	manager := newTokenManager(context.Background(), func(context.Context) (*ags.AcquireDeploymentTokenResponseParams, error) {
		calls++
		if calls > 1 {
			return nil, errors.New("control plane unavailable")
		}
		token, expires := "dpt_old", now.Add(90*time.Minute).Format(time.RFC3339)
		return &ags.AcquireDeploymentTokenResponseParams{Token: &token, ExpiresAt: &expires}, nil
	}, func() time.Time { return now })
	if token, err := manager.Token(context.Background()); err != nil || token != "dpt_old" {
		t.Fatalf("initial Token() = (%q, %v)", token, err)
	}
	if token, err := manager.Token(context.Background()); err != nil || token != "dpt_old" || calls != 2 {
		t.Fatalf("fallback Token() = (%q, %v), calls=%d", token, err, calls)
	}
	now = now.Add(2 * time.Hour)
	if _, err := manager.Token(context.Background()); err == nil {
		t.Fatal("expired fallback token should not be returned")
	}
}

func TestTokenManagerLeaderCancellationDoesNotPoisonSharedRefresh(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	lifecycleCtx, stop := context.WithCancel(context.Background())
	defer stop()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	manager := newTokenManager(lifecycleCtx, func(ctx context.Context) (*ags.AcquireDeploymentTokenResponseParams, error) {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			token, expires := "dpt_shared", now.Add(24*time.Hour).Format(time.RFC3339)
			return &ags.AcquireDeploymentTokenResponseParams{Token: &token, ExpiresAt: &expires}, nil
		}
	}, func() time.Time { return now })

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderResult := make(chan error, 1)
	go func() {
		_, err := manager.Token(leaderCtx)
		leaderResult <- err
	}()
	<-started
	followerResult := make(chan error, 1)
	go func() {
		token, err := manager.Token(context.Background())
		if err == nil && token != "dpt_shared" {
			err = errors.New("unexpected follower token")
		}
		followerResult <- err
	}()
	cancelLeader()
	if err := <-leaderResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want canceled", err)
	}
	close(release)
	if err := <-followerResult; err != nil {
		t.Fatalf("follower error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls.Load())
	}
}
