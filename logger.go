package job

import "sync"

// Log record handler
type LogHandler func(...interface{})
type logHandlerMapItem struct {
	toggle  bool
	handler LogHandler
	mu      sync.Mutex
}

var (
	logHandlersMap  = map[interface{}]*logHandlerMapItem{}
	logDummyHandler = func(...interface{}) {}
)

func RegisterLogger(kind interface{}, handler LogHandler, on bool) {
	item := &logHandlerMapItem{}
	item.toggle = on
	item.handler = func(args...interface{}) {
		item.mu.Lock()
		defer item.mu.Unlock()
		handler(args...)
	}
	logHandlersMap[kind] = item
}

func ToggleLogger(kind interface{}, on bool) {
	logHandlersMap[kind].toggle = on
}

func Logger(kind interface{}) LogHandler {
	item := logHandlersMap[kind]
	if item.toggle {
		return item.handler
	} else {
		return logDummyHandler
	}
}
