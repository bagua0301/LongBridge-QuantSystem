package types

import (
	"context"
	"time"

	"github.com/longportapp/openapi-go/quote"
)

// 秒级聚合数据
type SecondData struct {
	Timestamp     int64     // 秒级时间戳
	Time          time.Time // 时间
	Open          float64   // 该秒开盘价
	High          float64   // 该秒最高价
	Low           float64   // 该秒最低价
	Close         float64   // 该秒收盘价
	Volume        int64     // 该秒开盘总成交量（前面累计）
	Turnover      float64   // 该秒开盘总成交额（前面累计）
	CloseVolume   int64     // 该秒收盘总成交量（前面累计）
	CloseTurnover float64   // 该秒收盘总成交额（前面累计）
}

// 分钟级数据
type MinuteData struct {
	Timestamp     int64 // 分钟时间戳（秒级，如 2024-01-01 10:30:00）
	Time          time.Time
	Open          float64 // 开盘价
	High          float64 // 最高价
	Low           float64 // 最低价
	Close         float64 // 收盘价
	Volume        int64   // 该分钟总成交量（前面累计）
	Turnover      float64 // 该分钟总成交额（前面累计）
	CloseVolume   int64   // 该分钟收盘总成交量（前面累计）
	CloseTurnover float64 // 该分钟收盘总成交额（前面累计）
}

// MACD指标数据
type MACDData struct {
	Timestamp int64   // 时间戳
	MACD      float64 // MACD线
	Signal    float64 // 信号线
	Histogram float64 // 柱状图 (MACD - Signal)
}

// 买卖信号
type SignalResult struct {
	Timestamp  int64
	Action     OrderAction // "BUY" 或 "SELL"
	Reason     string      // 触发原因
	StopLoss   float64
	TakeProfit float64
}

// 持仓信息
type PositionInfo struct {
	Symbol       string
	StrategyName string        // 策略ID，用于标识是哪个策略产生的持仓（空表示原有持仓）
	Category     OrderCategory // 订单类型
	Quantity     float64       // 持仓数量（支持小数，如基金份额）
	AvgPrice     float64       // 平均成本
	CurrentPrice float64       // 当前价格
	MarketValue  float64       // 市值
	ProfitLoss   float64       // 盈亏
	ProfitRate   float64       // 盈亏率
}

// 已成交订单记录
type CompletedOrder struct {
	OrderID      string        // 订单ID
	Symbol       string        // 股票代码
	Category     OrderCategory // 订单类型
	Action       OrderAction   // 订单动作类型
	StrategyName string        // 策略ID，用于区分是哪个策略产生的订单
	Price        float64       // 成交价格
	Quantity     float64       // 成交数量
	Amount       float64       // 成交金额
	ProfitLoss   float64       // 盈亏金额（卖出时计算）
	ProfitRate   float64       // 盈亏率
	Timestamp    time.Time     // 成交时间
	Reason       string        // 触发原因
	CloseOrderID string        // 平仓订单ID
}

// 价格接口
type PriceManagerInterface interface {
	OnQuote(ev *quote.PushQuote)
	GetAllSymbol() []string
	ClearSymbol(symbol string)
	ClearAll()

	// 绘制macd
	DrawMACD(symbol string, filePath string, allOrders []*CompletedOrder) error
	// 绘制price
	DrawPrice(symbol string, filePath string, allOrders []*CompletedOrder) error
	// 最新价格
	GetCurrentPrice(symbol string) float64

	// 获取MACD
	GetMACD(symbol string, priceType PriceType, num int) []*MACDData
	// 统一获取MACD信号
	GetMACDSignal(symbol string, priceType PriceType) int
	// 统一获取MACD强度
	GetMACDStrength(symbol string, priceType PriceType) float64

	// 获取当前的成交量
	GetCurrentVolume(symbol string, priceType PriceType, exNum int) float64
	// 获取最近N个级别平均价格
	GetAveragePrice(symbol string, priceType PriceType, num int, exNum int) float64
	// 获取过去N个级别平均成交量
	GetAverageVolume(symbol string, priceType PriceType, num int, exNum int) float64

	// 获取指定周期数的ATR（平均真实波幅）
	GetATR(symbol string, priceType PriceType, num int, exNum int) float64
	// 计算一组价格序列的 RSI
	GetRSI(symbol string, priceType PriceType, num int, exNum int) float64

	// 获取秒级数据
	GetSecondData(symbol string, num int) []*SecondData

	// 获取买卖信号
	CheckSignals(symbol string) SignalResult
}

