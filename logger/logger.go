package logger

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"LongBridge-QuantSystem/config"
)

// LevelLogger 级别日志器
type LevelLogger struct {
	appConfig *config.AppConfig
	infoLog   *log.Logger
	warnLog   *log.Logger
	errorLog  *log.Logger
	debugLog  *log.Logger
	file      *os.File

	// CMD日志输出
	cmdInfoLog  *log.Logger
	cmdWarnLog  *log.Logger
	cmdErrorLog *log.Logger
	cmdDebugLog *log.Logger

	mu sync.RWMutex // 保护配置读取
}

var globalLogger *LevelLogger

// InitLogger 初始化全局日志器
func InitLogger(appConfig *config.AppConfig) error {
	logger, err := NewLevelLogger(appConfig)
	if err != nil {
		return err
	}
	globalLogger = logger

	// 初始化HTML日志
	if err := InitHTMLLogger(appConfig); err != nil {
		log.Printf("Warning: Failed to init HTML logger: %v", err)
	}

	return nil
}

// GetLogger 获取全局日志器
func GetLogger() *LevelLogger {
	return globalLogger
}

// CloseLogger 关闭所有日志器
func CloseLogger() {
	if globalLogger != nil {
		globalLogger.Close()
	}
	if htmlLogger != nil {
		htmlLogger.Close()
	}
}

// NewLevelLogger 创建级别日志器
func NewLevelLogger(appConfig *config.AppConfig) (*LevelLogger, error) {
	// 创建日志目录
	if err := os.MkdirAll("./logs", os.ModePerm); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %v", err)
	}

	strTime := time.Now().Format("2006-01-02_15-04-05")
	// 创建日志文件
	logFileName := "./logs/quant_" + strTime + ".log"
	file, err := os.OpenFile(logFileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0766)
	if err != nil {
		return nil, fmt.Errorf("创建日志文件失败: %v", err)
	}

	// 设置log名字
	appConfig.SetLogFileName(strTime)

	// 日志标志
	logFlags := log.LstdFlags

	l := &LevelLogger{
		appConfig:   appConfig,
		infoLog:     log.New(file, "[INFO] ", logFlags),
		warnLog:     log.New(file, "[WARN] ", logFlags),
		errorLog:    log.New(file, "[ERROR] ", logFlags),
		debugLog:    log.New(file, "[DEBUG] ", logFlags),
		file:        file,
		cmdInfoLog:  log.New(os.Stdout, "[INFO] ", logFlags),
		cmdWarnLog:  log.New(os.Stdout, "[WARN] ", logFlags),
		cmdErrorLog: log.New(os.Stdout, "[ERROR] ", logFlags),
		cmdDebugLog: log.New(os.Stdout, "[DEBUG] ", logFlags),
	}

	// 输出启动信息
	l.printStartupInfo(logFileName)

	return l, nil
}

// printStartupInfo 输出启动信息
func (l *LevelLogger) printStartupInfo(logFileName string) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	fmt.Printf("\n========================================\n")
	fmt.Printf("日志系统初始化完成\n")
	fmt.Printf("日志文件: %s\n", logFileName)
	fmt.Printf("文件日志级别: %s\n", strings.ToUpper(l.appConfig.LogLevel))
	fmt.Printf("CMD日志级别: %s\n", strings.ToUpper(l.appConfig.CmdLevel))
	fmt.Printf("CMD日志启用: %v\n", l.appConfig.CmdEnabled)
	fmt.Printf("详细日志: %v\n", l.appConfig.LogEnabled)
	fmt.Printf("========================================\n\n")
}

// Close 关闭日志文件
func (l *LevelLogger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

// getCaller 获取调用者信息（跳过 skip 层）
func getCaller(skip int) (string, int) {
	_, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return "unknown", 0
	}
	parts := strings.Split(file, "/")
	fileName := parts[len(parts)-1]
	return fileName, line
}

