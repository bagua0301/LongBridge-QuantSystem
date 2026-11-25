package quant

import (
	"LongBridge-QuantSystem/types"
)

// GetTradeControl 获取交易时段控制器
func (qs *QuantSystem) GetTradeControl() interface{} {
	return qs.TradeControl
}

// GetTradeCtx 获取交易上下文
func (qs *QuantSystem) GetTradeCtx() interface{} {
	return qs.TradeCtx
}

// GetQuoteCtx 获取行情上下文
func (qs *QuantSystem) GetQuoteCtx() interface{} {
	return qs.QuoteCtx
}

// GetAppConfig 获取应用配置
func (qs *QuantSystem) GetAppConfig() interface{} {
	return qs.AppConfig
}

// GetStrategyMgr 获取策略管理器
func (qs *QuantSystem) GetStrategyMgr() interface{} {
	return qs.StrategyMgr
}

// GetPriceManager 获取价格更新器
func (qs *QuantSystem) GetPriceManager() types.PriceManagerInterface {
	return qs.PriceManager
}

// GetOrderQuery 获取订单查询管理器
func (qs *QuantSystem) GetOrderQuery() types.OrderQueryInterface {
	return qs.OrderQuery
}
