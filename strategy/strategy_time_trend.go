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

type TimeTrendStrategy struct {
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
	lastCloseTime   time.Time        // 最后平仓时间

	buyTrendCount  int
	sellTrendCount int
}

func NewTimeTrendStrategy() *TimeTrendStrategy {
	return &TimeTrendStrategy{
		shortName:    "TimeTrend",
		name:         "TimeTrend",
		description:  "时间趋势",
		strategyType: "",

		orders:          make([]*StrategyOrder, 0),
		pendingQty:      0,
		continueLossNum: 0,

		buyTrendCount:  0,
		sellTrendCount: 0,
	}
}

// 获取策略短名称
func (m *TimeTrendStrategy) GetShortName() string {
	return m.shortName
}

// 获取策略名称
func (m *TimeTrendStrategy) GetName() string {
	return m.name
}

// 获取策略描述
func (m *TimeTrendStrategy) GetDescription() string {
	return m.description
}

// 获取策略类型
func (m *TimeTrendStrategy) GetStrategyType() string {
	return string(m.strategyType)
}

// 获取需要的数据类型
func (m *TimeTrendStrategy) GetRequiredData() []string {
	return []string{"quote"}
}

// 获取策略详细信息
func (m *TimeTrendStrategy) GetStrategyConfig() map[string]interface{} {
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
func (m *TimeTrendStrategy) Init(appConfig *myconfig.AppConfig) error {
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
func (m *TimeTrendStrategy) GetSignal(symbol string) ([]types.OrderTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.appConfig == nil {
		return nil, fmt.Errorf("策略未初始化")
	}
	if m.PriceManager == nil {
		return nil, fmt.Errorf("PriceManager == nil")
	}

	// 0. 获取当前秒级最新价
	currentPrice := m.PriceManager.GetCurrentPrice(symbol)
	unsoldOrders := m.getUnsoldOrdersByPriceAsc()
	var orders []types.OrderTarget

	// ==========================================
	// 1. 核心时间窗口定义 (N周期窗口控制)
	// ==========================================
	TrendMALong := m.appConfig.TrendMALong
	TrendMAShort := m.appConfig.TrendMAShort
	TrendMAPre := m.appConfig.TrendMAPre
	PriceType := types.PriceType(m.appConfig.TrendPriceType)

	// 计算N秒周期的长期均线，以及短期趋势线
	maLong := m.PriceManager.GetAveragePrice(symbol, PriceType, TrendMALong, 1)   // N秒大趋势
	maShort := m.PriceManager.GetAveragePrice(symbol, PriceType, TrendMAShort, 1) // 短期爆发力
	if maLong == 0 || maShort == 0 {
		return orders, nil
	}

	// N周期确认大趋势方向
	maLongPrev := m.PriceManager.GetAveragePrice(symbol, PriceType, TrendMAPre, 1)
	if maLongPrev == 0 {
		return orders, nil
	}
	maLongSlope := maLong - maLongPrev

	// 获取N周期内的真实波幅 ATR
	atr := m.PriceManager.GetATR(symbol, PriceType, TrendMALong, 1)
	m.updateHighestPrice(currentPrice, atr)

	// ==========================================
	// 2. 动态阈值与过滤条件
	// ==========================================
	// 突破门槛：至少需要突破 ATR 的一定比例，且满足硬性最小波动
	entryThreshold := atr * m.appConfig.TrendAtrFactor

	// ==========================================
	// 3. 趋势方向判断 (多空信号核心)
	// ==========================================
	// 出场用：MA交叉趋势反转信号
	isBuy := maShort > maLong && (maShort-maLong) > entryThreshold
	isSell := maShort < maLong && (maLong-maShort) > entryThreshold

	// 入场用：资金流向比率主信号
	flowPeriod := m.appConfig.TrendFlowPeriod
	flowRatio := m.getFlowRatio(symbol, flowPeriod)
	entryBuy := flowRatio >= m.appConfig.TrendFlowEntryThreshold
	entrySell := flowRatio <= -m.appConfig.TrendFlowEntryThreshold

	volAvg := m.PriceManager.GetAverageVolume(symbol, PriceType, TrendMALong, 1)
	currVol := m.PriceManager.GetAverageVolume(symbol, PriceType, TrendMAPre, 1)
	volConfirmed := currVol > volAvg*m.appConfig.TrendVolumeFactor &&
		currVol < volAvg*m.appConfig.TrendVolumeMaxFactor

	logger.Info("[%s] Price=%.4f, maLong=%.4f, maShort=%.4f, maLongPrev=%.4f, Slope=%.4f, ATR=%.4f, flowRatio=%.4f, entryBuy=%v entrySell=%v",
		m.name, currentPrice, maLong, maShort, maLongPrev, maLongSlope, atr, flowRatio, entryBuy, entrySell)

	// 信号连续性计数（用 flowRatio 主信号）
	if entryBuy {
		m.buyTrendCount++
		m.sellTrendCount = 0
	} else if entrySell {
		m.sellTrendCount++
		m.buyTrendCount = 0
	} else {
		if m.buyTrendCount > 0 {
			m.buyTrendCount--
		}
		if m.sellTrendCount > 0 {
			m.sellTrendCount--
		}
	}

	logger.Info("[%s] Price=%.4f, maLong=%.4f, maShort=%.4f, maLongPrev=%.4f, Slope=%.4f, ATR=%.4f, flowRatio=%.4f, BuyCount=%d, SellCount=%d",
		m.name, currentPrice, maLong, maShort, maLongPrev, maLongSlope, atr, flowRatio, m.buyTrendCount, m.sellTrendCount)

	// ==========================================
	// 4. 止损 / 止盈及反转平仓优先检查
	// ==========================================
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

		flowExitThresh := m.appConfig.TrendFlowExitThreshold
		switch order.Category {
		case types.LongOrderCategory:
			profitPercent := (currentPrice - order.Price) / order.Price
			profit := (currentPrice - order.Price) * float64(unclosedQty)

			if flowExitThresh > 0 && flowRatio < -flowExitThresh {
				reason = fmt.Sprintf("Long Flow Exit sell=%.4f pt:%.2f Profit:%.2f", flowRatio, profitPercent*100, profit)
				shouldClose = true
				action = types.SellOrderAction
			} else if isSell {
				reason = fmt.Sprintf("Long Trend Rev pt:%.2f Profit:%.2f", profitPercent*100, profit)
				shouldClose = true
				action = types.SellOrderAction
			} else if currentPrice-order.Price >= atr*m.appConfig.TrendATRLongWinBase {
				reason = fmt.Sprintf("Long atr max pt:%.2f Profit:%.2f", profitPercent*100, profit)
				shouldClose = true
				action = types.SellOrderAction
			} else if order.StopLongLossPrice > 0 && currentPrice <= order.StopLongLossPrice {
				reason = fmt.Sprintf("Long Stop pt:%.2f Profit:%.2f", profitPercent*100, profit)
				shouldClose = true
				action = types.SellOrderAction
			}

		case types.ShortOrderCategory:
			profitPercent := (order.Price - currentPrice) / order.Price
			profit := (order.Price - currentPrice) * float64(unclosedQty)

			if flowExitThresh > 0 && flowRatio > flowExitThresh {
				reason = fmt.Sprintf("Short Flow Exit buy=%.4f pt:%.2f Profit:%.2f", flowRatio, profitPercent*100, profit)
				shouldClose = true
				action = types.BuyOrderAction
			} else if isBuy {
				reason = fmt.Sprintf("Short Trend Rev pt:%.2f Profit:%.2f", profitPercent*100, profit)
				shouldClose = true
				action = types.BuyOrderAction
			} else if order.Price-currentPrice >= atr*m.appConfig.TrendATRShortWinBase {
				reason = fmt.Sprintf("Short atr max pt:%.2f Profit:%.2f", profitPercent*100, profit)
				shouldClose = true
				action = types.BuyOrderAction
			} else if order.StopShortLossPrice > 0 && currentPrice >= order.StopShortLossPrice {
				reason = fmt.Sprintf("Short Stop pt:%.2f Profit:%.2f", profitPercent*100, profit)
				shouldClose = true
				action = types.BuyOrderAction
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

	// 若存在平仓动作，优先执行平仓
	if len(orders) > 0 {
		return orders, nil
	}

	// ==========================================
	// 5. 开仓风控与条件过滤
	// ==========================================
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
	if !volConfirmed {
		return orders, nil
	}
	if currVol >= volAvg*m.appConfig.TrendVolumeMaxFactor {
		m.ResetBuySellInfo()
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

	// ==========================================
	// 6. 信号触发开仓
	// ==========================================
	// 做多触发：flowRatio 主信号 + 连续性计数达标
	if entryBuy && m.buyTrendCount >= m.appConfig.TrendConfirm {
		m.pendingQty += quantity
		orders = append(orders, types.OrderTarget{
			Symbol:       symbol,
			Category:     types.LongOrderCategory,
			Action:       types.BuyOrderAction,
			StrategyName: m.name,
			TargetPrice:  currentPrice,
			Quantity:     quantity,
			Reason:       fmt.Sprintf("Flow Buy ratio=%.2f cnt=%d", flowRatio, m.buyTrendCount),
			CloseOrderID: "",
		})
		logger.Info("[%s] >> Flow 做多 flowRatio=%.4f buyTrendCount=%d", m.name, flowRatio, m.buyTrendCount)

	} else if entrySell && m.sellTrendCount >= m.appConfig.TrendConfirm {
		m.pendingQty += quantity
		orders = append(orders, types.OrderTarget{
			Symbol:       symbol,
			Category:     types.ShortOrderCategory,
			Action:       types.SellOrderAction,
			StrategyName: m.name,
			TargetPrice:  currentPrice,
			Quantity:     quantity,
			Reason:       fmt.Sprintf("Flow Sell ratio=%.2f cnt=%d", flowRatio, m.sellTrendCount),
			CloseOrderID: "",
		})
		logger.Info("[%s] >> Flow 做空 flowRatio=%.4f sellTrendCount=%d", m.name, flowRatio, m.sellTrendCount)
	}

	logger.Info("[%s] Price=%.4f, maLong=%.4f, maShort=%.4f, maLongPrev=%.4f, Slope=%.4f, ATR=%.4f, flowRatio=%.4f",
		m.name, currentPrice, maLong, maShort, maLongPrev, maLongSlope, atr, flowRatio)

	return orders, nil
}

func (m *TimeTrendStrategy) isInCooldown() bool {
	return utils.Since(m.lastCloseTime) < time.Duration(m.appConfig.OpenCooldown)*time.Second
}

// 重置买卖信息
func (m *TimeTrendStrategy) ResetBuySellInfo() {
	m.buyTrendCount = 0
	m.sellTrendCount = 0
}

// 卖出所有
func (m *TimeTrendStrategy) sellAll(symbol string, currentPrice float64, reason string) []types.OrderTarget {
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

func (m *TimeTrendStrategy) checkSellOrders(order *StrategyOrder) bool {
	// 1. 检查是否有剩余持仓
	if order.BoughtQuantity-order.CloseQuantity <= 0 {
		return false
	}

	// NoneStatus 或 FailedStatus 允许卖出
	return order.CloseStatus == types.NoneStatus || order.CloseStatus == types.FailedStatus
}

// 更新最高价
func (m *TimeTrendStrategy) updateHighestPrice(currentPrice float64, atr float64) {
	stopBase := m.appConfig.TrendATRLongStopBase
	shortStopBase := m.appConfig.TrendATRShortStopBase
	maxLoss := m.appConfig.TrendStopMaxLoss

	for _, order := range m.orders {
		if order.BoughtQuantity <= order.CloseQuantity {
			continue
		}
		switch order.Category {
		case types.LongOrderCategory:
			if order.HighestPrice == 0 || currentPrice >= order.HighestPrice {
				order.HighestPrice = currentPrice
			}
			atrStop := order.Price - (atr * stopBase)
			pctStop := order.Price * (1 - maxLoss)
			trailStop := order.HighestPrice - (atr * stopBase)
			newStopLoss := math.Max(atrStop, math.Max(trailStop, pctStop))
			if order.StopLongLossPrice == 0 || newStopLoss > order.StopLongLossPrice {
				order.StopLongLossPrice = newStopLoss
				logger.Warn("atr=%.4f, Price=%.4f, currentPrice=%.4f, High=%.4f, StopLongLossPrice=%.4f", atr, order.Price, currentPrice, order.HighestPrice, newStopLoss)
			}
		case types.ShortOrderCategory:
			if order.LowestPrice == 0 || currentPrice <= order.LowestPrice {
				order.LowestPrice = currentPrice
			}
			atrStop := order.Price + (atr * shortStopBase)
			pctStop := order.Price * (1 + maxLoss)
			trailStop := order.LowestPrice + (atr * shortStopBase)
			newStopLoss := math.Min(atrStop, math.Min(trailStop, pctStop))
			if order.StopShortLossPrice == 0 || newStopLoss < order.StopShortLossPrice {
				order.StopShortLossPrice = newStopLoss
				logger.Warn("atr=%.4f, Price=%.4f, currentPrice=%.4f, Low=%.4f, StopShortLossPrice=%.4f", atr, order.Price, currentPrice, order.LowestPrice, newStopLoss)
			}
		}
	}
}

// 获取总持仓数量
func (m *TimeTrendStrategy) getTotalQuantity() int64 {
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
func (m *TimeTrendStrategy) getUnsoldCount() int {
	count := 0
	for _, order := range m.orders {
		if order.BoughtQuantity-order.CloseQuantity > 0 {
			count++
		}
	}
	return count
}

// getFlowRatio 计算最近 N 秒的资金流向比率。
// 阳线(Close>Open)该秒成交量计入买方，阴线(Close<Open)计入卖方。
// 返回值 [-1, 1]：正值表示买方主导，负值表示卖方主导。
func (m *TimeTrendStrategy) getFlowRatio(symbol string, period int) float64 {
	if period <= 0 {
		return 0
	}
	data := m.PriceManager.GetSecondData(symbol, period)
	if len(data) == 0 {
		return 0
	}
	var buyVol, sellVol float64
	for _, d := range data {
		vol := float64(d.CloseVolume - d.Volume)
		if vol <= 0 {
			continue
		}
		if d.Close > d.Open {
			buyVol += vol
		} else if d.Close < d.Open {
			sellVol += vol
		} else {
			// 收盘=开盘，成交量对半分配
			buyVol += vol * 0.5
			sellVol += vol * 0.5
		}
	}
	total := buyVol + sellVol
	if total == 0 {
		return 0
	}
	return (buyVol - sellVol) / total
}

// 获取未完全卖出的订单（按买入价格从低到高排序）
func (m *TimeTrendStrategy) getUnsoldOrdersByPriceAsc() []*StrategyOrder {
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
func (m *TimeTrendStrategy) OnBuyOrderSubmitted(orderID string, order *types.OrderTarget) {
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
func (m *TimeTrendStrategy) OnBuyOrderFilled(orderID string, order *types.OrderTarget) {
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
		m.ResetBuySellInfo()
		logger.Info("[%s] 平仓买入成交: 买入订单ID=%s, 买入价=%.4f, 卖出价=%.4f, 数量=%d, 盈亏:%.2f, 买入价:%.4f",
			m.name, orderID, matchedOrder.Price, price, quantity, profit, price)
	}
}

// 卖出订单已提交
func (m *TimeTrendStrategy) OnSellOrderSubmitted(orderID string, order *types.OrderTarget) {
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
func (m *TimeTrendStrategy) OnSellOrderFilled(orderID string, order *types.OrderTarget) {
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
		m.ResetBuySellInfo()
		logger.Info("[%s] 做空卖出成交: 订单ID:%s, 股票:%s 总待买入数量：%d",
			m.name, orderID, order.Symbol, m.pendingQty)
	}
}

// 订单失败回调
func (m *TimeTrendStrategy) OnOrderFailed(orderID string, order *types.OrderTarget) {
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
func (m *TimeTrendStrategy) GetCurrentState() map[string]interface{} {
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
			"highestprice":   order.HighestPrice,
			"lowestprice":    order.LowestPrice,
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
func (m *TimeTrendStrategy) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.orders = make([]*StrategyOrder, 0)
	m.pendingQty = 0
}

// cmd卖出 - 返回多个订单
func (m *TimeTrendStrategy) CmdSell(symbol string, cmdSellInfo string) []types.OrderTarget {
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
func (m *TimeTrendStrategy) CmdBuy(symbol string, cmdBuyInfo string) []types.OrderTarget {
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
