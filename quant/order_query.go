package quant

import (
	"context"
	"strings"
	"sync"
	"time"

	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"

	"github.com/longportapp/openapi-go/trade"
)

// PendingOrder 待确认订单
type PendingOrder struct {
	OrderID    string
	Order      types.OrderTarget
	SubmitTime time.Time
}

// OrderQuery 订单查询管理器
type OrderQuery struct {
	qs            *QuantSystem
	pendingOrders map[string]*PendingOrder
	mu            sync.RWMutex
}

// NewOrderQuery 创建订单查询管理器
func NewOrderQuery(qs *QuantSystem) *OrderQuery {
	return &OrderQuery{
		qs:            qs,
		pendingOrders: make(map[string]*PendingOrder),
	}
}

// AddPendingOrder 添加待确认订单
func (oq *OrderQuery) AddPendingOrder(orderID string, order *types.OrderTarget) {
	oq.mu.Lock()
	defer oq.mu.Unlock()
	oq.pendingOrders[orderID] = &PendingOrder{
		OrderID:    orderID,
		Order:      *order,
		SubmitTime: utils.Now(),
	}
	logger.Debug("添加待确认订单: %s, %s %d股@%.4f", orderID, order.Action, order.Quantity, order.TargetPrice)
}

// RemovePendingOrder 移除待确认订单
func (oq *OrderQuery) RemovePendingOrder(orderID string) {
	oq.mu.Lock()
	defer oq.mu.Unlock()
	delete(oq.pendingOrders, orderID)
	logger.Debug("移除待确认订单: %s", orderID)
}

// GetPendingOrder 获取待确认订单
func (oq *OrderQuery) GetPendingOrder(orderID string) *PendingOrder {
	oq.mu.RLock()
	defer oq.mu.RUnlock()

	order, exists := oq.pendingOrders[orderID]
	if !exists {
		return nil
	}
	return order
}

// GetPendingOrders 获取所有待确认订单
func (oq *OrderQuery) GetPendingOrders() []*PendingOrder {
	oq.mu.RLock()
	defer oq.mu.RUnlock()

	orders := make([]*PendingOrder, 0, len(oq.pendingOrders))
	for _, order := range oq.pendingOrders {
		orders = append(orders, order)
	}
	return orders
}

// CheckPendingOrders 检查所有待确认订单状态
func (oq *OrderQuery) CheckPendingOrders(ctx context.Context) {
	orders := oq.GetPendingOrders()

	isTestMode := myconfig.IsTestMode()
	orderTimeOut := time.Duration(oq.qs.AppConfig.OrderTimeOut)
	for _, pending := range orders {
		// 检查是否超时
		if utils.Since(pending.SubmitTime) > orderTimeOut*time.Second {
			logger.Warn("订单超时未成交: %s, %s, 已等待 %.0f 秒",
				pending.OrderID, pending.Order.Symbol, utils.Since(pending.SubmitTime).Seconds())

			if !isTestMode {
				// 取消超时订单
				oq.cancelOrder(ctx, pending)
			}
			continue
		}

		if !isTestMode {
			oq.checkOrderStatus(ctx, pending)
		}
	}
}

