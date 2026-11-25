package strategy

import (
	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

type TimeSignalStrategy struct {
	shortName    string
	name         string
	description  string
	strategyType string
	appConfig    *myconfig.AppConfig // 应用配置
	PriceManager types.PriceManagerInterface

	// 状态管理
	mu              sync.RWMutex
	orders          []*StrategyOrder // 所有买入订单记录
	pendingQty      int64            // 待确认数量
	continueLossNum int              // 连续亏损次数
	lastLossTime    time.Time        // 最后亏损时间
	lastCloseTime   time.Time        // 最后买卖时间
}

func NewTimeSignalStrategy() *TimeSignalStrategy {
	return &TimeSignalStrategy{
		shortName:    "TimeSignal",
		name:         "TimeSignal",
		description:  "时间信号",
		strategyType: "",

		orders:          make([]*StrategyOrder, 0),
		pendingQty:      0,
		continueLossNum: 0,
	}
}

// 获取策略短名称
func (m *TimeSignalStrategy) GetShortName() string {
	return m.shortName
}

// 获取策略名称
func (m *TimeSignalStrategy) GetName() string {
	return m.name
}

// 获取策略描述
func (m *TimeSignalStrategy) GetDescription() string {
	return m.description
}

// 获取策略类型
func (m *TimeSignalStrategy) GetStrategyType() string {
	return string(m.strategyType)
}

// 获取需要的数据类型
func (m *TimeSignalStrategy) GetRequiredData() []string {
	return []string{"quote"}
}

// 获取策略详细信息
func (m *TimeSignalStrategy) GetStrategyConfig() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"shortName":    m.shortName,
		"name":         m.name,
		"description":  m.description,
		"strategyType": m.strategyType,
	}
}

// 初始化策略
func (m *TimeSignalStrategy) Init(appConfig *myconfig.AppConfig) error {
	if m.appConfig != nil {
		return nil // 已经初始化过了
	}
	m.appConfig = appConfig
	// 初始化 PriceManager
	if GlobalStrategyManager != nil && GlobalStrategyManager.quantSystem != nil {
		m.PriceManager = GlobalStrategyManager.quantSystem.GetPriceManager()
	}
	if m.PriceManager == nil {
		logger.Fatal("m.PriceManager == nil")
		return fmt.Errorf("m.PriceManager == nil")
	}

	return nil
}

