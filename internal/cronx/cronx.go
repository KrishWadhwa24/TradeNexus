// Package cronx adapts zerolog to robfig/cron's panic-recovery job wrapper.
package cronx

import (
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

// Recover returns a cron.JobWrapper that recovers a panicking job and logs
// it instead of letting the panic crash the whole process. robfig/cron does
// not recover panics by default, so every cron.New() in this codebase must
// chain this in.
func Recover(log zerolog.Logger) cron.JobWrapper {
	return cron.Recover(logAdapter{log})
}

// Safe recovers a panic inside fn, logging it instead of letting it crash
// the whole process. Use it around bare goroutines (ticker loops, one-shot
// startup sequences) that aren't cron jobs and so aren't covered by
// Recover's cron.JobWrapper — an unrecovered panic in any goroutine takes
// down the entire process, including every other package's scheduled work.
func Safe(log zerolog.Logger, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("cronx: recovered panic in background goroutine")
		}
	}()
	fn()
}

type logAdapter struct{ log zerolog.Logger }

func (l logAdapter) Info(msg string, kv ...interface{}) {
	l.log.Info().Fields(fields(kv)).Msg(msg)
}

func (l logAdapter) Error(err error, msg string, kv ...interface{}) {
	l.log.Error().Err(err).Fields(fields(kv)).Msg(msg)
}

func fields(kv []interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok {
			m[k] = kv[i+1]
		}
	}
	return m
}
