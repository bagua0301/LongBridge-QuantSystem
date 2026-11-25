package quant

import (
	"fmt"
	"sync"
	"time"

	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/strategy_t"
	"LongBridge-QuantSystem/trading"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"

	"github.com/longportapp/openapi-go/trade"
	"github.com/shopspring/decimal"
)

var (
	// 订单去重缓存（记录最近提交的订单）
	orderCache     = make(map[string]time.Time)
	orderCacheLock sync.RWMutex
)

// isOrderDuplicate 检查订单是否重复
func isOrderDuplicate(uniqueKey string) bool {
	orderCacheLock.RLock()
	defer orderCacheLock.RUnlock()

	if t, exists := orderCache[uniqueKey]; exists {
		// 如果订单在最近5秒内提交过，认为是重复
		if utils.Since(t) < 5*time.Second {
			logger.Warn("检测到重复订单，已过滤: %s", uniqueKey)
			return true
		}
	}
	return false
}

// recordOrder 记录已提交的订单
func recordOrder(uniqueKey string) {
	orderCacheLock.Lock()
	defer orderCacheLock.Unlock()
	orderCache[uniqueKey] = utils.Now()

	// 清理过期的缓存（超过10秒）
	for key, t := range orderCache {
		if utils.Since(t) > 10*time.Second {
			delete(orderCache, key)
		}
	}
}

// GetCompletedOrder 获取订单
func (qs *QuantSystem) GetCompletedOrder(orderID string) *types.CompletedOrder {
	qs.OrdersLock.RLock()
	defer qs.OrdersLock.RUnlock()

	order, ok := qs.CompletedOrders[orderID]
	if !ok {
		return nil
	}

	return order
}

// AddCompletedOrder 添加已完成订单
func (qs *QuantSystem) AddCompletedOrder(order *types.CompletedOrder) {
	qs.OrdersLock.Lock()
	defer qs.OrdersLock.Unlock()
	qs.CompletedOrders[order.OrderID] = order
}

// notifyStrategyBuySubmitted 通知策略买入订单已提交
func (qs *QuantSystem) notifyStrategyBuySubmitted(orderID string, order *types.OrderTarget) {
	qs.StrategyMgr.DispatchOrderEvent("BuySubmitted", orderID, order)
}

// notifyStrategyBuyFilled 通知策略买入成交
func (qs *QuantSystem) notifyStrategyBuyFilled(orderID string, order *types.OrderTarget) {
	qs.StrategyMgr.DispatchOrderEvent("BuyFilled", orderID, order)
}

// notifyStrategySellSubmitted 通知策略卖出订单已提交（需要绑定买入订单ID）
func (qs *QuantSystem) notifyStrategySellSubmitted(orderID string, order *types.OrderTarget) {
	qs.StrategyMgr.DispatchOrderEvent("SellSubmitted", orderID, order)
}

// notifyStrategySellFilled 通知策略卖出成交
func (qs *QuantSystem) notifyStrategySellFilled(orderID string, order *types.OrderTarget) {
	qs.StrategyMgr.DispatchOrderEvent("SellFilled", orderID, order)
}

// notifyStrategyOrderFailed 通知策略订单失败
func (qs *QuantSystem) notifyStrategyOrderFailed(orderID string, order *types.OrderTarget) {
	qs.StrategyMgr.DispatchOrderEvent("OrderFailed", orderID, order)
}

// executeOrderWithRisk 执行订单（带风控）
func (qs *QuantSystem) executeOrderWithRisk(order *types.OrderTarget) {
	logger.Info("========== 执行订单检查 ==========")
	logger.Info("股票: %s, 操作: %s, 数量: %d, 价格: %.4f",
		order.Symbol, order.Action, order.Quantity, order.TargetPrice)

	// ============================================
	// 去重检查
	// ============================================
	if order.UniqueKey == "" {
		if order.Action == types.SellOrderAction && order.CloseOrderID != "" {
			order.UniqueKey = fmt.Sprintf("%s_%s_%s_%s", order.Symbol, order.StrategyName, order.Action, order.CloseOrderID)
		} else {
			order.UniqueKey = fmt.Sprintf("%s_%s_%s_%.4f_%d_%s",
				order.Symbol, order.StrategyName, order.Action, order.TargetPrice, order.Quantity, order.CloseOrderID)
		}
		if isOrderDuplicate(order.UniqueKey) {
			logger.Error("重复订单被过滤: %s", order.UniqueKey)
			qs.notifyStrategyOrderFailed("", order)
			return
		}
		// 记录订单
		recordOrder(order.UniqueKey)
	}

	// 买入时检查持仓比例
	if (order.Category == types.LongOrderCategory && order.Action == types.BuyOrderAction) ||
		(order.Category == types.ShortOrderCategory && order.Action == types.SellOrderAction) {
		if !qs.CheckMaxPosition(order.Symbol, order.Quantity, order.TargetPrice) {
			logger.Warn("买入被拒绝: %s 持仓比例超限", order.Symbol)
			qs.notifyStrategyOrderFailed("", order)
			return
		}
	}

	// 多单卖出时检查是否有足够持仓
	if order.Category == types.LongOrderCategory && order.Action == types.SellOrderAction {
		qs.PositionsLock.RLock()
		pos, exists := qs.Positions[order.Symbol]
		qs.PositionsLock.RUnlock()

		if !exists {
			logger.Warn("卖出被拒绝: %s 没有持仓", order.Symbol)
			qs.notifyStrategyOrderFailed("", order)
			return
		}

		if pos == nil {
			logger.Warn("卖出被拒绝: %s 持仓信息为空", order.Symbol)
			qs.notifyStrategyOrderFailed("", order)
			return
		}

		if pos.Quantity < float64(order.Quantity) {
			logger.Warn("卖出被拒绝: %s 持仓不足, 需要 %d, 持有 %.2f",
				order.Symbol, order.Quantity, pos.Quantity)
			qs.notifyStrategyOrderFailed("", order)
			return
		}
	}

	if myconfig.IsTestMode() {
		// 执行测试订单
		qs.executeTestOrder(order)
	} else {
		// 执行真实下单
		qs.executeRealOrder(order)
	}
}