// 获取交易信号
func (m *TimeSignalStrategy) GetSignal(symbol string) ([]types.OrderTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.appConfig == nil {
		return nil, fmt.Errorf("策略未初始化")
	}
	if m.PriceManager == nil {
		return nil, fmt.Errorf("PriceManager == nil")
	}
	// 获取当前价格
	currentPrice := m.PriceManager.GetCurrentPrice(symbol)

	unsoldOrders := m.getUnsoldOrdersByPriceAsc()
	var orders []types.OrderTarget

	var isBuy, isSell bool = false, false
	signalResult := m.PriceManager.CheckSignals(symbol)
	switch signalResult.Action {
	case types.BuyOrderAction:
		isBuy = true
	case types.SellOrderAction:
		isSell = true
	}
	if isBuy || isSell {
		logger.Info("isSell=%v, isBuy=%v, Reason=%s, currentPrice=%.4f",
			isSell, isBuy, signalResult.Reason, currentPrice)
	}

	// 1. 止损/止盈优先检查
	for _, order := range unsoldOrders {
		if !m.checkSellOrders(order) {
			continue
		}
		if order.Price <= 0 {
			continue
		}
		unclosedQty := order.BoughtQuantity - order.CloseQuantity
		var reason string
		var shouldClose bool = false
		var action types.OrderAction = types.NoneOrderAction

		switch order.Category {
		case types.LongOrderCategory:
			if isSell {
				if m.appConfig.SignalLongPresent > 0 {
					changePresent := math.Abs((currentPrice - order.Price) / order.Price)
					if changePresent < m.appConfig.SignalLongPresent {
						continue
					}
				}

				reason = fmt.Sprintf("%s %.4f", signalResult.Reason, (currentPrice-order.Price)*float64(unclosedQty))
				shouldClose = true
				action = types.SellOrderAction

				logger.Warn("[%s] 多单平仓信号: 原因=%s, 买入价=%.4f, 当前价=%.4f, 利润=%.4f",
					m.name, reason, order.Price, currentPrice, (currentPrice-order.Price)*float64(unclosedQty))
			}

		case types.ShortOrderCategory:
			if isBuy {
				if m.appConfig.SignalShortPresent > 0 {
					changePresent := math.Abs((currentPrice - order.Price) / order.Price)
					if changePresent < m.appConfig.SignalShortPresent {
						continue
					}
				}

				reason = fmt.Sprintf("%s %.4f", signalResult.Reason, (order.Price-currentPrice)*float64(unclosedQty))
				shouldClose = true
				action = types.BuyOrderAction

				logger.Warn("[%s] 空单平仓信号: 原因=%s, 买入价=%.4f, 当前价=%.4f",
					m.name, reason, order.Price, currentPrice)
			}
		}
		if shouldClose && action != types.NoneOrderAction {
			order.CloseStatus = types.PendingStatus
			orders = append(orders, types.OrderTarget{
				Symbol:       symbol,
				Category:     order.Category,
				Action:       action,
				StrategyName: order.StrategyName,
				TargetPrice:  currentPrice,
				Quantity:     unclosedQty,
				Reason:       reason,
				CloseOrderID: order.OrderID,
			})
		}
	}

	if len(orders) > 0 {
		return orders, nil
	}
	if len(unsoldOrders) >= m.appConfig.StrategyUnSoldOrderNum || m.pendingQty > 0 {
		return orders, nil
	}
	// 连续亏损熔断,只判断时间,应该可能会切换策略,给策略冷却时间
	if utils.Since(m.lastLossTime) < time.Duration(m.appConfig.StrategyContinueLossStopMinute)*time.Minute {
		logger.Info("[%s] 连续亏损%d次,暂停%d分钟交易", m.name, m.continueLossNum, m.appConfig.StrategyContinueLossStopMinute)
		return orders, nil
	}
	if m.isInCooldown() {
		return orders, nil
	}

	if GlobalStrategyManager == nil {
		logger.Error("GlobalStrategyManager == nil")
		return orders, nil
	}
	if !GlobalStrategyManager.CanSessionOrder() {
		return orders, nil
	}
	quantity := GlobalStrategyManager.GetQuantity()
	if quantity <= 0 {
		return orders, nil
	}

	signalType := types.SignalTrendType(m.appConfig.SignalType)
	if signalType <= types.SignalTrendMin || signalType >= types.SignalTrendMax {
		signalType = types.SignalTrendLong
	}
	// 做多
	if isBuy {
		if signalType != types.SignalTrendLong {
			return orders, nil
		}

		m.pendingQty += quantity
		// 做多买入
		reason := fmt.Sprintf("%s", signalResult.Reason)
		orders = append(orders, types.OrderTarget{
			Symbol:       symbol,
			Category:     types.LongOrderCategory,
			Action:       types.BuyOrderAction,
			StrategyName: m.name,
			TargetPrice:  currentPrice,
			Quantity:     quantity,
			Reason:       reason,
			CloseOrderID: "",
		})
		logger.Info("[%s] 做多买入信号: 原因=%s, 买入价=%.4f", m.name, reason, currentPrice)
	} else if isSell {
		if signalType != types.SignalTrendShort {
			return orders, nil
		}
		m.pendingQty += quantity
		reason := fmt.Sprintf("%s", signalResult.Reason)
		orders = append(orders, types.OrderTarget{
			Symbol:       symbol,
			Category:     types.ShortOrderCategory,
			Action:       types.SellOrderAction,
			StrategyName: m.name,
			TargetPrice:  currentPrice,
			Quantity:     quantity,
			Reason:       reason,
			CloseOrderID: "",
		})
		logger.Info("[%s] 做空开仓信号: 原因=%s, 买入价=%.4f", m.name, reason, currentPrice)
	}

	return orders, nil
}

func (m *TimeSignalStrategy) isInCooldown() bool {
	return utils.Since(m.lastCloseTime) < time.Duration(m.appConfig.OpenCooldown)*time.Second
}

