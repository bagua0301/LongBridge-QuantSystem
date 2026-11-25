package logger

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"LongBridge-QuantSystem/config"
)

type HTMLColor string

const (
	ColorDebug   HTMLColor = "#000000"
	ColorInfo    HTMLColor = "#0000FF"
	ColorWarn    HTMLColor = "#FF00FF"
	ColorError   HTMLColor = "#FF0000"
	ColorFatal   HTMLColor = "#8B0000"
	ColorVerbose HTMLColor = "#808080"
	ColorSuccess HTMLColor = "#00FF00"
	ColorTime    HTMLColor = "#888888"
	ColorLevel   HTMLColor = "#000000"
)

type HTMLLogger struct {
	file      *os.File
	mu        sync.Mutex
	appConfig *config.AppConfig

	logChan  chan string
	stopChan chan struct{}
	wg       sync.WaitGroup
}

var htmlLogger *HTMLLogger
var htmlOnce sync.Once

func InitHTMLLogger(appConfig *config.AppConfig) error {
	if !appConfig.HTMLLogEnabled {
		fmt.Println("HTML日志已禁用")
		return nil
	}

	var err error
	htmlOnce.Do(func() {
		if err = os.MkdirAll("./logs", os.ModePerm); err != nil {
			return
		}

		logFileName := "./logs/quant_" + time.Now().Format("2006-01-02_15-04-05") + ".html"
		file, err := os.Create(logFileName)
		if err != nil {
			return
		}

		htmlLogger = &HTMLLogger{
			file:      file,
			appConfig: appConfig,
			logChan:   make(chan string, appConfig.LogChanSize),
			stopChan:  make(chan struct{}),
		}

		htmlLogger.writeHeader()

		htmlLogger.wg.Add(1)
		go htmlLogger.writeLoop()

		fmt.Printf("HTML日志文件: %s\n", logFileName)
		fmt.Printf("HTML日志级别: %s\n", strings.ToUpper(appConfig.HTMLLogLevel))
	})

	return err
}

func GetHTMLLogger() *HTMLLogger {
	return htmlLogger
}

func (h *HTMLLogger) IsEnabled() bool {
	if h == nil || h.file == nil || h.appConfig == nil {
		return false
	}
	return h.appConfig.HTMLLogEnabled
}

func (h *HTMLLogger) ShouldLog(level config.LogLevel) bool {
	if !h.IsEnabled() {
		return false
	}
	return h.appConfig.GetHTMLLogLevel() <= level
}

func (h *HTMLLogger) UpdateConfig(appConfig *config.AppConfig) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.appConfig = appConfig
}