// handleFilledOrder 处理已成交订单
func (oq *OrderQuery) handleFilledOrder(pending *PendingOrder, orderDetail *trade.OrderDetail) {
	if oq.GetPendingOrder(pending.OrderID) == nil {
		return
	}
	// 移除待确认订单
	oq.RemovePendingOrder(pending.OrderID)
	// go中pending 还有效,对象不再被任何变量引用时才会回收

	executedQty := orderDetail.ExecutedQuantity
	if executedQty == 0 {
		executedQty = pending.Order.Quantity
	}

	executedPrice, ok := utils.DecimalToFloat64(orderDetail.ExecutedPrice)
	if !ok || executedPrice <= 0 {
		executedPrice = pending.Order.TargetPrice
	} else {
		pending.Order.TargetPrice = executedPrice
	}

	logger.Warn("🎉 检测到订单成交: %s, %s, 成交数量=%d, 成交价格=%.4f",
		pending.OrderID, pending.Order.Symbol, executedQty, executedPrice)

	var profitLoss, profitRate float64 = 0, 0
	amount := executedPrice * float64(executedQty)
	switch pending.Order.Category {
	case types.LongOrderCategory:
		if pending.Order.Action == types.BuyOrderAction {
			logger.Info("📈 做多买入成交: %s, 数量: %d, 价格: %.4f, 金额: %.2f",
				pending.Order.Symbol, executedQty, executedPrice, amount)
		} else if pending.Order.Action == types.SellOrderAction && pending.Order.CloseOrderID != "" {
			closeCompletedOrder := oq.qs.GetCompletedOrder(pending.Order.CloseOrderID)
			if closeCompletedOrder != nil {
				profitLoss = (executedPrice - closeCompletedOrder.Price) * float64(executedQty)
				profitRate = profitLoss / amount

				logger.Info("✅ 做多卖出成交: %s, 数量: %d, 价格: %.4f, 金额: %.2f, 盈亏: %.2f (%.2f%%)",
					pending.Order.Symbol, executedQty, executedPrice, amount, profitLoss, profitRate)

				// 更新交易计数
				oq.qs.IncrementDailyTradeCount()
			}
		}
	case types.ShortOrderCategory:
		if pending.Order.Action == types.SellOrderAction {
			logger.Info("📈 做空卖出成交: %s, 数量: %d, 价格: %.4f, 金额: %.2f",
				pending.Order.Symbol, executedQty, executedPrice, amount)
		} else if pending.Order.Action == types.BuyOrderAction && pending.Order.CloseOrderID != "" {
			closeCompletedOrder := oq.qs.GetCompletedOrder(pending.Order.CloseOrderID)
			if closeCompletedOrder != nil {
				profitLoss = (closeCompletedOrder.Price - pending.Order.TargetPrice) * float64(executedQty)
				profitRate = profitLoss / amount

				logger.Info("✅ 做空买入成交: %s, 数量: %d, 价格: %.4f, 金额: %.2f, 盈亏: %.2f (%.2f%%)",
					pending.Order.Symbol, executedQty, executedPrice, amount, profitLoss, profitRate)

				// 更新交易计数
				oq.qs.IncrementDailyTradeCount()
			}
		}
	}

	completedOrder := &types.CompletedOrder{
		OrderID:      pending.OrderID,
		Symbol:       pending.Order.Symbol,
		Category:     pending.Order.Category,
		Action:       pending.Order.Action,
		StrategyName: pending.Order.StrategyName,
		Price:        executedPrice,
		Quantity:     float64(executedQty),
		Amount:       amount,
		ProfitLoss:   profitLoss,
		ProfitRate:   profitRate,
		Timestamp:    utils.Now(),
		Reason:       pending.Order.Reason,
		CloseOrderID: pending.Order.CloseOrderID,
	}

	if pending.Order.Action == types.BuyOrderAction {
		oq.qs.notifyStrategyBuyFilled(pending.OrderID, &pending.Order)
	} else {
		oq.qs.notifyStrategySellFilled(pending.OrderID, &pending.Order)
	}

	oq.qs.UpdatePositionFromOrder(completedOrder)
	// 保存成交订单
	oq.qs.AddCompletedOrder(completedOrder)
}

