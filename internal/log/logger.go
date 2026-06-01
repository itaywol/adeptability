package log

// Nop returns a Logger that discards every record. Used by tests and by
// orchestration code that wants logging to be optional.
func Nop() Logger { return nopLogger{} }

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}
func (n nopLogger) With(...any) Logger { return n }
