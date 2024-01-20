package kit

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/neoxelox/errors"

	"github.com/neoxelox/kit/util"
)

const (
	_HTTP_CLIENT_RETRY_DEDUP_SKIP_COUNT = 6
)

var (
	ErrHTTPClientGeneric   = errors.New("http client failed")
	ErrHTTPClientTimedOut  = errors.New("http client timed out")
	ErrHTTPClientBadStatus = errors.New("http client bad status code (%d)")
)

var (
	_HTTP_CLIENT_DEFAULT_CONFIG = HTTPClientConfig{
		AllowedRedirects: util.Pointer(0),
		DefaultRetry: &RetryConfig{
			Attempts:     1,
			InitialDelay: 0 * time.Second,
			LimitDelay:   0 * time.Second,
			Retriables:   []error{},
		},
	}
)

type HTTPClientConfig struct {
	Timeout          time.Duration
	AllowedRedirects *int
	DefaultRetry     *RetryConfig
}

type HTTPClient struct {
	config   HTTPClientConfig
	observer *Observer
	client   *http.Client
}

func NewHTTPClient(observer *Observer, config HTTPClientConfig) *HTTPClient {
	util.Merge(&config, _HTTP_CLIENT_DEFAULT_CONFIG)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= *config.AllowedRedirects {
				return http.ErrUseLastResponse
			}

			return nil
		},
		Timeout: config.Timeout,
	}

	return &HTTPClient{
		config:   config,
		observer: observer,
		client:   client,
	}
}

func (self *HTTPClient) Request(
	ctx context.Context, method string, url string, body io.Reader, retry ...RetryConfig) (*http.Response, error) {
	_retry := util.Optional(retry, *self.config.DefaultRetry)

	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, ErrHTTPClientGeneric.Raise().Cause(err)
	}

	return self._do(request, &_retry)
}

func (self *HTTPClient) Do(request *http.Request) (*http.Response, error) {
	return self._do(request, self.config.DefaultRetry)
}

func (self *HTTPClient) _do(request *http.Request, retry *RetryConfig) (*http.Response, error) {
	_, endTraceRequest := self.observer.TraceClientRequest(request.Context(), request)
	defer endTraceRequest()

	retryOnBadStatusCode := false
	for _, err := range retry.Retriables {
		if ErrHTTPClientBadStatus.Is(err) {
			retryOnBadStatusCode = true
			break
		}
	}

	var response *http.Response

	err := util.ExponentialRetry(
		retry.Attempts, retry.InitialDelay,
		retry.LimitDelay, retry.Retriables,
		func(attempt int) error {
			var err error // nolint:govet

			response, err = self.client.Do(request) // nolint:bodyclose
			if err != nil {
				if urlErr, ok := err.(*url.Error); ok && urlErr.Timeout() {
					return ErrHTTPClientTimedOut.Raise().
						Skip(2 + _HTTP_CLIENT_RETRY_DEDUP_SKIP_COUNT).
						Extra(map[string]any{"attempt": attempt, "timeout": self.config.Timeout}).
						Cause(err)
				}

				return ErrHTTPClientGeneric.Raise().
					Skip(2 + _HTTP_CLIENT_RETRY_DEDUP_SKIP_COUNT).
					Extra(map[string]any{"attempt": attempt}).
					Cause(err)
			}

			if response.StatusCode >= 400 && retryOnBadStatusCode {
				response.Body.Close()
				return ErrHTTPClientBadStatus.Raise(response.StatusCode).
					Skip(2 + _HTTP_CLIENT_RETRY_DEDUP_SKIP_COUNT).
					Extra(map[string]any{"attempt": attempt, "status": response.StatusCode})
			}

			return nil
		})
	if err != nil {
		return nil, err
	}

	return response, nil
}

func (self *HTTPClient) Close(ctx context.Context) error {
	err := util.Deadline(ctx, func(exceeded <-chan struct{}) error {
		// Don't log the normal closing messages because this HTTP client
		// is expected to be embedded in other user's custom HTTP clients

		self.client.CloseIdleConnections()

		return nil
	})
	if err != nil {
		if util.ErrDeadlineExceeded.Is(err) {
			return ErrHTTPClientTimedOut.Raise().Cause(err)
		}

		return err
	}

	return nil
}