// 卖出所有
func (m *TimeSignalStrategy) sellAll(symbol string, currentPrice float64, reason string) []types.OrderTarget {
	// 获取未完全卖出的订单（按买入价格从低到高排序）
	unsoldOrders := m.getUnsoldOrdersByPriceAsc()
	if len(unsoldOrders) == 0 {
		return nil
	}

	var orders []types.OrderTarget
	for _, order := range unsoldOrders {
		if !m.checkSellOrders(order) {
			continue
		}
		order.CloseStatus = types.PendingStatus
		orders = append(orders, types.OrderTarget{
			Symbol:       symbol,
			Category:     types.LongOrderCategory,
			Action:       types.SellOrderAction,
			StrategyName: order.StrategyName,
			TargetPrice:  currentPrice,
			Quantity:     order.BoughtQuantity - order.CloseQuantity,
			Reason:       reason,
			CloseOrderID: order.OrderID,
		})
	}
	return orders
}

func (m *TimeSignalStrategy) checkSellOrders(order *StrategyOrder) bool {
	// 1. 检查是否有剩余持仓
	if order.BoughtQuantity-order.CloseQuantity <= 0 {
		return false
	}

	// NoneStatus 或 FailedStatus 允许卖出
	return order.CloseStatus == types.NoneStatus || order.CloseStatus == types.FailedStatus
}

// 获取总持仓数量
func (m *TimeSignalStrategy) getTotalQuantity() int64 {
	var total int64
	for _, order := range m.orders {
		remaining := order.BoughtQuantity - order.CloseQuantity
		if remaining > 0 {
			total += remaining
		}
	}
	return total
}

// 获取未完全卖出的订单数量
func (m *TimeSignalStrategy) getUnsoldCount() int {
	count := 0
	for _, order := range m.orders {
		if order.BoughtQuantity-order.CloseQuantity > 0 {
			count++
		}
	}
	return count
}

// 获取未完全卖出的订单（按买入价格从低到高排序）
func (m *TimeSignalStrategy) getUnsoldOrdersByPriceAsc() []*StrategyOrder {
	unsold := make([]*StrategyOrder, 0)
	for _, order := range m.orders {
		if order.BoughtQuantity-order.CloseQuantity > 0 {
			unsold = append(unsold, order)
		}
	}

	sort.Slice(unsold, func(i, j int) bool {
		return unsold[i].Price < unsold[j].Price
	})

	return unsold
}

// 买入订单已提交
func (m *TimeSignalStrategy) OnBuyOrderSubmitted(orderID string, order *types.OrderTarget) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, v := range m.orders {
		if v.OrderID == order.CloseOrderID && (v.BoughtQuantity-v.CloseQuantity) > 0 && v.CloseOrderID == "" {
			v.CloseOrderID = orderID
			v.CloseStatus = types.SubmittedStatus
			logger.Info("[%s] 平仓订单已提交: 开仓订单ID=%s, 平仓订单ID=%s",
				m.name, orderID, order.CloseOrderID)
			break
		}
	}
}

