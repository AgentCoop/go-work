package job

// Log record handler
type logHandler func(...interface{})
type logHandlerMapItem struct {
	toggle  bool
	handler logHandler
}

var (
	logHandlersMap  = map[interface{}]*logHandlerMapItem{}
	logDummyHandler = func(...interface{}) { }
)

func RegisterLogger(kind interface{}, handler logHandler, on bool) {
	logHandlersMap[kind] = &logHandlerMapItem{
		toggle:  on,
		handler: handler,
	}
}

func ToggleLogger(kind interface{}, on bool) {
	logHandlersMap[kind].toggle = on
}

func Logger(kind interface{}) logHandler {
	item := logHandlersMap[kind]
	if item.toggle {
		return item.handler
	} else {
		return logDummyHandler
	}
}
