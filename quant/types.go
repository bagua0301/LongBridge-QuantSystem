package quant

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"

	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/strategy"
	"LongBridge-QuantSystem/strategy_t"
	"LongBridge-QuantSystem/trading"
	"LongBridge-QuantSystem/types"

	"github.com/longportapp/openapi-go/quote"
	"github.com/longportapp/openapi-go/trade"
)

// 确保 QuantSystem 实现了 types.QuantSystemInterface
var _ types.QuantSystemInterface = (*QuantSystem)(nil)

// configFieldInfo 缓存字段信息
type configFieldInfo struct {
	FieldName  string
	FieldIndex int
	FieldType  reflect.Kind
}

// QuantSystem 量化交易系统
type QuantSystem struct {
	// 上下文控制
	ctx             context.Context
	cancel          context.CancelFunc
	TradeControl    *trading.TradingTimeControl
	TradeCtx        *trade.TradeContext
	QuoteCtx        *quote.QuoteContext
	AppConfig       *myconfig.AppConfig
	configFields    map[string]configFieldInfo // 缓存字段信息
	PriceManager    types.PriceManagerInterface
	OrderQuery      *OrderQuery
	StrategyMgr     *strategy.StrategyManager
	StrategyTestMgr *strategy_t.StrategyTestManager

	// 持仓管理
	Positions     map[string]*types.PositionInfo
	PositionsLock sync.RWMutex

	// 订单记录
	CompletedOrders map[string]*types.CompletedOrder
	OrdersLock      sync.RWMutex

	// 风控统计
	DailyTradeCount int
	LastTradeDate   string
	TradeLock       sync.Mutex

	// 运行状态
	running bool
	runLock sync.RWMutex

	// 上次订单订单策略时间
	lastOrderLogicTimestamp atomic.Int64
}

// TradeSummary 交易汇总
type TradeSummary struct {
	TotalBuyAmount    float64                 // 总买入金额
	TotalSellAmount   float64                 // 总卖出金额
	TotalProfitLoss   float64                 // 总盈亏
	WinCount          int                     // 盈利订单数
	LossCount         int                     // 亏损订单数
	floatingWinCount  int                     // 浮动盈亏订单数
	floatingLossCount int                     // 浮动亏损订单数
	WinRate           float64                 // 胜率
	MaxWin            float64                 // 最大单笔盈利
	MaxLoss           float64                 // 最大单笔亏损
	SymbolStats       map[string]*SymbolStats // 各股票统计
}

// SymbolStats 单只股票统计
type SymbolStats struct {
	Symbol     string
	BuyAmount  float64
	SellAmount float64
	ProfitLoss float64 // 已实现盈亏
	Profit     float64 // 浮动盈亏
	WinCount   int
	LossCount  int
}
