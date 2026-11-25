package config

import (
	"log"
	"path/filepath"
	"strings"
	"sync"

	"LongBridge-QuantSystem/utils"

	"github.com/joho/godotenv"
)

// LogLevel 日志级别类型
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelNone
)

// `config:"ro"`        // 只读，不可通过命令修改
// `config:"rw"`        // 可读写

// AppConfig 应用配置
type AppConfig struct {
	// 长桥 API 配置
	AppKey      string `flag:"ro"`
	AppSecret   string `flag:"ro"`
	AccessToken string `flag:"ro"`

	// 交易标的配置
	Symbols []string `flag:"Com"`

	// 交易时段配置
	TradingSessions int `flag:"Com"`

	// 订单配置
	OrderType       string  `flag:"Com"`
	OrderMode       string  `flag:"Com"`
	SlippagePercent float64 `flag:"Com"`
	OrderTimeOut    int     `flag:"Com"`
	OrderCheckTime  int     `flag:"Com"`

	// 资金管理配置
	MinQuantity int64   `flag:"Com"`
	MaxQuantity int64   `flag:"Com"`
	BaseAmount  float64 `flag:"Com"`

	// 风控配置
	MaxDailyTrades     int     `flag:"Com"`
	MaxPositionPercent float64 `flag:"Com"`
	StopLossPercent    float64 `flag:"Com"`

	// RSI相关配置
	StrategyTotalCapital     float64 `flag:"Com"`
	StrategyMaxLossRatio     float64 `flag:"Com"`
	StrategyMaxPositionRatio float64 `flag:"Com"`

	// 默认策略
	StrategyType                      []string `flag:"Com"`
	StrategyUnSoldOrderNum            int      `flag:"Com"`
	StrategySessionPreFirstMinute     int      `flag:"Com"`
	StrategySessionPreLastMinute      int      `flag:"Com"`
	StrategySessionRegularFirstMinute int      `flag:"Com"`
	StrategySessionRegularLastMinute  int      `flag:"Com"`
	StrategySessionAfterFirstMinute   int      `flag:"Com"`
	StrategySessionAfterLastMinute    int      `flag:"Com"`
	StrategyPreQuantityStep           int64    `flag:"Com"`
	StrategyRegularQuantityStep       int64    `flag:"Com"`
	StrategyAfterQuantityStep         int64    `flag:"Com"`
	StrategyRegularFirstMinute        int      `flag:"Com"`
	StrategyRegularAfterMinute        int      `flag:"Com"`
	StrategyOrderLogicSec             int64    `flag:"Com"`
	StrategyContinueLossSwitch        bool     `flag:"Com"`
	StrategyContinueLossNum           int      `flag:"Com"`
	StrategyContinueLossStopMinute    int      `flag:"Com"`
	StrategyOvernightExitFirstMinute  int      `flag:"Com"`

	SecondHistoryLimit    int `flag:"Com"`
	MinuteHistoryLimit    int `flag:"Com"`
	MACDMinuteUpdateCount int `flag:"Com"`
	MACDSecondUpdateCount int `flag:"Com"`

	OpenCooldown int `flag:"Com"`
	MACDFast     int `flag:"Com"`
	MACDSlow     int `flag:"Com"`
	MACDSignal   int `flag:"Com"`

	SignalType                    int     `flag:"TimeSignal"`
	SignalCurrentSec              int     `flag:"TimeSignal"`
	SignalRSIOverBuy              float64 `flag:"TimeSignal"`
	SignalRSIOverSell             float64 `flag:"TimeSignal"`
	SignalLongPresent             float64 `flag:"TimeSignal"`
	SignalShortPresent            float64 `flag:"TimeSignal"`
	SignalWindow                  int     `flag:"TimeSignal"`
	SignalRSI                     int     `flag:"TimeSignal"`
	SignalRSIBase                 int     `flag:"TimeSignal"`
	SignalATR                     int     `flag:"TimeSignal"`
	SignalATRNoiseThresholdFactor float64 `flag:"TimeSignal"`
	SignalATRNoiseThresholdMin    float64 `flag:"TimeSignal"`
	SignalVolumeDynamic           int     `flag:"TimeSignal"`
	SignalVolumeFactor            float64 `flag:"TimeSignal"`

	TrendPriceType          int     `flag:"TimeTrend"`
	TrendMALong             int     `flag:"TimeTrend"`
	TrendMAShort            int     `flag:"TimeTrend"`
	TrendMAPre              int     `flag:"TimeTrend"`
	TrendConfirm            int     `flag:"TimeTrend"`
	TrendFlowPeriod         int     `flag:"TimeTrend"`
	TrendFlowEntryThreshold float64 `flag:"TimeTrend"`
	TrendFlowExitThreshold  float64 `flag:"TimeTrend"`
	TrendAtrFactor          float64 `flag:"TimeTrend"`
	TrendVolumeFactor       float64 `flag:"TimeTrend"`
	TrendVolumeMaxFactor    float64 `flag:"TimeTrend"`
	TrendStopMaxLoss        float64 `flag:"TimeTrend"`
	TrendATRLongStopBase    float64 `flag:"TimeTrend"`
	TrendATRShortStopBase   float64 `flag:"TimeTrend"`
	TrendATRLongWinBase     float64 `flag:"TimeTrend"`
	TrendATRShortWinBase    float64 `flag:"TimeTrend"`

	// 网格公共配置
	GridPriceType    int `flag:"GridCom"`
	GridWaitDuration int `flag:"GridCom"`
	GridATR          int `flag:"GridCom"`
	GridMAPre        int `flag:"GridCom"`

	// 止盈止损配置
	GridATRLongStopBase      float64 `flag:"Grid"`
	GridATRShortStopBase     float64 `flag:"Grid"`
	GridATRLongWinBase       float64 `flag:"Grid"`
	GridATRShortWinBase      float64 `flag:"Grid"`
	GridMoveLongMaxMultiple  float64 `flag:"Grid"`
	GridMoveShortMaxMultiple float64 `flag:"Grid"`
	GridMoveProfitPercent    float64 `flag:"Grid"`
	GridMoveBasePercent      float64 `flag:"Grid"`
	GridMovePercent          float64 `flag:"Grid"`

	// 策略特定配置 - GridPercent
	GridPercentStep float64 `flag:"GridPercent"`

	// 策略特定配置 - Test
	TestBuyQuantity int64 `flag:"TestOrder"`
	TestHoldSeconds int64 `flag:"TestOrder"`

	// 日志配置
	LogFileName      string `flag:"Log"`
	LogChanSize      int    `flag:"Log"`
	LogFlushCount    int    `flag:"Log"`
	LogFlushDuration int    `flag:"Log"`

	LogLevel       string `flag:"Log"`
	LogEnabled     bool   `flag:"Log"`
	HTMLLogLevel   string `flag:"Log"`
	HTMLLogEnabled bool   `flag:"Log"`
	CmdLevel       string `flag:"Log"`
	CmdEnabled     bool   `flag:"Log"`

	// 时区配置
	TimeZone string `flag:"Time"`

	// 策略开启测试
	TestOpen            bool    `flag:"Test"`
	TestStrategy        string  `flag:"Test"`
	TestOpenDraw        bool    `flag:"Test"`
	TestXlsx            string  `flag:"Test"`
	TestTimeTicker      int     `flag:"Test"`
	TestTimeScale       float64 `flag:"Test"`
	TestPriceIndexStart int     `flag:"Test"`
	TestPriceIndexType  int     `flag:"Test"`
	TestPriceIndexEnd   int     `flag:"Test"`
}

