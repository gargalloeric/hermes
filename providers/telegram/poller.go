package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gargalloeric/hermes"
)

const (
	defaultLongPollingSec = "60"
	maxBackoff            = 1 * time.Minute
)

// pollerReceiver is the poller implementation for receving Telegram updates.
type pollerReceiver struct {
	token      string
	backoff    time.Duration
	maxRetries int
	offset     int
	updates    chan update
	client     *http.Client
}

func newPoller(token string) *pollerReceiver {
	return &pollerReceiver{
		token:      "bot" + token,
		backoff:    1 * time.Second,
		maxRetries: 2,
		updates:    make(chan update),
		client: &http.Client{
			Timeout: 70 * time.Second,
		},
	}
}

func (p *pollerReceiver) Start(ctx context.Context) error {
	defer close(p.updates)

	failCount := 0

	for {
		updates, err := p.getUpdates(ctx)

		if err != nil {
			failCount++
			delay := p.calculateBackoff(err, failCount)

			if err := p.wait(ctx, delay); err != nil {
				return err
			}
			continue
		}

		failCount = 0
		for _, upd := range updates {
			select {
			case p.updates <- upd:
				p.offset = upd.UpdateID + 1
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if len(updates) == 0 {
			if err := p.wait(ctx, p.backoff); err != nil {
				return err
			}
		}

	}

}

func (p *pollerReceiver) Updates() <-chan update {
	return p.updates
}

func (p *pollerReceiver) getUpdates(ctx context.Context) ([]update, error) {
	endpoint := p.buildEndpoint()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create polling request: %w", err)
	}
	req.Header.Set("User-Agent", hermes.UserAgent())

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp response
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if !apiResp.Ok {
		return nil, p.wrapErrResponse(apiResp)
	}

	return apiResp.Result, nil
}

func (p *pollerReceiver) wrapErrResponse(resp response) error {
	retryAfter := 0
	if resp.Parameters != nil {
		retryAfter = resp.Parameters.RetryAfter
	}

	return &apiError{
		Message:    resp.Description,
		RetryAfter: time.Duration(retryAfter) * time.Second,
	}
}

func (p *pollerReceiver) buildEndpoint() string {
	params := url.Values{}
	params.Add("offset", strconv.Itoa(p.offset))
	params.Add("timeout", defaultLongPollingSec)
	params.Add("allowed_updates", `["message","edited_message","chat_member"]`)

	endpoint := fmt.Sprintf("%s/%s/getUpdates?%s", apiBase, p.token, params.Encode())

	return endpoint
}

func (p *pollerReceiver) calculateBackoff(err error, fails int) time.Duration {
	tgError, ok := errors.AsType[*apiError](err)
	if ok && tgError.RetryAfter > 0 {
		return tgError.RetryAfter
	}

	// Exponential backoff for other errors: 2s, 4s, 8s...
	delay := time.Second << uint(fails-1)
	if delay > maxBackoff || delay <= 0 { // delay <= 0 checks for overflow
		return maxBackoff
	}
	return delay
}

func (p pollerReceiver) wait(ctx context.Context, delay time.Duration) error {
	if delay == 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