// executeRealOrder 执行真实下单
func (qs *QuantSystem) executeRealOrder(order *types.OrderTarget) {
	logger.Warn("========== 准备下单 ==========")
	logger.Warn("股票: %s", order.Symbol)
	logger.Warn("操作: %s", order.Action)
	logger.Warn("策略ID: %s", order.StrategyName)
	logger.Warn("目标价格: %.4f", order.TargetPrice)
	logger.Warn("策略原因: %s", order.Reason)

	if qs.TradeCtx == nil {
		logger.Error("交易上下文为空，无法下单")
		qs.notifyStrategyOrderFailed("", order)
		return
	}

	// 获取当前时段
	currentSession := qs.TradeControl.GetCurrentSession()
	sessionName := trading.GetSessionName(currentSession)
	logger.Info("当前时段: %s", sessionName)

	// 判断当前是否是盘中时段
	isRegularSession := (currentSession == trading.SessionRegular)

	// 确定订单类型（根据配置和时段）
	var orderType trade.OrderType
	var finalPrice float64
	var triggerPrice float64
	var useSlippage bool

	// 如果配置是市价单，但当前不是盘中时段，自动切换为限价单（不加滑点）
	if qs.AppConfig.OrderType == "MARKET" && !isRegularSession {
		logger.Warn("市价单仅支持盘中交易，当前时段为 %s，自动切换为限价单（使用原始价格）", sessionName)
		orderType = trade.OrderTypeLO
		finalPrice = utils.RoundToUSStockPrice(order.TargetPrice)
		triggerPrice = finalPrice
		useSlippage = false
		logger.Info("订单类型: 限价单 (自动切换，无滑点)")
	} else if qs.AppConfig.OrderType == "MARKET" {
		orderType = trade.OrderTypeMO
		finalPrice = utils.RoundToUSStockPrice(order.TargetPrice)
		triggerPrice = finalPrice
		useSlippage = false
		logger.Info("订单类型: 市价单 (仅限盘中)")
	} else {
		orderType = trade.OrderTypeLO
		// 限价单只在盘中时段应用滑点保护
		useSlippage = isRegularSession
		logger.Info("订单类型: 限价单")
	}

	// 限价单需要计算价格（只在盘中时段带滑点保护）
	if useSlippage {
		if order.Action == types.BuyOrderAction {
			finalPrice = order.TargetPrice * (1 + qs.AppConfig.SlippagePercent/100)
			logger.Info("滑点保护: 买入限价 %.6f (原价 %.6f + %.2f%%)",
				finalPrice, order.TargetPrice, qs.AppConfig.SlippagePercent)
		} else {
			finalPrice = order.TargetPrice * (1 - qs.AppConfig.SlippagePercent/100)
			logger.Info("滑点保护: 卖出限价 %.6f (原价 %.6f - %.2f%%)",
				finalPrice, order.TargetPrice, qs.AppConfig.SlippagePercent)
		}
		// 四舍五入到两位小数
		finalPrice = utils.RoundToUSStockPrice(finalPrice)
		triggerPrice = finalPrice
		logger.Info("价格调整后: %.2f (符合最小价格变动单位)", finalPrice)
	} else if orderType == trade.OrderTypeLO {
		// 限价单但无滑点保护，直接使用原价
		finalPrice = utils.RoundToUSStockPrice(order.TargetPrice)
		triggerPrice = finalPrice
		logger.Info("限价单使用原始价格: %.2f", finalPrice)
	}

	// 确保价格符合最小变动单位（再次确认）
	if orderType == trade.OrderTypeLO {
		finalPrice = utils.RoundToUSStockPrice(finalPrice)
		triggerPrice = finalPrice
	}

	logger.Info("最终委托价格: %.2f", finalPrice)

	// 确定买卖方向
	var side trade.OrderSide
	if order.Action == types.BuyOrderAction {
		side = trade.OrderSideBuy
	} else {
		side = trade.OrderSideSell
	}

	// 计算下单数量
	var finalQuantity int64

	if qs.AppConfig.OrderMode == "AMOUNT" {
		// 按金额下单
		price := finalPrice
		if price <= 0 {
			price = order.TargetPrice
		}
		finalQuantity = int64(qs.AppConfig.BaseAmount / price)
		if finalQuantity < 1 {
			finalQuantity = 1
		}
		logger.Info("下单模式: 按金额 (%.2f 美元) → 计算数量 %d 股", qs.AppConfig.BaseAmount, finalQuantity)
	} else {
		// 按数量下单：使用策略返回的数量
		finalQuantity = order.Quantity
		if finalQuantity <= 0 {
			finalQuantity = 1
		}
		logger.Info("下单模式: 按数量 → %d 股", finalQuantity)
	}

	// 应用数量限制
	if finalQuantity < qs.AppConfig.MinQuantity {
		logger.Warn("数量 %d 低于最小限制 %d，调整为最小限制", finalQuantity, qs.AppConfig.MinQuantity)
		finalQuantity = qs.AppConfig.MinQuantity
	}
	if finalQuantity > qs.AppConfig.MaxQuantity {
		logger.Warn("数量 %d 超过最大限制 %d，调整为最大限制", finalQuantity, qs.AppConfig.MaxQuantity)
		finalQuantity = qs.AppConfig.MaxQuantity
	}

	// 构建订单请求
	submitOrder := &trade.SubmitOrder{
		Symbol:            order.Symbol,
		OrderType:         orderType,
		Side:              side,
		SubmittedQuantity: uint64(finalQuantity),
		TimeInForce:       trade.TimeTypeDay,
	}

	// 限价单需要设置价格和触发价格（直接赋值，不是指针）
	if orderType == trade.OrderTypeLO {
		submitOrder.SubmittedPrice = decimal.NewFromFloat(finalPrice)
		submitOrder.TriggerPrice = decimal.NewFromFloat(triggerPrice)
		logger.Info("限价单设置: 委托价=%.2f, 触发价=%.2f", finalPrice, triggerPrice)
	}

	// 提交订单
	resp, err := qs.TradeCtx.SubmitOrder(qs.ctx, submitOrder)
	if err != nil {
		logger.Error("下单失败: %v", err)
		qs.notifyStrategyOrderFailed("", order)
		return
	}

	logger.Info("下单成功! 订单ID: %s", resp)
	logger.Warn("最终订单: %s %d股 @ %.2f (类型: %s)",
		order.Action, finalQuantity, finalPrice,
		map[bool]string{true: "限价单", false: "市价单"}[orderType == trade.OrderTypeLO])

	// ============================================
	// 卖出订单：预减持仓（防止重复卖出）
	// ============================================
	// 根据方向通知策略
	if order.Action == types.BuyOrderAction {
		qs.notifyStrategyBuySubmitted(resp, order)
	} else {
		qs.notifyStrategySellSubmitted(resp, order)
	}

	// 记录待确认订单
	qs.OrderQuery.AddPendingOrder(resp, order)
}

