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

// 网格策略
type GridStrategy struct {
	shortName    string
	name         string
	description  string
	strategyType LongStrategyType
	appConfig    *myconfig.AppConfig // 应用配置
	PriceManager types.PriceManagerInterface

	// 状态管理
	mu              sync.RWMutex
	orders          []*StrategyOrder // 所有买入订单记录
	pendingQty      int64            // 待确认买入订单的数量
	continueLossNum int              // 连续亏损次数
	lastLossTime    time.Time        // 最后亏损时间
	lastCloseTime   time.Time        // 最后平仓时间

	basePrice      float64   // 当前基准价格
	lastActionTime time.Time // 上次操作时间（对齐到分钟）
}

// 百分比网格策略
func NewGridPercentStrategy() *GridStrategy {
	return &GridStrategy{
		shortName:    "Grid",
		name:         "GridPercent",
		description:  "基于成交量的百分比网格",
		strategyType: LongTypePercent,

		orders:          make([]*StrategyOrder, 0),
		basePrice:       0,
		pendingQty:      0,
		continueLossNum: 0,
	}
}

// 获取策略短名称
func (m *GridStrategy) GetShortName() string {
	return m.shortName
}

// 获取策略名称
func (m *GridStrategy) GetName() string {
	return m.name
}

// 获取策略描述
func (m *GridStrategy) GetDescription() string {
	return m.description
}

// 获取策略类型
func (m *GridStrategy) GetStrategyType() string {
	return string(m.strategyType)
}

// 获取需要的数据类型
func (m *GridStrategy) GetRequiredData() []string {
	return []string{"quote"}
}

