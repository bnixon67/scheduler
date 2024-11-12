package scheduler

import (
	"log/slog"
	"testing"
)

func TestNewWorkers(t *testing.T) {
	bufferSize := 42
	logger := slog.Default()

	got := newWorkers(bufferSize, logger)

	wantJobQueueCh := make(chan *Job, bufferSize)
	if cap(got.jobQueueCh) != cap(wantJobQueueCh) {
		t.Errorf("cap(jobQueueCh) = %d, want %d",
			cap(got.jobQueueCh), cap(wantJobQueueCh))
	}

	if got.ctx == nil {
		t.Errorf("ctx is nil")
	}

	if got.cancel == nil {
		t.Errorf("cancel is nil")
	}

	if got.logger != slog.Default() {
		t.Errorf("logger incorrect")
	}

}
