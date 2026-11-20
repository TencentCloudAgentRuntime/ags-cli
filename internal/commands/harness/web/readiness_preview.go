//go:build preview

package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
)

const affinityCookie = "tencentags-deployment-affinity-id"

// Probe the native Web entry, retaining gateway cookies across cold-start
// responses. The browser must receive the gateway's actual affinity, which can
// differ from the requested affinity under a best-effort Deployment policy.
func waitForWeb(ctx context.Context, entry *url.URL, transport http.RoundTripper, interval time.Duration) (string, error) {
	jar, _ := cookiejar.New(nil)
	affinity := entry.Query().Get("affinity_id")
	confirmedAffinity := func() string {
		for _, cookie := range jar.Cookies(entry) {
			if cookie.Name == affinityCookie {
				if value, err := url.QueryUnescape(cookie.Value); err == nil && value != "" {
					return value
				}
			}
		}
		return affinity
	}
	client := &http.Client{Transport: transport, Jar: jar, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// Never forward a credential URL or cookies to another origin. Also
		// remove Go's automatically generated credential-bearing Referer.
		if req.URL.Scheme != entry.Scheme || req.URL.Host != entry.Host || len(via) >= 10 {
			return http.ErrUseLastResponse
		}
		req.Header.Del("Referer")
		if actual := confirmedAffinity(); actual != "" {
			query := req.URL.Query()
			query.Set("affinity_id", actual)
			req.URL.RawQuery = query.Encode()
		}
		return nil
	}}
	lastStatus := 0
	for {
		probe := *entry
		query := probe.Query()
		affinity = confirmedAffinity()
		if affinity != "" {
			query.Set("affinity_id", affinity)
		}
		for _, cookie := range jar.Cookies(entry) {
			if cookie.Name == "tencentags-deployment-token" {
				query.Del("token")
			}
		}
		probe.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.String(), nil)
		if err != nil {
			return "", webReadinessFailure(ctx, lastStatus)
		}
		response, err := client.Do(request)
		if err != nil {
			// Transport errors can contain the URL, token, response body or
			// proxy credentials. Only safe status/context information is public.
			return "", webReadinessFailure(ctx, lastStatus)
		}
		lastStatus = response.StatusCode
		_ = response.Body.Close()
		if lastStatus == http.StatusOK {
			return confirmedAffinity(), nil
		}
		if lastStatus != http.StatusNotFound && lastStatus != http.StatusBadGateway && lastStatus != http.StatusServiceUnavailable {
			return "", webReadinessFailure(ctx, lastStatus)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", webReadinessFailure(ctx, lastStatus)
		case <-timer.C:
		}
	}
}

func webReadinessFailure(ctx context.Context, status int) error {
	failure := &output.Failure{
		Code: "WEB_READY_FAILED", Kind: output.KindNetwork,
		Message: "Could not verify native Web console readiness",
		Hint:    "Check Deployment/instance health and network access, then retry 'agr harness web'.",
	}
	if status != 0 {
		failure.Message = fmt.Sprintf("Native Web console returned HTTP %d", status)
		failure.Details = map[string]any{"HTTPStatus": status}
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		failure.Code, failure.Kind = "WEB_READY_TIMEOUT", output.KindTimeout
		failure.Message = "Timed out waiting for native Web console readiness"
		failure.Hint = "Check Deployment/instance health or raise --timeout, then retry 'agr harness web'."
	case ctx.Err() != nil:
		failure.Code, failure.Kind = "CANCELED", output.KindGenericError
		failure.Message = "Web readiness wait was canceled"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		failure.Kind = output.KindAuthOrPermission
		failure.Hint = "Check Deployment access permissions, then rerun 'agr harness web' to acquire a fresh token."
	}
	return output.NewCLIError(failure)
}
