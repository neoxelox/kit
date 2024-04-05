package kit

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/neoxelox/errors"

	"github.com/neoxelox/kit/util"
)

const (
	_HTTP_CLIENT_RETRY_DEDUP_SKIP_COUNT = 6
)

var (
	ErrHTTPClientGeneric     = errors.New("http client failed")
	ErrHTTPClientTimedOut    = errors.New("http client timed out")
	ErrHTTPClientBadStatus   = errors.New("http client bad status (%d)")
	ErrHTTPClientRateLimited = errors.New("http client rate limited (%d)")
)

var (
	_HTTP_CLIENT_DEFAULT_CONFIG = HTTPClientConfig{
		BaseURL:          nil,
		Headers:          nil,
		RaiseForStatus:   util.Pointer(false),
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
	BaseURL          *string
	Headers          *map[string]string
	RaiseForStatus   *bool
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
	ctx context.Context, method string, url string,
	body []byte, headers map[string]string, retry ...RetryConfig) (*http.Response, error) {
	_retry := util.Optional(retry, *self.config.DefaultRetry)

	if self.config.BaseURL != nil {
		url = *self.config.BaseURL + url
	}

	var buffer io.Reader = nil
	if len(body) > 0 {
		buffer = bytes.NewReader(body)
	}

	request, err := http.NewRequestWithContext(ctx, method, url, buffer)
	if err != nil {
		return nil, ErrHTTPClientGeneric.Raise().Cause(err)
	}

	for header, value := range headers {
		request.Header.Set(header, value)
	}

	return self._do(request, &_retry)
}

func (self *HTTPClient) Do(request *http.Request) (*http.Response, error) {
	return self._do(request, self.config.DefaultRetry)
}

func (self *HTTPClient) _do(request *http.Request, retry *RetryConfig) (*http.Response, error) {
	if self.config.Headers != nil {
		for header, value := range *self.config.Headers {
			request.Header.Set(header, value)
		}
	}

	_, endTraceRequest := self.observer.TraceClientRequest(request.Context(), request)
	defer endTraceRequest()

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

			if response.StatusCode == 429 && *self.config.RaiseForStatus {
				wait := int64(0)

				if retryAfter := response.Header.Get("Retry-After"); len(retryAfter) > 0 {
					wait, _ = strconv.ParseInt(retryAfter, 10, 0)
				} else if retryOn := response.Header.Get("X-Rate-Limit-Reset"); len(retryOn) > 0 {
					retryOnInt, _ := strconv.ParseInt(retryOn, 10, 0)
					now := time.Now().Unix()
					if retryOnInt > now {
						wait = retryOnInt - now
					} else {
						wait = retryOnInt
					}
				}

				response.Body.Close()

				return ErrHTTPClientRateLimited.Raise(wait).
					Skip(2 + _HTTP_CLIENT_RETRY_DEDUP_SKIP_COUNT).
					Extra(map[string]any{"attempt": attempt, "status": response.StatusCode, "wait": wait})
			}

			if response.StatusCode >= 400 && response.StatusCode != 429 && *self.config.RaiseForStatus {
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