func (h *HTMLLogger) getLevelName(level config.LogLevel) string {
	switch level {
	case config.LogLevelDebug:
		return "DEBUG"
	case config.LogLevelInfo:
		return "INFO"
	case config.LogLevelWarn:
		return "WARN"
	case config.LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

func (h *HTMLLogger) getLevelColor(level config.LogLevel) HTMLColor {
	switch level {
	case config.LogLevelDebug:
		return ColorDebug
	case config.LogLevelInfo:
		return ColorInfo
	case config.LogLevelWarn:
		return ColorWarn
	case config.LogLevelError:
		return ColorError
	default:
		return ColorDebug
	}
}

func (h *HTMLLogger) WriteLog(level config.LogLevel, formattedMsg string) {
	if !h.IsEnabled() {
		return
	}
	if !h.ShouldLog(level) {
		return
	}

	levelName := h.getLevelName(level)
	color := h.getLevelColor(level)
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	html := fmt.Sprintf(`
        <div class="log-entry log-%s">
            <span class="log-time">%s</span>
            <span class="log-level" style="color: %s;">[%s]</span>
            <span class="log-message">%s</span>
        </div>`,
		strings.ToLower(levelName), timestamp, color, levelName, escapeHTML(formattedMsg))

	select {
	case h.logChan <- html:
	default:
	}
}

func (h *HTMLLogger) writeLoop() {
	defer h.wg.Done()

	ticker := time.NewTicker(time.Duration(h.appConfig.LogFlushDuration))
	defer ticker.Stop()

	count := 0
	buf := make([]byte, 0, 64*1024)

	flush := func() {
		if len(buf) > 0 {
			h.file.Write(buf)
			h.file.Sync()
			buf = buf[:0]
			count = 0
		}
	}

	for {
		select {
		case <-h.stopChan:
			for {
				select {
				case html := <-h.logChan:
					buf = append(buf, html...)
					count++
				default:
					flush()
					return
				}
			}
		case html := <-h.logChan:
			buf = append(buf, html...)
			count++
			if count >= h.appConfig.LogFlushCount {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (h *HTMLLogger) writeHeader() {
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>量化交易系统日志</title>
    <style>
        body {
            font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
            background-color: #f5f5f5;
            margin: 0;
            padding: 20px;
        }
        .container {
            max-width: 1400px;
            margin: 0 auto;
            background-color: #ffffff;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 20px;
            text-align: center;
        }
        .header h1 {
            margin: 0;
            font-size: 24px;
        }
        .header p {
            margin: 5px 0 0;
            opacity: 0.9;
            font-size: 12px;
        }
        .controls {
            padding: 15px 20px;
            background-color: #f8f9fa;
            border-bottom: 1px solid #e0e0e0;
            display: flex;
            gap: 10px;
            flex-wrap: wrap;
        }
        .filter-btn {
            padding: 5px 15px;
            border: 1px solid #ddd;
            background: white;
            cursor: pointer;
            border-radius: 4px;
            font-size: 12px;
            transition: all 0.3s;
        }
        .filter-btn:hover {
            background: #e0e0e0;
        }
        .filter-btn.active {
            background: #667eea;
            color: white;
            border-color: #667eea;
        }
        .log-container {
            padding: 20px;
            max-height: 80vh;
            overflow-y: auto;
        }
        .log-entry {
            font-family: 'Consolas', monospace;
            font-size: 12px;
            line-height: 1.5;
            padding: 4px 8px;
            margin: 2px 0;
            border-left: 3px solid transparent;
            white-space: pre-wrap;
            word-break: break-all;
        }
        .log-entry:hover {
            background-color: #f0f0f0;
        }
        .log-time {
            color: #888888;
            margin-right: 15px;
        }
        .log-level {
            display: inline-block;
            min-width: 60px;
            font-weight: bold;
            margin-right: 15px;
        }
        .log-message {
            color: #333;
        }
        .log-debug .log-level { color: #000000; }
        .log-info .log-level { color: #0000FF; }
        .log-warn .log-level { color: #FF00FF; }
        .log-error .log-level { color: #FF0000; }
        .log-fatal .log-level { color: #8B0000; }
        .log-verbose .log-level { color: #808080; }
        
        .log-debug { border-left-color: #000000; }
        .log-info { border-left-color: #0000FF; }
        .log-warn { border-left-color: #FF00FF; }
        .log-error { border-left-color: #FF0000; }
        .log-fatal { border-left-color: #8B0000; }
        .log-verbose { border-left-color: #808080; }
        
        .footer {
            padding: 15px;
            text-align: center;
            background-color: #f8f9fa;
            border-top: 1px solid #e0e0e0;
            font-size: 11px;
            color: #666;
        }
        .stats {
            float: right;
            font-size: 11px;
            color: #666;
        }
        .clearfix::after {
            content: "";
            clear: both;
            display: table;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>📊 量化交易系统日志</h1>
            <p>生成时间: ` + time.Now().Format("2006-01-02 15:04:05") + `</p>
            <p>日志级别: ` + h.getHTMLLogLevelName() + `</p>
        </div>
        <div class="controls">
            <button class="filter-btn active" data-level="all">全部</button>
            <button class="filter-btn" data-level="fatal">🔴 Fatal</button>
            <button class="filter-btn" data-level="error">❌ Error</button>
            <button class="filter-btn" data-level="warn">⚠️ Warn</button>
            <button class="filter-btn" data-level="info">ℹ️ Info</button>
            <button class="filter-btn" data-level="debug">🐛 Debug</button>
            <button class="filter-btn" data-level="verbose">📝 Verbose</button>
            <div class="stats" id="stats"></div>
        </div>
        <div class="log-container" id="logContainer">
`
	h.file.WriteString(html)
}

func (h *HTMLLogger) writeFooter() {
	html := `
        </div>
        <div class="footer">
            <p>量化交易系统 | 日志级别: ` + h.getHTMLLogLevelName() + ` | 详细日志: ` + fmt.Sprintf("%v", h.appConfig.LogEnabled) + `</p>
        </div>
    </div>
    <script>
        function filterLogs(level) {
            const entries = document.querySelectorAll('.log-entry');
            let visibleCount = 0;
            entries.forEach(entry => {
                if (level === 'all' || entry.classList.contains('log-' + level)) {
                    entry.style.display = 'block';
                    visibleCount++;
                } else {
                    entry.style.display = 'none';
                }
            });
            document.getElementById('stats').innerHTML = '显示 ' + visibleCount + ' / ' + entries.length + ' 条日志';
            
            document.querySelectorAll('.filter-btn').forEach(btn => {
                if (btn.dataset.level === level) {
                    btn.classList.add('active');
                } else {
                    btn.classList.remove('active');
                }
            });
        }
        
        document.querySelectorAll('.filter-btn').forEach(btn => {
            btn.addEventListener('click', () => filterLogs(btn.dataset.level));
        });
        
        const totalEntries = document.querySelectorAll('.log-entry').length;
        document.getElementById('stats').innerHTML = '显示 ' + totalEntries + ' / ' + totalEntries + ' 条日志';
    </script>
</body>
</html>`
	h.file.WriteString(html)
}

func (h *HTMLLogger) getHTMLLogLevelName() string {
	if h.appConfig == nil {
		return "INFO"
	}
	switch h.appConfig.GetHTMLLogLevel() {
	case config.LogLevelDebug:
		return "DEBUG"
	case config.LogLevelInfo:
		return "INFO"
	case config.LogLevelWarn:
		return "WARN"
	case config.LogLevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func (h *HTMLLogger) Close() {
	if h == nil || h.file == nil {
		return
	}
	close(h.stopChan)
	h.wg.Wait()
	h.writeFooter()
	h.file.Sync()
	h.file.Close()
}
