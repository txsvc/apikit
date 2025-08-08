package api

import (
	"context"
	"net/http"
	"time"

	"github.com/PuerkitoBio/rehttp"
)

type (
	loggingTransport struct {
		InnerTransport http.RoundTripper
	}

	contextKey struct {
		name string
	}
)

var contextKeyRequestStart = &contextKey{"RequestStart"}

func NewTransport(transport http.RoundTripper) *http.Client {
	retryTransport := rehttp.NewTransport(
		transport,
		rehttp.RetryAll(
			rehttp.RetryMaxRetries(3),
			rehttp.RetryAny(
				rehttp.RetryTemporaryErr(),
				rehttp.RetryStatuses(502, 503),
			),
		),
		rehttp.ExpJitterDelay(100*time.Millisecond, 1*time.Second),
	)

	return &http.Client{
		Transport: &loggingTransport{
			InnerTransport: retryTransport,
		},
	}
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := context.WithValue(req.Context(), contextKeyRequestStart, time.Now())
	req = req.WithContext(ctx)

	resp, err := t.InnerTransport.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	return resp, err
}
