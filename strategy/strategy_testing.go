package strategy

import (
	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"
	"fmt"
	"sync"
	"time"
)

// TestStrategy 测试策略：定时买卖循环
type TestStrategy struct {
	shortName    string
	name         string
	description  string
	appConfig    *myconfig.AppConfig // 应用配置
	PriceManager types.PriceManagerInterface

	// 配置参数
	holdDuration time.Duration // 持仓时间，默认 30 秒
	buyQuantity  int64         // 每次买入数量（股），由配置文件决定

	// 状态管理
	mu             sync.RWMutex
	isHolding      bool      // 是否持有仓位
	holdStartTime  time.Time // 持仓开始时间（买入成交时记录）
	lastBuyPrice   float64   // 上次买入价格
	lastBuyTime    time.Time // 上次买入时间
	cycleCount     int       // 循环次数
	currentOrderID string    // 当前订单ID

	// 待确认订单
	pendingBuyOrder bool      // 是否有待确认的买入订单
	pendingBuyTime  time.Time // 待确认买入订单时间
	pendingQty      bool      // 是否有待确认的卖出订单
	pendingSellTime time.Time // 待确认卖出订单时间
}

// NewTestStrategy 创建测试策略
func NewTestStrategy() *TestStrategy {
	return &TestStrategy{
		shortName:   "TestOrder",
		name:        "TestOrder",
		description: "测试策略：定时买卖循环（数量由配置文件控制）",

		holdDuration:    30 * time.Second,
		buyQuantity:     10,
		isHolding:       false,
		cycleCount:      0,
		pendingBuyOrder: false,
		pendingQty:      false,
	}
}

// GetName 获取策略短名称
func (m *TestStrategy) GetShortName() string {
	return m.shortName
}

// GetName 获取策略名称
func (m *TestStrategy) GetName() string {
	return m.name
}

// GetDescription 获取策略描述
func (m *TestStrategy) GetDescription() string {
	return m.description
}

// GetName 获取策略类型
func (m *TestStrategy) GetStrategyType() string {
	return ""
}

// GetRequiredData 获取需要的数据类型
func (m *TestStrategy) GetRequiredData() []string {
	return []string{"quote"}
}

// GetStrategyConfig 获取策略详细信息
func (m *TestStrategy) GetStrategyConfig() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"shortName":   m.shortName,
		"name":        m.name,
		"description": m.description,
	}
}

// Init 初始化策略
func (m *TestStrategy) Init(appConfig *myconfig.AppConfig) error {
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

	if appConfig.TestBuyQuantity != 0 {
		m.buyQuantity = appConfig.TestBuyQuantity
	}
	if appConfig.TestHoldSeconds != 0 {
		m.holdDuration = time.Duration(appConfig.TestHoldSeconds) * time.Second
	}
	return nil
}

// GetSignal 获取交易信号 - 返回多个订单
func (m *TestStrategy) GetSignal(symbol string) ([]types.OrderTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	currentPrice := m.PriceManager.GetCurrentPrice(symbol)

	var orders []types.OrderTarget

	if m.pendingQty {
		return orders, fmt.Errorf("有卖出订单待成交")
	}
	if m.pendingBuyOrder {
		return orders, fmt.Errorf("有买入订单待成交")
	}

	if !m.isHolding && !m.pendingBuyOrder && !m.pendingQty {
		reason := fmt.Sprintf("测试策略第%d轮: 准备买入 %d 股，价格 %.4f",
			m.cycleCount+1, m.buyQuantity, currentPrice)
		logger.Info("[TestStrategy] %s", reason)

		orders = append(orders, types.OrderTarget{
			Symbol:       symbol,
			Category:     types.LongOrderCategory,
			Action:       types.BuyOrderAction,
			StrategyName: m.name,
			TargetPrice:  currentPrice,
			Quantity:     m.buyQuantity,
			Reason:       reason,
			CloseOrderID: "",
		})
		return orders, nil
	}

	if m.isHolding && !m.pendingQty {
		holdTime := utils.Since(m.holdStartTime)
		if holdTime >= m.holdDuration {
			reason := fmt.Sprintf("测试策略第%d轮: 准备卖出 %d 股，价格 %.4f，持仓 %.0f 秒",
				m.cycleCount, m.buyQuantity, currentPrice, holdTime.Seconds())
			logger.Info("[TestStrategy] %s", reason)

			orders = append(orders, types.OrderTarget{
				Symbol:       symbol,
				Category:     types.LongOrderCategory,
				Action:       types.SellOrderAction,
				StrategyName: m.name,
				TargetPrice:  currentPrice,
				Quantity:     m.buyQuantity,
				Reason:       reason,
				CloseOrderID: "",
			})
			return orders, nil
		}
	}

	return orders, nil
}

// OnBuyOrderSubmitted 买入订单已提交
func (m *TestStrategy) OnBuyOrderSubmitted(orderID string, order *types.OrderTarget) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingBuyOrder = true
	m.pendingBuyTime = utils.Now()
	logger.Info("[TestStrategy] 订单ID=%s, 股票:%s 买入订单已提交，等待成交", orderID, order.Symbol)
}