// formatMessage 格式化消息（统一格式化）
func formatMessage(skip int, format string, v ...interface{}) (string, string, int) {
	msg := fmt.Sprintf(format, v...)
	fileName, line := getCaller(skip + 1)
	formattedMsg := fmt.Sprintf("%s:%d: %s", fileName, line, msg)
	return formattedMsg, fileName, line
}

// shouldLogFile 判断是否需要记录文件日志
func (l *LevelLogger) shouldLogFile(level config.LogLevel) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.appConfig.LogEnabled && l.appConfig.GetLogLevel() <= level
}

// shouldLogCmd 判断是否需要记录CMD日志
func (l *LevelLogger) shouldLogCmd(level config.LogLevel) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.appConfig.CmdEnabled && l.appConfig.GetCmdLevel() <= level
}

// shouldLogHTML 判断是否需要记录HTML日志
func (l *LevelLogger) shouldLogHTML(level config.LogLevel) bool {
	if htmlLogger == nil {
		return false
	}
	return htmlLogger.ShouldLog(level)
}

// shouldLog 判断是否需要记录日志（检查至少一个输出目标需要记录）
func (l *LevelLogger) shouldLog(level config.LogLevel) bool {
	return l.shouldLogFile(level) || l.shouldLogCmd(level) || l.shouldLogHTML(level)
}

// UpdateConfig 动态更新配置（运行时修改日志级别）
func (l *LevelLogger) UpdateConfig(appConfig *config.AppConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.appConfig = appConfig

	// 同步更新HTML日志配置
	if htmlLogger != nil {
		htmlLogger.UpdateConfig(appConfig)
	}

	// 输出配置变更信息
	fmt.Printf("[LOG] 日志配置已更新 - 文件级别: %s, CMD级别: %s, CMD启用: %v\n",
		strings.ToUpper(appConfig.LogLevel),
		strings.ToUpper(appConfig.CmdLevel),
		appConfig.CmdEnabled)
}

// GetConfig 获取当前配置（只读）
func (l *LevelLogger) GetConfig() *config.AppConfig {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.appConfig
}

// Debug 调试日志（仅 DEBUG 级别输出）
func (l *LevelLogger) Debug(format string, v ...interface{}) {
	if !l.shouldLog(config.LogLevelDebug) {
		return
	}

	formattedMsg, _, _ := formatMessage(2, format, v...)

	// 文件输出
	if l.shouldLogFile(config.LogLevelDebug) {
		l.debugLog.Print(formattedMsg)
	}

	// CMD输出
	if l.shouldLogCmd(config.LogLevelDebug) {
		l.cmdDebugLog.Print(formattedMsg)
	}

	// HTML输出
	if l.shouldLogHTML(config.LogLevelDebug) {
		htmlLogger.WriteLog(config.LogLevelDebug, formattedMsg)
	}
}

// Info 信息日志（INFO 及以上级别输出）
func (l *LevelLogger) Info(format string, v ...interface{}) {
	if !l.shouldLog(config.LogLevelInfo) {
		return
	}

	formattedMsg, _, _ := formatMessage(2, format, v...)

	// 文件输出
	if l.shouldLogFile(config.LogLevelInfo) {
		l.infoLog.Print(formattedMsg)
	}

	// CMD输出
	if l.shouldLogCmd(config.LogLevelInfo) {
		l.cmdInfoLog.Print(formattedMsg)
	}

	// HTML输出
	if l.shouldLogHTML(config.LogLevelInfo) {
		htmlLogger.WriteLog(config.LogLevelInfo, formattedMsg)
	}
}

// Warn 警告日志（WARN 及以上级别输出）
func (l *LevelLogger) Warn(format string, v ...interface{}) {
	if !l.shouldLog(config.LogLevelWarn) {
		return
	}

	formattedMsg, _, _ := formatMessage(2, format, v...)

	// 文件输出
	if l.shouldLogFile(config.LogLevelWarn) {
		l.warnLog.Print(formattedMsg)
	}

	// CMD输出
	if l.shouldLogCmd(config.LogLevelWarn) {
		l.cmdWarnLog.Print(formattedMsg)
	}

	// HTML输出
	if l.shouldLogHTML(config.LogLevelWarn) {
		htmlLogger.WriteLog(config.LogLevelWarn, formattedMsg)
	}
}

