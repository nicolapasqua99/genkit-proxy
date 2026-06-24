package proxy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/firebase/genkit/go/core"
)

// seqFakeResponse is one canned result for seqFakeGen.
type seqFakeResponse struct {
	resp           GenerateResponse
	err            error
	chunkBeforeErr bool // GenerateStream: call onChunk("x") before returning err
}

// seqFakeGen returns a preset sequence of responses, reusing the last entry
// once the sequence is exhausted.
type seqFakeGen struct {
	mu        sync.Mutex
	responses []seqFakeResponse
	callCount int
}

func (f *seqFakeGen) next() seqFakeResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := f.callCount
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	f.callCount++
	return f.responses[idx]
}

func (f *seqFakeGen) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

func (f *seqFakeGen) Generate(_ context.Context, _ GenerateRequest, _ string) (GenerateResponse, error) {
	r := f.next()
	return r.resp, r.err
}

func (f *seqFakeGen) GenerateStream(_ context.Context, _ GenerateRequest, _ string, onChunk func(string) error) (GenerateResponse, error) {
	r := f.next()
	if r.chunkBeforeErr && r.err != nil {
		if err := onChunk("x"); err != nil {
			return GenerateResponse{}, err
		}
	}
	return r.resp, r.err
}

var (
	retryMinReq = GenerateRequest{ModelName: "googleai/gemini", UserMessage: "hi"}

	retryErrRateLimit = &core.GenkitError{Status: core.RESOURCE_EXHAUSTED, Message: "test"}
	errRetryUpstream  = errors.New("upstream error") // classifies as categoryUpstream
	retryErrAuth      = &core.GenkitError{Status: core.UNAUTHENTICATED, Message: "test"}
	retryErrValid     = &ValidationError{Field: "f", Reason: "r"}
)