// 获取策略详细信息
func (m *GridStrategy) GetStrategyConfig() map[string]interface{} {
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
func (m *GridStrategy) Init(appConfig *myconfig.AppConfig) error {
	if m.appConfig != nil {
		return nil // 已经初始化过了
	}
	m.appConfig = appConfig
	// 初始化
	if GlobalStrategyManager != nil && GlobalStrategyManager.quantSystem != nil {
		m.PriceManager = GlobalStrategyManager.quantSystem.GetPriceManager()
	}
	if m.PriceManager == nil {
		logger.Fatal("m.PriceManager == nil")
		return fmt.Errorf("m.PriceManager == nil")
	}

	return nil
}

// 获取交易信号（纯网格逻辑）
func (m *GridStrategy) GetSignal(symbol string) ([]types.OrderTarget, error) {
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
	curMinuteTime := utils.Now().Truncate(time.Minute) // 对齐到分钟
	// 初始化
	if m.basePrice == 0 {
		m.basePrice = currentPrice
		m.lastActionTime = curMinuteTime
		logger.Warn("[%s] 初始化基准价: %.4f", m.name, m.basePrice)
		return nil, nil
	}

	unsoldOrders := m.getUnsoldOrdersByPriceAsc()
	var orders []types.OrderTarget

	waitDuration := time.Duration(m.appConfig.GridWaitDuration) * time.Second
	// 无订单长时间无操作重置基准（风险控制）
	if len(unsoldOrders) <= 0 && waitDuration > 0 && utils.Since(m.lastActionTime) > waitDuration {
		logger.Warn("[%s] 长时间无操作，基准价更新: %.4f -> %.4f", m.name, m.basePrice, currentPrice)
		m.basePrice = currentPrice
		m.lastActionTime = curMinuteTime
	}

	PriceType := types.PriceType(m.appConfig.GridPriceType)
	// 获取N周期内的真实波幅 ATR
	atr := m.PriceManager.GetATR(symbol, PriceType, m.appConfig.GridATR, 1)
	m.updateHighestPrice(currentPrice, atr)

	for _, order := range unsoldOrders {
		if !m.checkSellOrders(order) {
			continue
		}
		highestPrice := order.HighestPrice
		if highestPrice <= 0 {
			continue
		}

		unclosedQty := order.BoughtQuantity - order.CloseQuantity
		var reason string
		var shouldClose bool = false
		var action types.OrderAction = types.NoneOrderAction

		switch order.Category {
		case types.LongOrderCategory:
			profitPercent := (currentPrice - order.Price) / order.Price
			profit := (currentPrice - order.Price) * float64(unclosedQty)
			if currentPrice > order.Price {
				if profitPercent >= atr*m.appConfig.GridATRLongWinBase {
					reason = fmt.Sprintf("Long atr max pt:%.2f Profit:%.2f", profitPercent*100, profit)
					shouldClose = true
					action = types.SellOrderAction
				} else if profitPercent >= m.appConfig.GridMoveProfitPercent {
					// A. 移动止盈保护
					if profitPercent >= m.appConfig.GridMoveBasePercent*m.appConfig.GridMoveLongMaxMultiple {
						reason = fmt.Sprintf("Long Move max Multiple pt:%.2f Profit:%.2f", profitPercent*100, profit)
						shouldClose = true
						action = types.SellOrderAction
					} else {
						moveBasePercent := math.Floor(profitPercent / m.appConfig.GridMoveBasePercent)
						var movePercent float64 = 0
						if moveBasePercent >= 1.0 {
							movePercent = m.appConfig.GridMovePercent * moveBasePercent
						} else {
							movePercent = m.appConfig.GridMovePercent
						}

						moveProfitPercent := (order.HighestPrice - currentPrice) / order.Price
						if moveProfitPercent >= movePercent {
							reason = fmt.Sprintf("Long Move mpp:%.2f mp:%.2f pt:%.2f Profit:%.2f", moveProfitPercent*100, movePercent*100, profitPercent*100, profit)
							shouldClose = true
							action = types.SellOrderAction
						}
					}
				}
			} else {
				// 动态ATR硬止损
				if order.StopLongLossPrice > 0 && currentPrice <= order.StopLongLossPrice {
					reason = fmt.Sprintf("Long Stop pt:%.2f Profit:%.2f", profitPercent*100, profit)
					shouldClose = true
					action = types.SellOrderAction
				}
			}

		case types.ShortOrderCategory:
			profitPercent := (order.Price - currentPrice) / order.Price
			profit := (order.Price - currentPrice) * float64(unclosedQty)
			if currentPrice < order.Price {
				if profitPercent >= atr*m.appConfig.GridATRShortWinBase {
					reason = fmt.Sprintf("Short atr max pt:%.2f Profit:%.2f", profitPercent*100, profit)
					shouldClose = true
					action = types.BuyOrderAction
				} else if profitPercent >= m.appConfig.GridMoveProfitPercent {
					// A. 空单移动止盈
					if profitPercent >= m.appConfig.GridMoveBasePercent*m.appConfig.GridMoveShortMaxMultiple {
						reason = fmt.Sprintf("Short Move max Multiple pt:%.2f Profit:%.2f", profitPercent*100, profit)
						shouldClose = true
						action = types.BuyOrderAction
					} else {
						moveBasePercent := math.Floor(profitPercent / m.appConfig.GridMoveBasePercent)
						var movePercent float64 = 0
						if moveBasePercent >= 1.0 {
							movePercent = m.appConfig.GridMovePercent * moveBasePercent
						} else {
							movePercent = m.appConfig.GridMovePercent
						}

						moveProfitPercent := (currentPrice - order.LowestPrice) / order.Price
						if moveProfitPercent >= movePercent {
							reason = fmt.Sprintf("Short Move mpp:%.2f mp:%.2f pt:%.2f Profit:%.2f", moveProfitPercent*100, movePercent*100, profitPercent*100, profit)
							shouldClose = true
							action = types.BuyOrderAction
						}
					}
				}
			} else {
				// 动态ATR止损
				if order.StopShortLossPrice > 0 && currentPrice >= order.StopShortLossPrice {
					reason = fmt.Sprintf("Short Stop pt:%.2f Profit:%.2f", profitPercent*100, profit)
					shouldClose = true
					action = types.BuyOrderAction
				}
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
	if m.continueLossNum >= m.appConfig.StrategyContinueLossNum &&
		utils.Since(m.lastLossTime) < time.Duration(m.appConfig.StrategyContinueLossStopMinute)*time.Minute {
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

	// 计算价格偏离百分比
	changePercent := (currentPrice - m.basePrice) / m.basePrice // 正值表示上涨

	// 趋势过滤: 用MA60判断多空方向
	ma60 := m.PriceManager.GetAveragePrice(symbol, PriceType, m.appConfig.GridMAPre, 1)

	isBuy := changePercent >= m.appConfig.GridPercentStep
	isSell := changePercent <= -m.appConfig.GridPercentStep
	if isBuy && ma60 > 0 && currentPrice > ma60 {
		isBuy = false
	}
	if isSell && ma60 > 0 && currentPrice < ma60 {
		isSell = false
	}

	if isBuy {
		m.basePrice = currentPrice // 基准下移
		m.pendingQty += quantity
		orders = append(orders, types.OrderTarget{
			Symbol:       symbol,
			Category:     types.LongOrderCategory,
			Action:       types.BuyOrderAction,
			StrategyName: m.name,
			TargetPrice:  currentPrice,
			Quantity:     quantity,
			Reason:       fmt.Sprintf("Up Trend, Vol Confirmed Buy, cp=%.2f%%", changePercent*100),
			CloseOrderID: "",
		})
		logger.Info("[%s] >> 触发周期做多信号，买入价格: %.4f, cp=%.2f%%", m.name, currentPrice, changePercent*100)
	} else if isSell {
		m.basePrice = currentPrice // 基准上移
		m.pendingQty += quantity
		orders = append(orders, types.OrderTarget{
			Symbol:       symbol,
			Category:     types.ShortOrderCategory,
			Action:       types.SellOrderAction,
			StrategyName: m.name,
			TargetPrice:  currentPrice,
			Quantity:     quantity,
			Reason:       fmt.Sprintf("Down Trend, Vol Confirmed Sell, cp=%.2f%%", changePercent*100),
			CloseOrderID: "",
		})
		logger.Info("[%s] >> 触发周期做空信号，开空价格: %.4f, cp=%.2f%%", m.name, currentPrice, changePercent*100)
	}
	logger.Info("[%s] Price=%.4f", m.name, currentPrice)

	return orders, nil
}

func (m *GridStrategy) isInCooldown() bool {
	return utils.Since(m.lastCloseTime) < time.Duration(m.appConfig.OpenCooldown)*time.Second
}

// 卖出所有
func (m *GridStrategy) sellAll(symbol string, currentPrice float64, reason string) []types.OrderTarget {
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

func (m *GridStrategy) checkSellOrders(order *StrategyOrder) bool {
	// 1. 检查是否有剩余持仓
	if order.BoughtQuantity <= order.CloseQuantity {
		return false
	}

	// NoneStatus 或 FailedStatus 允许卖出
	return order.CloseStatus == types.NoneStatus || order.CloseStatus == types.FailedStatus
}

// 更新最高价
func (m *GridStrategy) updateHighestPrice(currentPrice float64, atr float64) {
	for _, order := range m.orders {
		if order.BoughtQuantity <= order.CloseQuantity {
			continue
		}
		switch order.Category {
		case types.LongOrderCategory:
			if order.HighestPrice == 0 || currentPrice >= order.HighestPrice {
				order.HighestPrice = currentPrice
			}
			newStopLoss := order.Price - (atr * m.appConfig.GridATRLongStopBase)
			if order.StopLongLossPrice == 0 || newStopLoss > order.StopLongLossPrice {
				order.StopLongLossPrice = newStopLoss
				logger.Info("atr=%.4f, Price=%.4f, currentPrice=%.4f, StopLongLossPrice=%.4f", atr, order.Price, currentPrice, newStopLoss)
			}
		case types.ShortOrderCategory:
			if order.LowestPrice == 0 || currentPrice <= order.LowestPrice {
				order.LowestPrice = currentPrice
			}
			newShortStopLoss := order.Price + (atr * m.appConfig.GridATRShortStopBase)
			if order.StopShortLossPrice == 0 || newShortStopLoss < order.StopShortLossPrice {
				order.StopShortLossPrice = newShortStopLoss
				logger.Info("atr=%.4f, Price=%.4f, currentPrice=%.4f, StopShortLossPrice=%.4f", atr, order.Price, currentPrice, newShortStopLoss)
			}
		}
	}
}

// 获取总持仓数量
func (m *GridStrategy) getTotalQuantity() int64 {
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
func (m *GridStrategy) getUnsoldCount() int {
	count := 0
	for _, order := range m.orders {
		if order.BoughtQuantity > order.CloseQuantity {
			count++
		}
	}
	return count
}

// 获取未完全卖出的订单（按买入价格从低到高排序）
func (m *GridStrategy) getUnsoldOrdersByPriceAsc() []*StrategyOrder {
	unsold := make([]*StrategyOrder, 0)
	for _, order := range m.orders {
		if order.BoughtQuantity > order.CloseQuantity {
			unsold = append(unsold, order)
		}
	}

	sort.Slice(unsold, func(i, j int) bool {
		return unsold[i].Price < unsold[j].Price
	})

	return unsold
}

// 买入订单已提交
func (m *GridStrategy) OnBuyOrderSubmitted(orderID string, order *types.OrderTarget) {
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
func (m *GridStrategy) OnBuyOrderFilled(orderID string, order *types.OrderTarget) {
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

				HighestPrice: order.TargetPrice,
				LowestPrice:  order.TargetPrice,
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
func (m *GridStrategy) OnSellOrderSubmitted(orderID string, order *types.OrderTarget) {
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
func (m *GridStrategy) OnSellOrderFilled(orderID string, order *types.OrderTarget) {
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
				HighestPrice:   order.TargetPrice,
				LowestPrice:    order.TargetPrice,
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
func (m *GridStrategy) OnOrderFailed(orderID string, order *types.OrderTarget) {
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
func (m *GridStrategy) GetCurrentState() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allOrders := make([]map[string]interface{}, 0)
	for _, order := range m.orders {
		orderInfo := map[string]interface{}{
			"order_id":       order.OrderID,
			"symbol":         order.Symbol,
			"category":       int32(order.Category),
			"action":         string(order.Action),
			"strategy_name":  order.StrategyName,
			"price":          order.Price,
			"quantity":       order.Quantity,
			"bought_qty":     order.BoughtQuantity,
			"sold_qty":       order.CloseQuantity,
			"remaining":      order.BoughtQuantity - order.CloseQuantity,
			"close_order_id": order.CloseOrderID,
			"close_price":    order.ClosePrice,
			"profit":         order.Profit,
		}
		allOrders = append(allOrders, orderInfo)
	}

	state := map[string]interface{}{
		"strategy_type":  string(m.strategyType),
		"base_price":     m.basePrice,
		"total_quantity": m.getTotalQuantity(),
		"unsold_count":   m.getUnsoldCount(),
		"orders":         allOrders,
		"pending_qty":    m.pendingQty,
	}

	return state
}

// 重置策略状态
func (m *GridStrategy) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.basePrice = 0
	m.orders = make([]*StrategyOrder, 0)
	m.pendingQty = 0
	m.lastActionTime = utils.Now().Truncate(time.Minute) // 对齐到分钟
}

// cmd卖出 - 返回多个订单
func (m *GridStrategy) CmdSell(symbol string, cmdSellInfo string) []types.OrderTarget {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 获取当前价格
	currentPrice := m.PriceManager.GetCurrentPrice(symbol)
	var orders []types.OrderTarget
	reason := fmt.Sprintf("指令主动卖出: %s", cmdSellInfo)
	logger.Warn("处理 sell 股票：%s cmdSellInfo:%s, currentPrice：%.4f", symbol, cmdSellInfo, currentPrice)
	if cmdSellInfo == "all" {
		for _, order := range m.orders {
			if order.BoughtQuantity <= order.CloseQuantity {
				continue
			}
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
	} else if cmdSellInfo == "win" {
		for _, order := range m.orders {
			if order.BoughtQuantity <= order.CloseQuantity {
				continue
			}
			if order.Price > currentPrice {
				continue
			}
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
	} else if cmdSellInfo == "lost" {
		for _, order := range m.orders {
			if order.BoughtQuantity <= order.CloseQuantity {
				continue
			}
			if order.Price < currentPrice {
				continue
			}
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
	}
	return orders
}

// cmd买入 - 返回多个订单
func (m *GridStrategy) CmdBuy(symbol string, cmdBuyInfo string) []types.OrderTarget {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 获取当前价格
	currentPrice := m.PriceManager.GetCurrentPrice(symbol)
	buyQuantity, err := utils.StringToInt64(cmdBuyInfo)
	logger.Warn("处理 buy 股票：%s buyQuantity：%d", symbol, buyQuantity)
	if err != nil {
		logger.Warn("转换失败:%v, cmdBuyInfo=%s", err, cmdBuyInfo)
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
		Reason:       fmt.Sprintf("指令主动买入数量：%d", buyQuantity),
		CloseOrderID: "",
	}}
}