// OnBuyOrderFilled 买入订单成交
func (m *TestStrategy) OnBuyOrderFilled(orderID string, order *types.OrderTarget) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isHolding = true
	m.holdStartTime = utils.Now()
	m.lastBuyPrice = order.TargetPrice
	m.lastBuyTime = utils.Now()
	m.cycleCount++
	m.pendingBuyOrder = false
	m.currentOrderID = orderID
	logger.Info("[TestStrategy] 买入成交! 订单ID=%s, 股票:%s, 第%d轮, 数量: %d股, 价格: %.4f, 将持有 %.0f 秒后卖出",
		order.OrderID, order.Symbol, m.cycleCount, order.Quantity, order.TargetPrice, m.holdDuration.Seconds())
}

// OnSellOrderSubmitted 卖出订单已提交
func (m *TestStrategy) OnSellOrderSubmitted(orderID string, order *types.OrderTarget) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingQty = true
	m.pendingSellTime = utils.Now()
	logger.Info("[TestStrategy] 卖出订单已提交，等待成交, 订单ID=%s, 股票:%s, 绑定买入订单ID=%s", order.OrderID, order.Symbol, order.CloseOrderID)
}

// OnSellOrderFilled 卖出订单成交
func (m *TestStrategy) OnSellOrderFilled(orderID string, order *types.OrderTarget) {
	m.mu.Lock()
	defer m.mu.Unlock()

	quantity := order.Quantity
	price := order.TargetPrice
	profit := (price - m.lastBuyPrice) * float64(quantity)
	logger.Info("[TestStrategy] 卖出成交! 订单ID=%s, 股票:%s, 第%d轮, 数量: %d股, 价格: %.4f, 盈亏: %.2f",
		order.OrderID, order.Symbol, m.cycleCount, quantity, price, profit)
	m.isHolding = false
	m.holdStartTime = time.Time{}
	m.lastBuyPrice = 0
	m.lastBuyTime = time.Time{}
	m.pendingQty = false
	m.currentOrderID = ""
}

// OnOrderFailed 订单失败回调
func (m *TestStrategy) OnOrderFailed(orderID string, order *types.OrderTarget) {
	m.mu.Lock()
	defer m.mu.Unlock()
	logger.Info("[TestStrategy] 订单失败, 订单ID=%s, 股票:%s, 重置状态", orderID, order.Symbol)
	m.pendingBuyOrder = false
	m.pendingQty = false
	m.pendingBuyTime = time.Time{}
	m.pendingSellTime = time.Time{}
}

// Reset 重置策略状态
func (m *TestStrategy) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isHolding = false
	m.holdStartTime = time.Time{}
	m.lastBuyPrice = 0
	m.lastBuyTime = time.Time{}
	m.cycleCount = 0
	m.pendingBuyOrder = false
	m.pendingQty = false
	m.pendingBuyTime = time.Time{}
	m.pendingSellTime = time.Time{}
	m.currentOrderID = ""
}

// CmdSell cmd卖出 - 返回多个订单
func (m *TestStrategy) CmdSell(symbol string, cmdSellInfo string) []types.OrderTarget {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.buyQuantity <= 0 {
		logger.Warn("[TestStrategy] 没有可卖出的股票")
		return nil
	}

	// 获取当前价格
	currentPrice := m.PriceManager.GetCurrentPrice(symbol)
	var orders []types.OrderTarget
	reason := fmt.Sprintf("指令主动卖出: %s", cmdSellInfo)
	orders = append(orders, types.OrderTarget{
		Symbol:       symbol,
		Category:     types.LongOrderCategory,
		Action:       types.SellOrderAction,
		StrategyName: m.name,
		TargetPrice:  currentPrice,
		Quantity:     m.buyQuantity,
		Reason:       reason,
		CloseOrderID: "",
	})
	return orders
}

// CmdBuy cmd买入 - 返回多个订单
func (m *TestStrategy) CmdBuy(symbol string, cmdBuyInfo string) []types.OrderTarget {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 获取当前价格
	currentPrice := m.PriceManager.GetCurrentPrice(symbol)
	buyQuantity, err := utils.StringToInt64(cmdBuyInfo)
	if err != nil {
		logger.Warn("转换失败:%v, cmdBuyInfo=%s", err, cmdBuyInfo)
		return nil
	}
	m.buyQuantity += buyQuantity
	logger.Info("[TestStrategy] 指令主动买入: %d 股, 当前总买入数量: %d 股", buyQuantity, m.buyQuantity)
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

// GetCurrentState 获取当前状态
func (m *TestStrategy) GetCurrentState() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := map[string]interface{}{
		"is_holding":        m.isHolding,
		"cycle_count":       m.cycleCount,
		"hold_duration":     m.holdDuration.Seconds(),
		"buy_quantity":      m.buyQuantity,
		"last_buy_price":    m.lastBuyPrice,
		"current_order_id":  m.currentOrderID,
		"pending_buy_order": m.pendingBuyOrder,
		"pending_qty":       m.pendingQty,
	}
	if m.isHolding {
		state["hold_elapsed"] = utils.Since(m.holdStartTime).Seconds()
		state["hold_remaining"] = (m.holdDuration - utils.Since(m.holdStartTime)).Seconds()
	}
	if m.pendingBuyOrder {
		state["pending_buy_wait"] = utils.Since(m.pendingBuyTime).Seconds()
	}
	if m.pendingQty {
		state["pending_sell_wait"] = utils.Since(m.pendingSellTime).Seconds()
	}
	return state
}
