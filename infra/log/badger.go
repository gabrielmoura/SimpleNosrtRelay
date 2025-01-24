package log

import (
	"fmt"

	"go.uber.org/zap"
)

// DefaultLog implementa um logger para badgerDB usando zap.
type DefaultLog struct {
	*zap.Logger
}

// Errorf implementa o log de erro para badgerDB.
func (l *DefaultLog) Errorf(f string, v ...interface{}) {
	message := fmt.Sprintf(f, v...)
	l.Logger.Error(message)
}

// Warningf implementa o log de aviso para badgerDB.
func (l *DefaultLog) Warningf(f string, v ...interface{}) {
	message := fmt.Sprintf(f, v...)
	l.Logger.Warn(message)
}

// Infof implementa o log de informação para badgerDB.
func (l *DefaultLog) Infof(f string, v ...interface{}) {
	message := fmt.Sprintf(f, v...)
	l.Logger.Info(message)
}

// Debugf implementa o log de depuração para badgerDB.
func (l *DefaultLog) Debugf(f string, v ...interface{}) {
	message := fmt.Sprintf(f, v...)
	l.Logger.Debug(message)
}
