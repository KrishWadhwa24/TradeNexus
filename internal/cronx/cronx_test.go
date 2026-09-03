package cronx

import (
	"io"
	"testing"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

// TestRecoverSwallowsPanic proves a job wrapped with Recover doesn't
// propagate a panic to the caller (this is the whole point of wiring
// cron.WithChain(cronx.Recover(...)) into every cron.New() call: without it,
// robfig/cron lets a job panic crash the entire process).
func TestRecoverSwallowsPanic(t *testing.T) {
	log := zerolog.New(io.Discard)
	wrap := Recover(log)

	panicked := false
	job := wrap(cron.FuncJob(func() { panic("boom") }))

	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		job.Run()
	}()

	if panicked {
		t.Fatal("Recover did not swallow the panic")
	}

	ranOK := false
	okJob := wrap(cron.FuncJob(func() { ranOK = true }))
	okJob.Run()
	if !ranOK {
		t.Fatal("Recover should still run a non-panicking job normally")
	}
}