// executeTestOrder 执行测试订单（只保存订单信息到测试管理器）
func (qs *QuantSystem) executeTestOrder(order *types.OrderTarget) {
	logger.Warn("========== 执行测试订单 ==========")
	logger.Warn("股票: %s", order.Symbol)
	logger.Warn("操作: %s", order.Action)
	logger.Warn("策略ID: %s", order.StrategyName)
	logger.Warn("目标价格: %.4f", order.TargetPrice)
	logger.Warn("数量: %d", order.Quantity)
	logger.Warn("策略原因: %s", order.Reason)

	testManager := strategy_t.GetGlobalTestManager()
	if testManager == nil {
		logger.Error("测试管理器为空")
		qs.notifyStrategyOrderFailed("", order)
		return
	}

	var orderID string

	if order.Action == types.BuyOrderAction {
		// 提交买入订单到测试管理器
		orderID = testManager.SubmitBuyOrder(order)
		order.OrderID = orderID
		logger.Warn("测试买入订单已提交: %s", orderID)

		// 通知策略订单已提交
		qs.notifyStrategyBuySubmitted(orderID, order)

	} else if order.Action == types.SellOrderAction {
		// 提交卖出订单到测试管理器
		orderID = testManager.SubmitSellOrder(order)
		logger.Warn("测试卖出订单已提交: %s, 绑定买入订单: %s", orderID, order.CloseOrderID)

		// 通知策略订单已提交
		qs.notifyStrategySellSubmitted(orderID, order)
	}

	// 记录待确认订单（测试模式也记录，用于查询）
	qs.OrderQuery.AddPendingOrder(orderID, order)

	logger.Warn("测试订单已提交，订单ID: %s", orderID)
}