// cancelOrder 取消真实订单
func (oq *OrderQuery) cancelOrder(ctx context.Context, pending *PendingOrder) {
	logger.Info("========== 取消超时订单 ==========")
	logger.Info("订单ID: %s", pending.OrderID)
	logger.Info("股票: %s", pending.Order.Symbol)
	logger.Info("方向: %s", pending.Order.Action)
	logger.Info("数量: %d", pending.Order.Quantity)
	logger.Info("价格: %.4f", pending.Order.TargetPrice)
	logger.Info("提交时间: %s", pending.SubmitTime.In(time.Local).Format("15:04:05"))
	logger.Info("已等待: %.0f 秒", utils.Since(pending.SubmitTime).Seconds())

	// 使用 WithdrawOrder 取消订单
	err := oq.qs.TradeCtx.WithdrawOrder(ctx, pending.OrderID)
	if err != nil {
		// 检查错误是否是订单已撤销
		if strings.Contains(err.Error(), "已撤销成功") || strings.Contains(err.Error(), "601011") {
			logger.Info("订单已撤销: %s", pending.OrderID)
		} else {
			logger.Error("取消订单失败 %s: %v", pending.OrderID, err)
			return
		}
	}

	// 取消成功后才能移除,有可能取消发送的时候成交了
	orderDetail, err := oq.qs.TradeCtx.OrderDetail(ctx, pending.OrderID)
	if err == nil && orderDetail.OrderId != "" {
		statusStr := string(orderDetail.Status)
		logger.Info("statusStr=%s", statusStr)
		switch statusStr {
		case "FilledStatus":
			logger.Info("订单取消,但订单成交了: %s", pending.OrderID)
			oq.handleFilledOrder(pending, &orderDetail)
		case "CancelledStatus", "RejectedStatus", "CanceledStatus":
			logger.Info("订单取消成功: %s", pending.OrderID)
			// 移除待确认订单
			oq.RemovePendingOrder(pending.OrderID)
			// 通知策略订单失败
			oq.qs.notifyStrategyOrderFailed(pending.OrderID, &pending.Order)
		}
	}
}

// checkOrderStatus 检查真实订单状态
func (oq *OrderQuery) checkOrderStatus(ctx context.Context, pending *PendingOrder) {
	// 查询订单详情
	orderDetail, err := oq.qs.TradeCtx.OrderDetail(ctx, pending.OrderID)
	if err != nil {
		logger.Debug("查询订单失败 %s: %v", pending.OrderID, err)
		return
	}

	if orderDetail.OrderId == "" {
		logger.Debug("订单不存在: %s", pending.OrderID)
		oq.RemovePendingOrder(pending.OrderID)
		return
	}

	logger.Debug("订单状态查询: %s, 状态=%s, 成交数量=%d, 委托数量=%d",
		pending.OrderID, orderDetail.Status, orderDetail.ExecutedQuantity, orderDetail.Quantity)

	// 将状态转换为字符串
	statusStr := string(orderDetail.Status)

	// 如果订单已成交
	switch statusStr {
	case "FilledStatus":
		oq.handleFilledOrder(pending, &orderDetail)
	case "CancelledStatus", "RejectedStatus":
		logger.Warn("订单未成交: %s, 状态=%s", pending.OrderID, orderDetail.Status)

		oq.RemovePendingOrder(pending.OrderID)
		oq.qs.notifyStrategyOrderFailed(pending.OrderID, &pending.Order)
	}
}

// GetPendingOrderCount 获取待确认订单数量
func (oq *OrderQuery) GetPendingOrderCount() int {
	oq.mu.RLock()
	defer oq.mu.RUnlock()
	return len(oq.pendingOrders)
}

/* 测试订单处理 beigin */
// handleTestOrderEvent 处理测试订单事件
func (oq *OrderQuery) HandleTestOrderEvent(event *types.TestOrderEvent) {
	logger.Info("[测试] 收到订单事件: %s, 订单ID=%s, 状态=%s", event.Order.Action, event.OrderID, event.Status)

	switch event.Status {
	case "FilledStatus":
		oq.handleTestFilledOrder(event)
	case "CancelledStatus", "RejectedStatus":
		// 订单取消
		oq.handleTestOrderCancelled(event)
	}
}