func TestRetryingGenerator(t *testing.T) {
	t.Run("success_on_first_attempt", func(t *testing.T) {
		fake := &seqFakeGen{responses: []seqFakeResponse{{resp: GenerateResponse{Output: "hi"}}}}
		gen := NewRetryingGenerator(fake, 3, 0)
		resp, err := gen.Generate(context.Background(), retryMinReq, "key")
		if err != nil {
			t.Fatal(err)
		}
		if resp.Output != "hi" {
			t.Errorf("output = %q, want %q", resp.Output, "hi")
		}
		if fake.calls() != 1 {
			t.Errorf("calls = %d, want 1", fake.calls())
		}
	})

	t.Run("retry_on_rate_limit", func(t *testing.T) {
		fake := &seqFakeGen{responses: []seqFakeResponse{
			{err: retryErrRateLimit},
			{resp: GenerateResponse{Output: "ok"}},
		}}
		gen := NewRetryingGenerator(fake, 3, 0)
		resp, err := gen.Generate(context.Background(), retryMinReq, "key")
		if err != nil {
			t.Fatal(err)
		}
		if resp.Output != "ok" {
			t.Errorf("output = %q, want %q", resp.Output, "ok")
		}
		if fake.calls() != 2 {
			t.Errorf("calls = %d, want 2", fake.calls())
		}
	})

	t.Run("retry_on_upstream", func(t *testing.T) {
		fake := &seqFakeGen{responses: []seqFakeResponse{
			{err: errRetryUpstream},
			{resp: GenerateResponse{Output: "ok"}},
		}}
		gen := NewRetryingGenerator(fake, 3, 0)
		_, err := gen.Generate(context.Background(), retryMinReq, "key")
		if err != nil {
			t.Fatal(err)
		}
		if fake.calls() != 2 {
			t.Errorf("calls = %d, want 2", fake.calls())
		}
	})

	t.Run("no_retry_on_timeout", func(t *testing.T) {
		for _, timeoutErr := range []error{context.DeadlineExceeded, context.Canceled} {
			fake := &seqFakeGen{responses: []seqFakeResponse{{err: timeoutErr}}}
			gen := NewRetryingGenerator(fake, 3, 0)
			_, err := gen.Generate(context.Background(), retryMinReq, "key")
			if err == nil {
				t.Fatalf("%v: expected error", timeoutErr)
			}
			if fake.calls() != 1 {
				t.Errorf("%v: calls = %d, want 1 (timeouts not retried)", timeoutErr, fake.calls())
			}
		}
	})

	t.Run("no_retry_on_unauthenticated", func(t *testing.T) {
		fake := &seqFakeGen{responses: []seqFakeResponse{{err: retryErrAuth}}}
		gen := NewRetryingGenerator(fake, 3, 0)
		_, err := gen.Generate(context.Background(), retryMinReq, "key")
		if err == nil {
			t.Fatal("expected error")
		}
		if fake.calls() != 1 {
			t.Errorf("calls = %d, want 1 (no retry for auth error)", fake.calls())
		}
	})

	t.Run("no_retry_on_validation", func(t *testing.T) {
		fake := &seqFakeGen{responses: []seqFakeResponse{{err: retryErrValid}}}
		gen := NewRetryingGenerator(fake, 3, 0)
		_, err := gen.Generate(context.Background(), retryMinReq, "key")
		if err == nil {
			t.Fatal("expected error")
		}
		if fake.calls() != 1 {
			t.Errorf("calls = %d, want 1 (no retry for validation error)", fake.calls())
		}
	})

	t.Run("exhausts_max_attempts", func(t *testing.T) {
		fake := &seqFakeGen{responses: []seqFakeResponse{{err: retryErrRateLimit}}}
		gen := NewRetryingGenerator(fake, 3, 0)
		_, err := gen.Generate(context.Background(), retryMinReq, "key")
		if err == nil {
			t.Fatal("expected error after exhausting attempts")
		}
		if fake.calls() != 3 {
			t.Errorf("calls = %d, want 3 (maxAttempts)", fake.calls())
		}
	})

	t.Run("context_canceled_during_sleep", func(t *testing.T) {
		// A very long backoff ensures the select fires on ctx.Done, not the timer.
		fake := &seqFakeGen{responses: []seqFakeResponse{{err: errRetryUpstream}}}
		gen := NewRetryingGenerator(fake, 3, time.Hour)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := gen.Generate(ctx, retryMinReq, "key")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})

	t.Run("disabled_when_max_attempts_le_1", func(t *testing.T) {
		fake := &seqFakeGen{responses: []seqFakeResponse{{err: retryErrRateLimit}}}
		gen := NewRetryingGenerator(fake, 1, 0)
		_, err := gen.Generate(context.Background(), retryMinReq, "key")
		if err == nil {
			t.Fatal("expected error")
		}
		if fake.calls() != 1 {
			t.Errorf("calls = %d, want 1 (retry disabled with maxAttempts=1)", fake.calls())
		}
	})

	t.Run("stream_retries_before_first_chunk", func(t *testing.T) {
		fake := &seqFakeGen{responses: []seqFakeResponse{
			{err: retryErrRateLimit, chunkBeforeErr: false},
			{resp: GenerateResponse{Output: "ok"}},
		}}
		gen := NewRetryingGenerator(fake, 3, 0)
		_, err := gen.GenerateStream(context.Background(), retryMinReq, "key", func(string) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if fake.calls() != 2 {
			t.Errorf("calls = %d, want 2 (retried before first chunk)", fake.calls())
		}
	})

	t.Run("stream_no_retry_after_first_chunk", func(t *testing.T) {
		fake := &seqFakeGen{responses: []seqFakeResponse{
			{err: retryErrRateLimit, chunkBeforeErr: true},
		}}
		gen := NewRetryingGenerator(fake, 3, 0)
		var chunks []string
		_, err := gen.GenerateStream(context.Background(), retryMinReq, "key", func(delta string) error {
			chunks = append(chunks, delta)
			return nil
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if fake.calls() != 1 {
			t.Errorf("calls = %d, want 1 (no retry after chunk sent)", fake.calls())
		}
		if len(chunks) != 1 {
			t.Errorf("chunks = %d, want 1", len(chunks))
		}
	})
}