// 买入订单成交
func (m *TimeSignalStrategy) OnBuyOrderFilled(orderID string, order *types.OrderTarget) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 做多
	if order.Category == types.LongOrderCategory {
		// 检查是否已有该订单
		var existingOrder *StrategyOrder
		for _, v := range m.orders {
			if v.OrderID == orderID {
				existingOrder = v
				break
			}
		}

		if existingOrder != nil {
			// 部分成交
			existingOrder.BoughtQuantity += order.Quantity
			existingOrder.Time = utils.Now()
			logger.Info("[%s] 买入部分成交: 订单ID=%s, 本次=%d, 累计=%d/%d",
				m.name, orderID, order.Quantity, existingOrder.BoughtQuantity, existingOrder.Quantity)
		} else {
			// 新订单
			newOrder := &StrategyOrder{
				Symbol:         order.Symbol,
				Category:       order.Category,
				Action:         order.Action,
				StrategyName:   order.StrategyName,
				OrderID:        orderID,
				Price:          order.TargetPrice,
				Quantity:       order.Quantity,
				BoughtQuantity: order.Quantity,
				Time:           utils.Now(),
				CloseQuantity:  0,
			}
			m.orders = append(m.orders, newOrder)

			logger.Info("[%s] 买入成交: 订单ID=%s, 价格=%.4f, 数量=%d",
				m.name, orderID, order.TargetPrice, order.Quantity)
		}

		if m.pendingQty >= order.Quantity {
			m.pendingQty -= order.Quantity
		} else {
			m.pendingQty = 0
		}
		m.lastCloseTime = utils.Now()
		logger.Info("[%s] 订单ID:%s, 股票:%s 收到买入成交成功,总待买入数量:%d, 买入价:%.4f",
			m.name, orderID, order.Symbol, m.pendingQty, order.TargetPrice)
	} else {
		var matchedOrder *StrategyOrder
		for _, v := range m.orders {
			if v.CloseOrderID == orderID {
				matchedOrder = v
				break
			}
		}
		if matchedOrder == nil {
			logger.Warn("[%s] 平仓买入成交回调: 找不到对应的卖出订单, 订单ID=%s", m.name, orderID)
			return
		}

		quantity := order.Quantity
		price := order.TargetPrice

		// 计算盈亏
		profit := float64(quantity) * (matchedOrder.Price - price)
		matchedOrder.CloseQuantity += quantity
		matchedOrder.Profit += profit

		if matchedOrder.CloseQuantity >= matchedOrder.BoughtQuantity {
			matchedOrder.ClosePrice = price
			matchedOrder.CloseOrderID = orderID
			matchedOrder.CloseStatus = types.CompletedStatus
			matchedOrder.CloseTime = utils.Now()
			logger.Info("[%s] 买入订单 %s 完全卖出!", m.name, matchedOrder.OrderID)

			if matchedOrder.Profit < 0 {
				m.continueLossNum++
				m.lastLossTime = utils.Now()

				// 自动切换策略
				if m.appConfig.StrategyContinueLossSwitch && m.continueLossNum >= m.appConfig.StrategyContinueLossNum {
					oldSignalType := m.appConfig.SignalType
					if m.appConfig.SignalType == int(types.SignalTrendLong) {
						m.appConfig.SignalType = int(types.SignalTrendShort)
					} else {
						m.appConfig.SignalType = int(types.SignalTrendLong)
					}
					m.continueLossNum = 0

					logger.Warn("策略切换 oldSignalType=%d SignalType=%d", oldSignalType, m.appConfig.SignalType)
				}
			} else {
				m.continueLossNum = 0
			}
		} else {
			matchedOrder.CloseStatus = types.PartialStatus
			matchedOrder.CloseTime = utils.Now()
		}

		m.lastCloseTime = utils.Now()
		logger.Info("[%s] 平仓买入成交: 买入订单ID=%s, 买入价=%.4f, 卖出价=%.4f, 数量=%d, 盈亏:%.2f, 买入价:%.4f",
			m.name, orderID, matchedOrder.Price, price, quantity, profit, price)
	}
}

// 卖出订单已提交
func (m *TimeSignalStrategy) OnSellOrderSubmitted(orderID string, order *types.OrderTarget) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, v := range m.orders {
		if v.OrderID == order.CloseOrderID && (v.BoughtQuantity-v.CloseQuantity) > 0 && v.CloseOrderID == "" {
			v.CloseOrderID = orderID
			v.CloseStatus = types.SubmittedStatus
			logger.Info("[%s] 卖出订单已提交: 卖出订单ID=%s, 买入订单ID=%s",
				m.name, orderID, order.CloseOrderID)
			break
		}
	}
}