// handleTestFilledOrder 处理测试成交
func (oq *OrderQuery) handleTestFilledOrder(event *types.TestOrderEvent) {
	pending := oq.GetPendingOrder(event.OrderID)
	if pending == nil {
		return
	}
	// 移除待确认订单
	oq.RemovePendingOrder(pending.OrderID)
	// go中pending 还有效,对象不再被任何变量引用时才会回收

	logger.Info("🎉 检测到测试订单成交: %s, %s, 成交数量=%d, 成交价格=%.4f",
		pending.OrderID, pending.Order.Symbol, event.Order.Quantity, event.Order.TargetPrice)

	var profitLoss, profitRate float64 = 0, 0
	amount := event.Order.TargetPrice * float64(event.Order.Quantity)
	switch pending.Order.Category {
	case types.LongOrderCategory:
		if pending.Order.Action == types.BuyOrderAction {
			logger.Info("📈 测试做多买入成交: %s, 数量: %d, 价格: %.4f, 金额: %.2f",
				pending.Order.Symbol, event.Order.Quantity, event.Order.TargetPrice, amount)
		} else if pending.Order.Action == types.SellOrderAction && pending.Order.CloseOrderID != "" {
			closeCompletedOrder := oq.qs.GetCompletedOrder(pending.Order.CloseOrderID)
			if closeCompletedOrder != nil {
				profitLoss = (pending.Order.TargetPrice - closeCompletedOrder.Price) * float64(event.Order.Quantity)
				profitRate = profitLoss / amount

				logger.Info("✅ 测试做多卖出成交: %s, 数量: %d, 价格: %.4f, 金额: %.2f, 盈亏: %.2f (%.2f%%)",
					pending.Order.Symbol, event.Order.Quantity, event.Order.TargetPrice, amount, profitLoss, profitRate)

				// 更新交易计数
				oq.qs.IncrementDailyTradeCount()
			}
		}
	case types.ShortOrderCategory:
		if pending.Order.Action == types.SellOrderAction {
			logger.Info("📈 测试做空卖出成交: %s, 数量: %d, 价格: %.4f, 金额: %.2f",
				pending.Order.Symbol, event.Order.Quantity, event.Order.TargetPrice, amount)
		} else if pending.Order.Action == types.BuyOrderAction && pending.Order.CloseOrderID != "" {
			closeCompletedOrder := oq.qs.GetCompletedOrder(pending.Order.CloseOrderID)
			if closeCompletedOrder != nil {
				profitLoss = (closeCompletedOrder.Price - pending.Order.TargetPrice) * float64(event.Order.Quantity)
				profitRate = profitLoss / amount

				logger.Info("✅ 测试做空买入成交: %s, 数量: %d, 价格: %.4f, 金额: %.2f, 盈亏: %.2f (%.2f%%)",
					pending.Order.Symbol, event.Order.Quantity, event.Order.TargetPrice, amount, profitLoss, profitRate)

				// 更新交易计数
				oq.qs.IncrementDailyTradeCount()
			}
		}
	}
	// 创建成交订单记录
	completedOrder := &types.CompletedOrder{
		OrderID:      pending.OrderID,
		Symbol:       pending.Order.Symbol,
		Category:     pending.Order.Category,
		Action:       pending.Order.Action,
		StrategyName: pending.Order.StrategyName,
		Price:        event.Order.TargetPrice,
		Quantity:     float64(event.Order.Quantity),
		Amount:       amount,
		ProfitLoss:   profitLoss,
		ProfitRate:   profitRate,
		Timestamp:    utils.Now(),
		Reason:       pending.Order.Reason,
		CloseOrderID: pending.Order.CloseOrderID,
	}

	if pending.Order.Action == types.BuyOrderAction {
		// 通知策略买入成交
		oq.qs.notifyStrategyBuyFilled(pending.OrderID, &pending.Order)
	} else {
		// 通知策略卖出成交
		oq.qs.notifyStrategySellFilled(pending.OrderID, &pending.Order)
	}

	// 更新持仓
	oq.qs.UpdatePositionFromOrder(completedOrder)
	// 保存成交订单
	oq.qs.AddCompletedOrder(completedOrder)
}

// handleTestOrderCancelled 处理测试订单取消
func (oq *OrderQuery) handleTestOrderCancelled(event *types.TestOrderEvent) {
	logger.Warn("测试订单已取消: 订单ID=%s 股票=%s", event.Order.Symbol, event.OrderID)

	// 移除待确认订单
	oq.RemovePendingOrder(event.OrderID)

	// 通知策略订单失败
	oq.qs.notifyStrategyOrderFailed(event.OrderID, &event.Order)
}

/* 测试订单处理 end */