// Error 错误日志（ERROR 及以上级别输出）
func (l *LevelLogger) Error(format string, v ...interface{}) {
	if !l.shouldLog(config.LogLevelError) {
		return
	}

	formattedMsg, _, _ := formatMessage(2, format, v...)

	// 文件输出
	if l.shouldLogFile(config.LogLevelError) {
		l.errorLog.Print(formattedMsg)
	}

	// CMD输出
	if l.shouldLogCmd(config.LogLevelError) {
		l.cmdErrorLog.Print(formattedMsg)
	}

	// HTML输出
	if l.shouldLogHTML(config.LogLevelError) {
		htmlLogger.WriteLog(config.LogLevelError, formattedMsg)
	}
}

// Fatal 致命错误（总是输出并退出）
func (l *LevelLogger) Fatal(format string, v ...interface{}) {
	formattedMsg, _, _ := formatMessage(2, format, v...)

	// 文件输出
	l.errorLog.Print(formattedMsg)

	// CMD输出（Fatal总是输出，如果启用CMD）
	if l.shouldLogCmd(config.LogLevelError) {
		l.cmdErrorLog.Print(formattedMsg)
	}

	// HTML输出
	if htmlLogger != nil {
		htmlLogger.WriteLog(config.LogLevelError, formattedMsg)
	}

	os.Exit(1)
}

// Verbose 详细日志（受 logEnabled 和日志级别双重控制）
func (l *LevelLogger) Verbose(format string, v ...interface{}) {
	// Verbose 特殊处理：需要 logEnabled 为 true
	l.mu.RLock()
	logEnabled := l.appConfig.LogEnabled
	l.mu.RUnlock()

	if !logEnabled {
		return
	}

	if !l.shouldLog(config.LogLevelDebug) {
		return
	}

	formattedMsg, _, _ := formatMessage(2, "[VERBOSE] "+format, v...)

	// 文件输出
	if l.shouldLogFile(config.LogLevelDebug) {
		l.debugLog.Print(formattedMsg)
	}

	// CMD输出
	if l.shouldLogCmd(config.LogLevelDebug) {
		l.cmdDebugLog.Print(formattedMsg)
	}

	// HTML输出
	if l.shouldLogHTML(config.LogLevelDebug) {
		htmlLogger.WriteLog(config.LogLevelDebug, formattedMsg)
	}
}

// 全局便捷函数

func Debug(format string, v ...interface{}) {
	if globalLogger != nil {
		globalLogger.Debug(format, v...)
	}
}

func Info(format string, v ...interface{}) {
	if globalLogger != nil {
		globalLogger.Info(format, v...)
	}
}

func Warn(format string, v ...interface{}) {
	if globalLogger != nil {
		globalLogger.Warn(format, v...)
	}
}

func Error(format string, v ...interface{}) {
	if globalLogger != nil {
		globalLogger.Error(format, v...)
	}
}

func Fatal(format string, v ...interface{}) {
	if globalLogger != nil {
		globalLogger.Fatal(format, v...)
	}
}

func Verbose(format string, v ...interface{}) {
	if globalLogger != nil {
		globalLogger.Verbose(format, v...)
	}
}

// UpdateGlobalConfig 动态更新全局配置
func UpdateGlobalConfig(appConfig *config.AppConfig) {
	if globalLogger != nil {
		globalLogger.UpdateConfig(appConfig)
	}
}

// GetGlobalConfig 获取全局配置
func GetGlobalConfig() *config.AppConfig {
	if globalLogger != nil {
		return globalLogger.GetConfig()
	}
	return nil
}
