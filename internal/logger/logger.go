package logger

import (
	"base-fiber-server/internal/common"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gofiber/fiber/v2"
)

var (
	BasePath      = os.Getenv("CONFIG_PATH") + "/runtime/logs"
	ErrorLog      *os.File
	DataLog       *os.File
	once          sync.Once
	errorChannel  chan LogEntry
	errorCtx      context.Context
	errorCancel   context.CancelFunc
	errorWriterWg sync.WaitGroup
	dataChannel   chan []byte
	dataCtx       context.Context
	dataCancel    context.CancelFunc
	dataWriterWg  sync.WaitGroup
	mu            sync.Mutex
)

type LogEntry struct {
	Timestamp string                     `json:"timestamp"`
	Level     string                     `json:"level"`
	Message   string                     `json:"message"`
	Request   common.RequestErrorMessage `json:"request"`
}

func InitLoggers() {
	_ = os.MkdirAll(BasePath, 0755)

	logFiles := []string{"error.log", "data.log"}
	for _, file := range logFiles {
		filePath := filepath.Join(BasePath, file)
		startFileWatcher(filePath)
	}

	reopenLogFiles()
}

func startAsyncErrorWriter(ctx context.Context, ch <-chan LogEntry) {
	defer errorWriterWg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-ch:
			if !ok {
				return
			}
			if ErrorLog != nil {
				data, err := json.Marshal(entry)

				mu.Lock()
				if err != nil {
					row, _ := json.Marshal(entry.Message)
					if _, err := ErrorLog.WriteString("[ERROR] " + string(row) + "\n"); err != nil {
						log.Printf("[logger] write error.log failed: %v\n", err)
					}
				} else {
					if _, err := ErrorLog.WriteString(string(data) + "\n"); err != nil {
						log.Printf("[logger] write error.log failed: %v\n", err)
					}
				}
				mu.Unlock()
			}
		}
	}
}

func startAsyncDateWriter(ctx context.Context, ch <-chan []byte) {
	defer dataWriterWg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case row, ok := <-ch:
			if !ok {
				return
			}
			mu.Lock()
			if DataLog != nil {
				if _, err := DataLog.WriteString(string(row) + "\n"); err != nil {
					log.Printf("[logger] write data.log failed: %v\n", err)
				}
			}
			mu.Unlock()
		}
	}
}

func AsyncError(msg interface{}) {
	entry := LogEntry{
		Timestamp: time.Now().Format(time.RFC3339),
		Level:     "ERROR",
	}

	switch msg := msg.(type) {
	case string:
		entry.Message = msg
	case common.RequestErrorMessage:
		entry.Message = msg.Error
		entry.Request = msg
	default:
		entry.Message = fmt.Sprintf("unknown error type: %T", msg)
		mu.Lock()
		if ErrorLog != nil {
			if _, err := ErrorLog.WriteString("[ERROR] " + entry.Message + "\n"); err != nil {
				log.Printf("[logger] write error.log failed: %v\n", err)
			}
		}
		mu.Unlock()
	}

	select {
	case errorChannel <- entry:
	default:
		dropMsg := map[string]string{
			"timestamp": time.Now().Format(time.RFC3339),
			"level":     "DROP",
			"message":   "error log buffer full",
		}
		row, _ := json.Marshal(dropMsg)

		mu.Lock()
		if ErrorLog != nil {
			if _, err := ErrorLog.WriteString(string(row) + "\n"); err != nil {
				log.Printf("[logger] write error.log failed: %v\n", err)
			}
		}
		mu.Unlock()
	}
}

func AsyncData(data []byte) {
	select {
	case dataChannel <- data:
	default:
		dropMsg := map[string]string{
			"timestamp": time.Now().Format(time.RFC3339),
			"level":     "DROP",
			"message":   "data log buffer full",
		}
		row, _ := json.Marshal(dropMsg)

		mu.Lock()
		if ErrorLog != nil {
			if _, err := ErrorLog.WriteString(string(row) + "\n"); err != nil {
				log.Printf("[logger] write error.log failed: %v\n", err)
			}
		}
		mu.Unlock()
	}
}

func AsyncRequestError(c *fiber.Ctx, code int, err error) {
	msg := common.RequestErrorMessage{
		Time:    time.Now().Format(time.RFC3339),
		Request: fmt.Sprintf("%s %s", c.Method(), c.OriginalURL()),
		Code:    code,
		Error:   err.Error(),
	}

	AsyncError(msg)
}

func Close() {
	once.Do(func() {
		if errorChannel != nil {
			errorCancel()
		}
		if dataChannel != nil {
			dataCancel()
		}

		errorWriterWg.Wait()
		dataWriterWg.Wait()

		if errorCancel != nil {
			close(errorChannel)
			errorCancel = nil
		}
		if dataCancel != nil {
			close(dataChannel)
			dataCancel = nil
		}

		mu.Lock()
		if ErrorLog != nil {
			_ = ErrorLog.Close()
			ErrorLog = nil
		}
		if DataLog != nil {
			_ = DataLog.Close()
			DataLog = nil
		}
		mu.Unlock()
	})
}

func reopenLogFiles() {
	mu.Lock()
	defer mu.Unlock()

	if ErrorLog != nil {
		_ = ErrorLog.Close()
	}
	if DataLog != nil {
		_ = DataLog.Close()
	}

	var err error
	ErrorLog, err = os.OpenFile(BasePath+"/error.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		log.Printf("[logger] Reopen error.log failed: %v\n", err)
	}

	DataLog, err = os.OpenFile(BasePath+"/data.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		log.Printf("[logger] Reopen data.log failed: %v\n", err)
	}

	// stop old writer goroutines
	if dataChannel != nil {
		dataCancel()
		dataWriterWg.Wait()
	}
	if errorChannel != nil {
		errorCancel()
		errorWriterWg.Wait()
	}
	// setup new writer goroutines
	dataChannel = make(chan []byte, 1000)
	dataCtx, dataCancel = context.WithCancel(context.Background())
	dataWriterWg.Add(1)

	errorChannel = make(chan LogEntry, 1000)
	errorCtx, errorCancel = context.WithCancel(context.Background())
	errorWriterWg.Add(1)

	go startAsyncErrorWriter(errorCtx, errorChannel)
	go startAsyncDateWriter(dataCtx, dataChannel)
}

func startFileWatcher(path string) {
	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			log.Printf("[logger] fsnotify watcher failed: %v\n", err)
			return
		}
		defer watcher.Close()

		logDir := filepath.Dir(path)
		if err := watcher.Add(logDir); err != nil {
			log.Printf("[logger] fsnotify add failed: %v\n", err)
			return
		}

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if strings.Contains(event.Name, filepath.Base(path)) {
					if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
						fmt.Printf("[logger] %s changed (%s), reopen triggered\n", event.Name, event.Op)
						time.Sleep(100 * time.Millisecond)
						reopenLogFiles()
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("[logger] fsnotify error: %v\n", err)
			}
		}
	}()
}
