// logger: log the errors and the info and stuff :)
package logger

import (
	"fmt"
	"log/slog"
)

func ErrorF(err error, format string, args ...any) {
	slog.Error(fmt.Sprintf("[days-gone-by] "+format, args...), "err", err)
}

func InfoF(format string, args ...any) {
	slog.Info(fmt.Sprintf("[days-gone-by] "+format, args...))
}