var (
	globalConfig *AppConfig
	configOnce   sync.Once
)

// GetConfig 获取全局配置（单例）
func GetConfig() *AppConfig {
	configOnce.Do(func() {
		var err error
		globalConfig, err = LoadConfig()
		if err != nil {
			log.Fatalf("加载配置失败: %v", err)
		}
	})
	return globalConfig
}

// IsTestMode 判断是否测试模式（全局函数）
func IsTestMode() bool {
	cfg := GetConfig()
	return cfg != nil && cfg.TestOpen
}

// GetLogLevel 获取文件日志级别
func (c *AppConfig) GetLogLevel() LogLevel {
	switch strings.ToUpper(c.LogLevel) {
	case "DEBUG":
		return LogLevelDebug
	case "INFO":
		return LogLevelInfo
	case "WARN":
		return LogLevelWarn
	case "ERROR":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

// GetCmdLevel 获取CMD日志级别
func (c *AppConfig) GetCmdLevel() LogLevel {
	switch strings.ToUpper(c.CmdLevel) {
	case "DEBUG":
		return LogLevelDebug
	case "INFO":
		return LogLevelInfo
	case "WARN":
		return LogLevelWarn
	case "ERROR":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

// GetHTMLLogLevel 获取HTML日志级别
func (c *AppConfig) GetHTMLLogLevel() LogLevel {
	switch strings.ToUpper(c.HTMLLogLevel) {
	case "DEBUG":
		return LogLevelDebug
	case "INFO":
		return LogLevelInfo
	case "WARN":
		return LogLevelWarn
	case "ERROR":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

func (c *AppConfig) SetLogFileName(logFileName string) {
	c.LogFileName = logFileName
}

// SetLogLevel 动态设置文件日志级别
func (c *AppConfig) SetLogLevel(level string) {
	c.LogLevel = level
}

// SetCmdLevel 动态设置CMD日志级别
func (c *AppConfig) SetCmdLevel(level string) {
	c.CmdLevel = level
}

// SetCmdEnabled 动态设置CMD日志开关
func (c *AppConfig) SetCmdEnabled(enabled bool) {
	c.CmdEnabled = enabled
}

// SetHTMLLogLevel 动态设置HTML日志级别
func (c *AppConfig) SetHTMLLogLevel(level string) {
	c.HTMLLogLevel = level
}

// SetHTMLLogEnabled 动态设置HTML日志开关
func (c *AppConfig) SetHTMLLogEnabled(enabled bool) {
	c.HTMLLogEnabled = enabled
}

// SetLogEnabled 动态设置文件日志开关
func (c *AppConfig) SetLogEnabled(enabled bool) {
	c.LogEnabled = enabled
}

// LoadConfig 加载配置
func LoadConfig() (*AppConfig, error) {
	// 加载 .env 文件
	configPath := "config.env"
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(utils.GetExecpath(), configPath)
	}
	if err := godotenv.Load(configPath); err != nil {
		log.Printf("Warning: Could not load config.env: %v, using environment variables", err)
	}

	cfg := &AppConfig{
		// 长桥 API 配置
		AppKey:      utils.GetEnv("LONGPORT_APP_KEY", ""),
		AppSecret:   utils.GetEnv("LONGPORT_APP_SECRET", ""),
		AccessToken: utils.GetEnv("LONGBRIDGE_ACCESS_TOKEN", ""),

		// 交易标的
		Symbols: utils.GetEnvSymbols("Symbols", []string{"SOXL.US"}),

		// 交易时段
		TradingSessions: utils.GetEnvInt("TradingSessions", 1),

		// 订单配置
		OrderType:       utils.GetEnv("OrderType", "MARKET"),
		OrderMode:       utils.GetEnv("OrderMode", "QUANTITY"),
		SlippagePercent: utils.GetEnvFloat64("SlippagePercent", 0.5),
		OrderTimeOut:    utils.GetEnvInt("OrderTimeOut", 15),
		OrderCheckTime:  utils.GetEnvInt("OrderCheckTime", 3),

		// 资金管理
		MinQuantity: utils.GetEnvInt64("MinQuantity", 1),
		MaxQuantity: utils.GetEnvInt64("MaxQuantity", 100),
		BaseAmount:  utils.GetEnvFloat64("BaseAmount", 10000),

		// 风控
		MaxDailyTrades:     utils.GetEnvInt("MaxDailyTrades", 20),
		MaxPositionPercent: utils.GetEnvFloat64("MaxPositionPercent", 30),
		StopLossPercent:    utils.GetEnvFloat64("StopLossPercent", 10),

		// RSI相关配置
		StrategyTotalCapital:     utils.GetEnvFloat64("StrategyTotalCapital", 10000),
		StrategyMaxLossRatio:     utils.GetEnvFloat64("StrategyMaxLossRatio", 0.15),
		StrategyMaxPositionRatio: utils.GetEnvFloat64("StrategyMaxPositionRatio", 0.3),

		// 默认策略
		StrategyType:                      utils.GetEnvSymbols("StrategyType", []string{"GridPercent"}),
		StrategyUnSoldOrderNum:            utils.GetEnvInt("StrategyUnSoldOrderNum", 1),
		StrategySessionPreFirstMinute:     utils.GetEnvInt("StrategySessionPreFirstMinute", 30),
		StrategySessionPreLastMinute:      utils.GetEnvInt("StrategySessionPreLastMinute", 30),
		StrategySessionRegularFirstMinute: utils.GetEnvInt("StrategySessionRegularFirstMinute", 15),
		StrategySessionRegularLastMinute:  utils.GetEnvInt("StrategySessionRegularLastMinute", 15),
		StrategySessionAfterFirstMinute:   utils.GetEnvInt("StrategySessionAfterFirstMinute", 30),
		StrategySessionAfterLastMinute:    utils.GetEnvInt("StrategySessionAfterLastMinute", 30),
		StrategyPreQuantityStep:           utils.GetEnvInt64("StrategyPreQuantityStep", 50),
		StrategyRegularQuantityStep:       utils.GetEnvInt64("StrategyRegularQuantityStep", 100),
		StrategyAfterQuantityStep:         utils.GetEnvInt64("StrategyAfterQuantityStep", 50),
		StrategyRegularFirstMinute:        utils.GetEnvInt("StrategyRegularFirstMinute", 15),
		StrategyRegularAfterMinute:        utils.GetEnvInt("StrategyRegularAfterMinute", 15),
		StrategyOrderLogicSec:             utils.GetEnvInt64("StrategyOrderLogicSec", 1),
		StrategyContinueLossSwitch:        utils.GetEnvBool("StrategyContinueLossSwitch", true),
		StrategyContinueLossNum:           utils.GetEnvInt("StrategyContinueLossNum", 2),
		StrategyContinueLossStopMinute:    utils.GetEnvInt("StrategyContinueLossStopMinute", 3),
		StrategyOvernightExitFirstMinute:  utils.GetEnvInt("StrategyOvernightExiFirstMinute", 3),

		SecondHistoryLimit:    utils.GetEnvInt("SecondHistoryLimit", 3600),
		MinuteHistoryLimit:    utils.GetEnvInt("MinuteHistoryLimit", 1440),
		MACDMinuteUpdateCount: utils.GetEnvInt("MACDMinuteUpdateCount", 600),
		MACDSecondUpdateCount: utils.GetEnvInt("MACDSecondUpdateCount", 1800),

		OpenCooldown: utils.GetEnvInt("OpenCooldown", 30),
		MACDFast:     utils.GetEnvInt("MACDFast", 12),
		MACDSlow:     utils.GetEnvInt("MACDSlow", 26),
		MACDSignal:   utils.GetEnvInt("MACDSignal", 9),

		SignalType:                    utils.GetEnvInt("SignalType", 2),
		SignalCurrentSec:              utils.GetEnvInt("SignalCurrentSec", 30),
		SignalRSIOverBuy:              utils.GetEnvFloat64("SignalRSIOverBuy", 75),
		SignalRSIOverSell:             utils.GetEnvFloat64("SignalRSIOverSell", 25),
		SignalLongPresent:             utils.GetEnvFloat64("SignalLongPresent", 0.0025),
		SignalShortPresent:            utils.GetEnvFloat64("SignalShortPresent", 0.0025),
		SignalWindow:                  utils.GetEnvInt("SignalWindow", 60),
		SignalRSI:                     utils.GetEnvInt("SignalRSI", 14),
		SignalRSIBase:                 utils.GetEnvInt("SignalRSIBase", 3),
		SignalATR:                     utils.GetEnvInt("SignalATR", 15),
		SignalATRNoiseThresholdFactor: utils.GetEnvFloat64("SignalATRNoiseThresholdFactor", 0.04),
		SignalATRNoiseThresholdMin:    utils.GetEnvFloat64("SignalATRNoiseThresholdMin", 0.01),
		SignalVolumeDynamic:           utils.GetEnvInt("SignalVolumeDynamic", 10),
		SignalVolumeFactor:            utils.GetEnvFloat64("SignalVolumeFactor", 1.2),

		TrendPriceType:          utils.GetEnvInt("TrendPriceType", 0),
		TrendMALong:             utils.GetEnvInt("TrendMALong", 300),
		TrendMAShort:            utils.GetEnvInt("TrendMAShort", 180),
		TrendMAPre:              utils.GetEnvInt("TrendMAPre", 60),
		TrendConfirm:            utils.GetEnvInt("TrendConfirm", 5),
		TrendFlowPeriod:         utils.GetEnvInt("TrendFlowPeriod", 60),
		TrendFlowEntryThreshold: utils.GetEnvFloat64("TrendFlowEntryThreshold", 0.2),
		TrendFlowExitThreshold:  utils.GetEnvFloat64("TrendFlowExitThreshold", 0.1),
		TrendAtrFactor:          utils.GetEnvFloat64("TrendAtrFactor", 0.5),
		TrendVolumeFactor:       utils.GetEnvFloat64("TrendVolumeFactor", 1.5),
		TrendVolumeMaxFactor:    utils.GetEnvFloat64("TrendVolumeMaxFactor", 10.0),
		TrendStopMaxLoss:        utils.GetEnvFloat64("TrendStopMaxLoss", 0.03),
		TrendATRLongStopBase:    utils.GetEnvFloat64("TrendATRLongStopBase", 10.0),
		TrendATRShortStopBase:   utils.GetEnvFloat64("TrendATRShortStopBase", 10.0),
		TrendATRLongWinBase:     utils.GetEnvFloat64("TrendATRLongWinBase", 10.0),
		TrendATRShortWinBase:    utils.GetEnvFloat64("TrendATRShortWinBase", 10.0),

		// 网格策略配置
		GridPriceType:    utils.GetEnvInt("GridPriceType", 0),
		GridWaitDuration: utils.GetEnvInt("GridWaitDuration", 300),
		GridATR:          utils.GetEnvInt("GridATR", 1800),
		GridMAPre:        utils.GetEnvInt("GridMAPre", 60),

		// 止盈止损配置
		GridATRLongStopBase:      utils.GetEnvFloat64("GridATRLongStopBase", 8.0),
		GridATRShortStopBase:     utils.GetEnvFloat64("GridATRShortStopBase", 8.0),
		GridATRLongWinBase:       utils.GetEnvFloat64("GridATRLongWinBase", 10.0),
		GridATRShortWinBase:      utils.GetEnvFloat64("GridATRShortWinBase", 10.0),
		GridMoveLongMaxMultiple:  utils.GetEnvFloat64("GridMoveLongMaxMultiple", 3.3),
		GridMoveShortMaxMultiple: utils.GetEnvFloat64("GridMoveShortMaxMultiple", 3.3),
		GridMoveProfitPercent:    utils.GetEnvFloat64("GridMoveProfitPercent", 0.003),
		GridMoveBasePercent:      utils.GetEnvFloat64("GridMoveBasePercent", 0.003),
		GridMovePercent:          utils.GetEnvFloat64("GridMovePercent", 0.0015),

		// 百分比网格配置
		GridPercentStep: utils.GetEnvFloat64("GridPercentStep", 0.01),

		// Test 配置
		TestBuyQuantity: utils.GetEnvInt64("TestBuyQuantity", 10),
		TestHoldSeconds: utils.GetEnvInt64("TestHoldSeconds", 30),

		// 日志
		LogFileName:      utils.GetEnv("LogFileName", ""),
		LogChanSize:      utils.GetEnvInt("logChanSize", 10000),
		LogFlushCount:    utils.GetEnvInt("logFlushCount", 1000),
		LogFlushDuration: utils.GetEnvInt("logFlushDuration", 30),

		LogLevel:       utils.GetEnv("LogLevel", "INFO"),
		LogEnabled:     utils.GetEnvBool("LogEnabled", true),
		HTMLLogLevel:   utils.GetEnv("HTMLLogLevel", "INFO"),
		HTMLLogEnabled: utils.GetEnvBool("HTMLLogEnabled", true),
		CmdLevel:       utils.GetEnv("CmdLevel", "WARN"),
		CmdEnabled:     utils.GetEnvBool("CmdEnabled", true),

		// 策略开启测试
		TestOpen:            utils.GetEnvBool("TestOpen", true),
		TestStrategy:        utils.GetEnv("TestStrategy", "price"),
		TestOpenDraw:        utils.GetEnvBool("TestOpenDraw", false),
		TestXlsx:            utils.GetEnv("TestXlsx", "test.xlsx"),
		TestTimeTicker:      utils.GetEnvInt("TestTimeTicker", 1),
		TestTimeScale:       utils.GetEnvFloat64("TestTimeScale", 10),
		TestPriceIndexType:  utils.GetEnvInt("TestPriceIndexType", 0),
		TestPriceIndexStart: utils.GetEnvInt("TestPriceIndexStart", 0),
		TestPriceIndexEnd:   utils.GetEnvInt("TestPriceIndexEnd", 0),

		// 时区配置
		TimeZone: utils.GetEnv("TimeZone", "America/New_York"),
	}

	// 验证订单配置
	if cfg.OrderType != "LIMIT" && cfg.OrderType != "MARKET" {
		log.Printf("Warning: Invalid OrderType '%s', using MARKET", cfg.OrderType)
		cfg.OrderType = "MARKET"
	}
	if cfg.OrderMode != "QUANTITY" && cfg.OrderMode != "AMOUNT" {
		log.Printf("Warning: Invalid OrderMode '%s', using QUANTITY", cfg.OrderMode)
		cfg.OrderMode = "QUANTITY"
	}

	return cfg, nil
}
