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