// 订单查询接口
type OrderQueryInterface interface {
	AddPendingOrder(orderID string, order *OrderTarget)
	RemovePendingOrder(orderID string)
	CheckPendingOrders(ctx context.Context)
	GetPendingOrderCount() int
	HandleTestOrderEvent(event *TestOrderEvent)
}

// 量化系统接口
type QuantSystemInterface interface {
	// 核心组件获取
	GetTradeControl() interface{}
	GetTradeCtx() interface{}
	GetQuoteCtx() interface{}
	GetAppConfig() interface{}
	GetStrategyMgr() interface{}
	GetPriceManager() PriceManagerInterface
	GetOrderQuery() OrderQueryInterface

	// 持仓管理
	GetPositions() map[string]*PositionInfo
	GetPosition(symbol string) *PositionInfo

	// 风控统计
	GetDailyTradeCount() int
	GetLastTradeDate() string
	IncrementDailyTradeCount()
	ResetDailyTradeCount()

	// 运行控制
	Run()
	Stop()
	IsRunning() bool

	OrderLogic()
}

// 订单类型
type OrderCategory int32

const (
	NoneOrderCategory  OrderCategory = 0 // 默认
	LongOrderCategory  OrderCategory = 1 // 股票做多
	ShortOrderCategory OrderCategory = 2 // 股票做空
)

// 订单动作类型
type OrderAction string

const (
	NoneOrderAction OrderAction = ""     // 默认
	BuyOrderAction  OrderAction = "BUY"  // 买入
	SellOrderAction OrderAction = "SELL" // 卖出
)

type OrderStatus string

const (
	NoneStatus      OrderStatus = ""          // 无卖出订单
	PendingStatus   OrderStatus = "pending"   // 待提交
	SubmittedStatus OrderStatus = "submitted" // 已提交未成交
	PartialStatus   OrderStatus = "partial"   // 部分成交
	CompletedStatus OrderStatus = "completed" // 完全成交
	FailedStatus    OrderStatus = "failed"    // 失败
)

// 订单目标
type OrderTarget struct {
	Symbol       string
	Category     OrderCategory // 订单类型
	Action       OrderAction   // 订单动作类型
	StrategyName string        // 关联的策略
	OrderID      string        // 订单ID
	TargetPrice  float64       // 目标价格
	Quantity     int64         // 数量
	Reason       string        // 触发原因
	CloseOrderID string        // 关闭订单ID
	UniqueKey    string        // 唯一标识，用于去重（如：买入订单ID+方向）
}

// 价格类型
type PriceType int32

const (
	SecondType PriceType = 0 // 秒级
	MinuteType PriceType = 1 // 分钟级
)

// 趋势类型
type TrendType string

const (
	NoneTrend TrendType = "SIDEWAYS" // 默认
	DownTrend TrendType = "DOWN"     // 下降趋势
	UpTrend   TrendType = "UP"       // 上升趋势
)

// 信号趋势类型
type SignalTrendType int

const (
	SignalTrendMin   SignalTrendType = 0 // 最小信号类型
	SignalTrendLong  SignalTrendType = 1 // 信号做多
	SignalTrendShort SignalTrendType = 2 // 信号做空
	SignalTrendMax   SignalTrendType = 3 // 最大信号类型
)

// ============================================
// 测试相关类型定义
// ============================================
// 价格事件
type TestPriceEvent struct {
	data *quote.PushQuote
}

// 订单事件
type TestOrderEvent struct {
	OrderID   string
	Order     OrderTarget
	Status    string
	Timestamp time.Time
	Profit    float64
}

// 测试订单
type TestOrder struct {
	OrderID    string
	Order      OrderTarget
	Status     string
	SubmitTime time.Time
	FillTime   time.Time
	Profit     float64
}
