package strategy

import (
	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/types"
	"time"
)

// 做多策略类型
type LongStrategyType string

const (
	LongTypeNone    LongStrategyType = ""        // 无策略
	LongTypeFixed   LongStrategyType = "Fixed"   // 固定差价做多
	LongTypePercent LongStrategyType = "Percent" // 百分比做多
)

// 做空策略类型
type ShortStrategyType string

const (
	ShortTypeNone    ShortStrategyType = ""        // 无策略
	ShortTypeFixed   ShortStrategyType = "Fixed"   // 固定差价做空
	ShortTypePercent ShortStrategyType = "Percent" // 百分比做空
)

// 订单记录
type StrategyOrder struct {
	Symbol       string              // 股票代码
	Category     types.OrderCategory // 订单类型
	Action       types.OrderAction   // 订单动作类型
	StrategyName string              // 关联的策略

	// 开仓
	OrderID        string    // 开仓订单ID
	Price          float64   // 开仓价格
	Quantity       int64     // 开仓总数量
	BoughtQuantity int64     // 开仓已成交数量 (订单可能部分成交)
	Time           time.Time // 开仓时间

	// 平仓
	CloseOrderID  string            // 平仓订单ID
	ClosePrice    float64           // 平仓价格
	CloseQuantity int64             // 平仓已成交数量
	CloseStatus   types.OrderStatus // 平仓订单状态
	CloseReason   string            // 平仓卖出原因
	CloseTime     time.Time         // 平仓时间

	Profit             float64 // 已盈利金额
	HighestPrice       float64 // 持仓期间最高价（用于移动止盈）
	LowestPrice        float64 // 持仓期间最低价（用于移动止盈）
	StopLongLossPrice  float64 // 做多止损价格
	StopShortLossPrice float64 // 做空止损价格
}

// 策略接口
type Strategy interface {
	// 基本信息
	GetShortName() string
	GetName() string
	GetDescription() string
	GetStrategyType() string
	GetRequiredData() []string
	GetStrategyConfig() map[string]interface{}
	Init(appConfig *myconfig.AppConfig) error

	// 获取交易信号 - 返回多个订单
	GetSignal(symbol string) ([]types.OrderTarget, error)

	// 订单事件回调
	OnBuyOrderSubmitted(orderID string, order *types.OrderTarget)  // 买入订单已提交
	OnBuyOrderFilled(orderID string, order *types.OrderTarget)     // 买入订单成交
	OnSellOrderSubmitted(orderID string, order *types.OrderTarget) // 卖出订单已提交
	OnSellOrderFilled(orderID string, order *types.OrderTarget)    // 卖出订单成交
	OnOrderFailed(orderID string, order *types.OrderTarget)        // 订单失败

	// 状态管理
	GetCurrentState() map[string]interface{}
	Reset()

	// cmd卖出 - 返回多个订单
	CmdSell(symbol string, cmdSellInfo string) []types.OrderTarget
	// cmd买入 - 返回多个订单
	CmdBuy(symbol string, cmdBuyInfo string) []types.OrderTarget
}
