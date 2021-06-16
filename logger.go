package job

func RegisterDefaultLogger(logger Logger) {
	m := logger()
	defaultLogger = m
	for level, item := range m {
		logchan := item.ch
		handler := item.rechandler
		l := level
		go func() {
			for {
				select {
				case entry := <- logchan:
					handler(entry, l)
				}
			}
		}()
	}
}