// 卖出订单成交
func (m *TimeSignalStrategy) OnSellOrderFilled(orderID string, order *types.OrderTarget) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 做多
	if order.Category == types.LongOrderCategory {
		var matchedOrder *StrategyOrder
		for _, v := range m.orders {
			if v.CloseOrderID == orderID {
				matchedOrder = v
				break
			}
		}
		if matchedOrder == nil {
			logger.Warn("[%s] 卖出成交回调: 找不到对应的买入订单, 订单ID=%s", m.name, orderID)
			return
		}

		quantity := order.Quantity
		price := order.TargetPrice

		// 计算盈亏
		profit := float64(quantity) * (price - matchedOrder.Price)
		matchedOrder.CloseQuantity += quantity
		matchedOrder.Profit += profit

		if matchedOrder.CloseQuantity >= matchedOrder.BoughtQuantity {
			matchedOrder.ClosePrice = price
			matchedOrder.CloseOrderID = orderID
			matchedOrder.CloseStatus = types.CompletedStatus
			matchedOrder.CloseTime = utils.Now()
			logger.Info("[%s] 买入订单 %s 完全卖出!", m.name, matchedOrder.OrderID)

			if matchedOrder.Profit < 0 {
				m.continueLossNum++
				m.lastLossTime = utils.Now()

				// 自动切换策略
				if m.appConfig.StrategyContinueLossSwitch && m.continueLossNum >= m.appConfig.StrategyContinueLossNum {
					oldSignalType := m.appConfig.SignalType
					if m.appConfig.SignalType == int(types.SignalTrendLong) {
						m.appConfig.SignalType = int(types.SignalTrendShort)
					} else {
						m.appConfig.SignalType = int(types.SignalTrendLong)
					}
					m.continueLossNum = 0

					logger.Warn("策略切换 oldSignalType=%d SignalType=%d", oldSignalType, m.appConfig.SignalType)
				}
			} else {
				m.continueLossNum = 0
			}
		} else {
			matchedOrder.CloseStatus = types.PartialStatus
			matchedOrder.CloseTime = utils.Now()
		}
		m.lastCloseTime = utils.Now()
		logger.Info("[%s] 卖出成交: 卖出订单ID=%s, 买入价=%.4f, 卖出价=%.4f, 数量=%d, 盈亏=%.4f",
			m.name, orderID, matchedOrder.Price, price, quantity, profit)
	} else {
		// 检查是否已有该订单
		var existingOrder *StrategyOrder
		for _, v := range m.orders {
			if v.CloseOrderID == orderID {
				existingOrder = v
				break
			}
		}

		if existingOrder != nil {
			// 部分成交
			existingOrder.BoughtQuantity += order.Quantity
			existingOrder.Time = utils.Now()
			logger.Info("[%s] 做空卖出部分成交: 订单ID=%s, 本次=%d, 累计=%d/%d",
				m.name, orderID, order.Quantity, existingOrder.BoughtQuantity, existingOrder.Quantity)
		} else {
			// 新订单
			newOrder := &StrategyOrder{
				Symbol:         order.Symbol,
				Category:       order.Category,
				Action:         order.Action,
				StrategyName:   order.StrategyName,
				OrderID:        orderID,
				Price:          order.TargetPrice,
				Quantity:       order.Quantity,
				BoughtQuantity: order.Quantity,
				Time:           utils.Now(),
				CloseQuantity:  0,
			}
			m.orders = append(m.orders, newOrder)

			logger.Info("[%s] 做空卖出成交: 订单ID=%s, 价格=%.4f, 数量=%d",
				m.name, orderID, order.TargetPrice, order.Quantity)
		}

		if m.pendingQty >= order.Quantity {
			m.pendingQty -= order.Quantity
		} else {
			m.pendingQty = 0
		}
		m.lastCloseTime = utils.Now()
		logger.Info("[%s] 做空卖出成交: 订单ID:%s, 股票:%s 总待买入数量：%d",
			m.name, orderID, order.Symbol, m.pendingQty)
	}
}

// 订单失败回调
func (m *TimeSignalStrategy) OnOrderFailed(orderID string, order *types.OrderTarget) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, v := range m.orders {
		if v.CloseOrderID == orderID {
			logger.Info("[%s] 订单失败，重置绑定状态: 绑定订单ID=%s", m.name, v.OrderID)
			v.CloseOrderID = ""
			v.CloseStatus = types.FailedStatus
			break
		}
	}

	find := false
	for _, v := range m.orders {
		if v.OrderID == orderID {
			logger.Info("[%s] 订单失败，重置待确认状态, 订单ID=%s pendingQty:%d", m.name, orderID, m.pendingQty)
			if m.pendingQty > 0 {
				if m.pendingQty >= order.Quantity {
					m.pendingQty -= order.Quantity
				} else {
					m.pendingQty = 0
				}
				find = true
			}
			break
		}
	}
	// 没成交的订单取消
	if !find && m.pendingQty > 0 {
		if m.pendingQty >= order.Quantity {
			m.pendingQty -= order.Quantity
		} else {
			m.pendingQty = 0
		}
	}
	logger.Info("[%s] 订单失败, 订单ID:%s, 股票:%s 总待买入数量：%d", m.name, orderID, order.Symbol, m.pendingQty)
}

// 获取当前策略状态
func (m *TimeSignalStrategy) GetCurrentState() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allOrders := make([]map[string]interface{}, 0)
	for _, order := range m.orders {
		orderInfo := map[string]interface{}{
			"symbol":         order.Symbol,
			"category":       int32(order.Category),
			"action":         string(order.Action),
			"strategy_name":  order.StrategyName,
			"order_id":       order.OrderID,
			"price":          order.Price,
			"quantity":       order.Quantity,
			"bought_qty":     order.BoughtQuantity,
			"time":           order.Time,
			"sold_qty":       order.CloseQuantity,
			"remaining":      order.BoughtQuantity - order.CloseQuantity,
			"close_order_id": order.CloseOrderID,
			"close_price":    order.ClosePrice,
			"close_time":     order.CloseTime,
			"profit":         order.Profit,
		}
		allOrders = append(allOrders, orderInfo)
	}

	sort.Slice(allOrders, func(i, j int) bool {
		ti := allOrders[i]["time"].(time.Time)
		tj := allOrders[j]["time"].(time.Time)
		return ti.Before(tj)
	})

	state := map[string]interface{}{
		"strategy_type":  string(m.strategyType),
		"total_quantity": m.getTotalQuantity(),
		"unsold_count":   m.getUnsoldCount(),
		"orders":         allOrders,
		"pending_qty":    m.pendingQty,
	}

	return state
}

// 重置策略状态
func (m *TimeSignalStrategy) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.orders = make([]*StrategyOrder, 0)
	m.pendingQty = 0
}

// cmd卖出 - 返回多个订单
func (m *TimeSignalStrategy) CmdSell(symbol string, cmdSellInfo string) []types.OrderTarget {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 获取当前价格
	currentPrice := m.PriceManager.GetCurrentPrice(symbol)
	var orders []types.OrderTarget
	// 1. 止损/止盈优先检查
	unsoldOrders := m.getUnsoldOrdersByPriceAsc()
	for _, order := range unsoldOrders {
		if !m.checkSellOrders(order) {
			continue
		}
		switch order.Category {
		case types.LongOrderCategory:
			reason := "Cmd Long Sell"
			order.CloseStatus = types.PendingStatus
			orders = append(orders, types.OrderTarget{
				Symbol:       symbol,
				Category:     order.Category,
				Action:       types.SellOrderAction,
				StrategyName: order.StrategyName,
				TargetPrice:  currentPrice,
				Quantity:     order.BoughtQuantity - order.CloseQuantity,
				Reason:       reason,
				CloseOrderID: order.OrderID,
			})

			logger.Info("[%s] 指令主动做多卖出: 原因=%s, 买入价=%.4f, 当前价=%.4f",
				m.name, reason, order.Price, currentPrice)
		case types.ShortOrderCategory:
			reason := "Cmd Short Close"
			order.CloseStatus = types.PendingStatus
			orders = append(orders, types.OrderTarget{
				Symbol:       symbol,
				Category:     order.Category,
				Action:       types.BuyOrderAction,
				StrategyName: order.StrategyName,
				TargetPrice:  currentPrice,
				Quantity:     order.BoughtQuantity - order.CloseQuantity,
				Reason:       reason,
				CloseOrderID: order.OrderID,
			})
			logger.Info("[%s] 指令主动做空平仓: 原因=%s, 买入价=%.4f, 当前价=%.4f",
				m.name, reason, order.Price, currentPrice)
		}
	}
	return orders
}

// cmd买入 - 返回多个订单
func (m *TimeSignalStrategy) CmdBuy(symbol string, cmdBuyInfo string) []types.OrderTarget {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 获取当前价格
	currentPrice := m.PriceManager.GetCurrentPrice(symbol)
	buyQuantity, err := utils.StringToInt64(cmdBuyInfo)
	if err != nil {
		logger.Warn("转换失败:%v, cmdBuyInfo=%s", err, cmdBuyInfo)
		return nil
	}
	unsoldOrders := m.getUnsoldOrdersByPriceAsc()
	if len(unsoldOrders) > 0 {
		logger.Warn("存在订单,购买失败 len(unsoldOrders)=%d", len(unsoldOrders))
		return nil
	}

	m.pendingQty += buyQuantity
	return []types.OrderTarget{{
		Symbol:       symbol,
		Category:     types.LongOrderCategory,
		Action:       types.BuyOrderAction,
		StrategyName: m.name,
		TargetPrice:  currentPrice,
		Quantity:     buyQuantity,
		Reason:       fmt.Sprintf("Cmd Buy Num:%d", buyQuantity),
		CloseOrderID: "",
	}}
}
