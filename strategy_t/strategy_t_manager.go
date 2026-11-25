package strategy_t

import (
	"container/heap"
	"fmt"
	"sync"
	"time"

	"LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"

	"github.com/longportapp/openapi-go/quote"
)

// VirtualEvent 虚拟事件
type VirtualEvent struct {
	TriggerTime time.Time // 触发时间（虚拟时间）
	Action      func()    // 要执行的函数
	index       int       // 堆索引（内部使用）
}

// eventHeap 实现 heap.Interface
type eventHeap []*VirtualEvent

func (h eventHeap) Len() int           { return len(h) }
func (h eventHeap) Less(i, j int) bool { return h[i].TriggerTime.Before(h[j].TriggerTime) }
func (h eventHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }

func (h *eventHeap) Push(x interface{}) {
	n := len(*h)
	ev := x.(*VirtualEvent)
	ev.index = n
	*h = append(*h, ev)
}

func (h *eventHeap) Pop() interface{} {
	old := *h
	n := len(old)
	ev := old[n-1]
	ev.index = -1
	*h = old[0 : n-1]
	return ev
}

// StrategyTestManager 测试管理器
type StrategyTestManager struct {
	strategies      []StrategyTest
	strategiesMap   map[string]StrategyTest
	currentStrategy StrategyTest
	mutex           sync.RWMutex
	appConfig       *config.AppConfig
	exitChan        chan struct{} // 退出信号通道

	// 使用接口类型，而不是具体类型
	quantSystem types.QuantSystemInterface

	// 订单通道
	orderChan chan *types.TestOrderEvent

	// 订单存储
	orders     map[string]*types.TestOrder
	ordersLock sync.RWMutex

	// 控制
	stopChan chan struct{}
	running  bool
	mu       sync.RWMutex

	// 订单ID计数器
	orderIDBuyCounter  int
	orderIDSellCounter int

	// 上次订单订单策略时间
	lastOrderLogicTimestamp int64

	// 虚拟事件调度
	eventQueue eventHeap  // 优先队列
	eventMu    sync.Mutex // 保护事件队列的锁
}

var (
	// GlobalTestManager 全局测试管理器（导出，供其他包使用）
	GlobalTestManager *StrategyTestManager
	once              sync.Once
)

// GetTestStrategyManager 获取测试策略管理器
func GetTestStrategyManager(appConfig *config.AppConfig) *StrategyTestManager {
	once.Do(func() {
		GlobalTestManager = &StrategyTestManager{
			strategies:              make([]StrategyTest, 0),
			strategiesMap:           make(map[string]StrategyTest),
			appConfig:               appConfig,
			exitChan:                make(chan struct{}),
			orderChan:               make(chan *types.TestOrderEvent, 1000),
			orders:                  make(map[string]*types.TestOrder),
			stopChan:                make(chan struct{}),
			orderIDBuyCounter:       0,
			orderIDSellCounter:      0,
			lastOrderLogicTimestamp: 0,
			eventQueue:              make(eventHeap, 0),
		}
		// 创建测试策略并注册
		GlobalTestManager.RegisterStrategy(NewTestPriceStrategy())
	})
	return GlobalTestManager
}

// 设置量化系统
func (m *StrategyTestManager) SetQuantSystem(qs types.QuantSystemInterface) {
	m.quantSystem = qs
}

// GetQuantSystem 获取量化系统
func (m *StrategyTestManager) GetQuantSystem() types.QuantSystemInterface {
	return m.quantSystem
}

// GetExitChan 获取退出信号通道
func (m *StrategyTestManager) GetExitChan() <-chan struct{} {
	return m.exitChan
}

// GetGlobalTestManager 获取全局测试管理器
func GetGlobalTestManager() *StrategyTestManager {
	return GlobalTestManager
}

// RegisterStrategy 注册策略
func (m *StrategyTestManager) RegisterStrategy(s StrategyTest) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.strategiesMap[s.GetName()]; exists {
		return
	}

	m.strategies = append(m.strategies, s)
	m.strategiesMap[s.GetName()] = s
}

// GetStrategy 根据名称获取策略
func (m *StrategyTestManager) GetStrategy(name string) (StrategyTest, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	s, ok := m.strategiesMap[name]
	return s, ok
}

// GetStrategyByIndex 根据索引获取策略
func (m *StrategyTestManager) GetStrategyByIndex(index int) (StrategyTest, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if index >= 0 && index < len(m.strategies) {
		return m.strategies[index], true
	}
	return nil, false
}

// GetAllStrategies 获取所有策略
func (m *StrategyTestManager) GetAllStrategies() []StrategyTest {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make([]StrategyTest, len(m.strategies))
	copy(result, m.strategies)
	return result
}

// SetCurrentStrategy 设置当前策略
func (m *StrategyTestManager) SetCurrentStrategy(nameOrIndex string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 尝试解析为数字索引
	var index int
	if _, err := fmt.Sscanf(nameOrIndex, "%d", &index); err == nil && index >= 1 && index <= len(m.strategies) {
		m.currentStrategy = m.strategies[index-1]
		return nil
	}

	// 按名称查找
	s, ok := m.strategiesMap[nameOrIndex]
	if !ok {
		return fmt.Errorf("策略不存在: %s", nameOrIndex)
	}

	m.currentStrategy = s
	return nil
}

// GetCurrentStrategy 获取当前策略
func (m *StrategyTestManager) GetCurrentStrategy() StrategyTest {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.currentStrategy
}

// GetCurrentStrategyInfo 获取当前策略信息
func (m *StrategyTestManager) GetCurrentStrategyInfo() string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if m.currentStrategy == nil {
		return "未选择策略"
	}
	return fmt.Sprintf("%s - %s", m.currentStrategy.GetName(), m.currentStrategy.GetDescription())
}

// GetStrategyCount 获取策略数量
func (m *StrategyTestManager) GetStrategyCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.strategies)
}

// Start 启动测试管理器
func (m *StrategyTestManager) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	if m.currentStrategy == nil {
		return
	}
	m.currentStrategy.Init()
	m.currentStrategy.Run()

	logger.Info("========================================")
	logger.Info("测试管理器已启动")
	logger.Info("========================================")

	go m.processOrderEvents()
}

// Stop 停止测试管理器
func (m *StrategyTestManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	m.running = false

	// 处理所有剩余事件（假设使用一个很大的未来时间）
	farFuture := time.Unix(1<<62, 0)
	m.processDueEvents(farFuture)

	m.orderChan = make(chan *types.TestOrderEvent, 1000)
	close(m.stopChan)
	close(m.exitChan)
	logger.Info("测试管理器已停止")
}

// processOrderEvents 处理订单事件
func (m *StrategyTestManager) processOrderEvents() {
	for {
		select {
		case <-m.stopChan:
			return
		case event := <-m.orderChan:
			m.handleOrderEvent(event)
		}
	}
}

// GetOrder 获取订单信息
func (m *StrategyTestManager) GetOrder(orderID string) *types.TestOrder {
	m.ordersLock.RLock()
	defer m.ordersLock.RUnlock()
	return m.orders[orderID]
}

// scheduleEvent 在指定的虚拟时间后执行 action
func (m *StrategyTestManager) scheduleEvent(delay time.Duration, action func()) {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()
	triggerTime := utils.Now().Add(delay)
	ev := &VirtualEvent{
		TriggerTime: triggerTime,
		Action:      action,
	}
	heap.Push(&m.eventQueue, ev)
}

// processDueEvents 处理所有触发时间 <= upToTime 的事件
// 返回处理的事件数量
func (m *StrategyTestManager) processDueEvents(upToTime time.Time) int {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()
	processed := 0
	for m.eventQueue.Len() > 0 {
		// 查看队首事件
		top := m.eventQueue[0]
		if top.TriggerTime.After(upToTime) {
			break
		}
		// 弹出并执行
		ev := heap.Pop(&m.eventQueue).(*VirtualEvent)
		// 执行前将虚拟时钟同步到该事件的触发时间（保证 Now() 准确）
		utils.VirtualClockSet(ev.TriggerTime)
		ev.Action()
		processed++
	}
	return processed
}

// handleOrderEvent 处理订单事件
func (m *StrategyTestManager) handleOrderEvent(event *types.TestOrderEvent) {
	logger.Info("[测试] 订单事件: %s, 订单ID=%s, 状态=%s", event.Order.Action, event.OrderID, event.Status)

	// 使用接口方法访问
	if m.quantSystem != nil {
		orderQuery := m.quantSystem.GetOrderQuery()
		if orderQuery != nil {
			orderQuery.HandleTestOrderEvent(event)
		} else {
			logger.Warn("[测试] 价格更新器未初始化，无法更新价格: 订单事件: %s, 订单ID=%s, 状态=%s", event.Order.Action, event.OrderID, event.Status)
		}
	} else {
		logger.Warn("[测试] 量化系统未初始化，无法更新价格: 订单事件: %s, 订单ID=%s, 状态=%s", event.Order.Action, event.OrderID, event.Status)
	}
}

// handlePriceEvent 处理价格事件（生成交易信号）
func (m *StrategyTestManager) handlePriceEvent(event []*quote.PushQuote) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}

	// 使用接口方法访问
	if m.quantSystem == nil {
		logger.Fatal("m.quantSystem == nil")
		return
	}
	PriceManager := m.quantSystem.GetPriceManager()
	if PriceManager == nil {
		logger.Fatal("PriceManager == nil")
		return
	}
	for _, v := range event {
		// 将事件时间戳转为 time.Time
		eventTime := utils.Float64ToTime(v.Timestamp)
		// 1. 先处理所有到期的虚拟事件（订单成交）
		m.processDueEvents(eventTime)
		// 2. 将虚拟时钟同步到当前价格事件时间（保证 Now() 等于事件时间）
		utils.VirtualClockSet(eventTime)

		PriceManager.OnQuote(v)

		if m.lastOrderLogicTimestamp <= v.Timestamp {
			m.lastOrderLogicTimestamp = v.Timestamp
			// 订单逻辑
			m.quantSystem.OrderLogic()
		}
	}
}

// SubmitBuyOrder 提交买入订单
func (m *StrategyTestManager) SubmitBuyOrder(order *types.OrderTarget) string {
	m.orderIDBuyCounter++
	orderID := fmt.Sprintf("TEST_BUY_%d", m.orderIDBuyCounter)

	testOrder := &types.TestOrder{
		Order:      *order,
		OrderID:    orderID,
		Status:     "SUBMITTED",
		SubmitTime: utils.Now(),
	}

	m.ordersLock.Lock()
	m.orders[orderID] = testOrder
	m.ordersLock.Unlock()

	logger.Info("[测试] 买入订单已提交: %s, 价格=%.4f, 数量=%d", orderID, order.TargetPrice, order.Quantity)

	// 调度订单成交事件（3秒虚拟时间后）
	m.scheduleEvent(3*time.Second, func() {
		m.BuyOrderFill(testOrder)
	})

	return orderID
}

// SubmitSellOrder 提交卖出订单
func (m *StrategyTestManager) SubmitSellOrder(order *types.OrderTarget) string {
	m.orderIDSellCounter++
	orderID := fmt.Sprintf("TEST_SELL_%d", m.orderIDSellCounter)

	testOrder := &types.TestOrder{
		OrderID:    orderID,
		Order:      *order,
		Status:     "SUBMITTED",
		SubmitTime: utils.Now(),
	}

	m.ordersLock.Lock()
	m.orders[orderID] = testOrder
	m.ordersLock.Unlock()

	logger.Info("[测试] 卖出订单已提交: %s, 价格=%.4f, 数量=%d, 绑定买入订单=%s",
		orderID, order.TargetPrice, order.Quantity, order.CloseOrderID)

	// 调度订单成交事件
	m.scheduleEvent(3*time.Second, func() {
		m.SellOrderFill(orderID)
	})

	return orderID
}

// BuyOrderFill 模拟买入订单成交
func (m *StrategyTestManager) BuyOrderFill(testOrder *types.TestOrder) {
	m.ordersLock.Lock()
	defer m.ordersLock.Unlock()

	order, exists := m.orders[testOrder.OrderID]
	if !exists {
		return
	}

	var profit float64 = 0
	if testOrder.Order.Category == types.ShortOrderCategory {
		// 获取买入订单的成本
		var buyPrice float64 = 0
		if buyOrder, ok := m.orders[testOrder.Order.CloseOrderID]; ok {
			buyPrice = buyOrder.Order.TargetPrice
		}
		profit = float64(testOrder.Order.Quantity) * (buyPrice - testOrder.Order.TargetPrice)
	}

	order.Status = "FilledStatus"
	order.FillTime = utils.Now()
	order.Profit = profit

	logger.Info("[测试] 买入订单成交: %s, 价格=%.4f, 数量=%d",
		testOrder.OrderID, testOrder.Order.TargetPrice, testOrder.Order.Quantity)

	// 发送订单事件
	m.orderChan <- &types.TestOrderEvent{
		OrderID:   testOrder.OrderID,
		Order:     testOrder.Order,
		Status:    "FilledStatus",
		Timestamp: utils.Now(),
		Profit:    profit,
	}
}

// SellOrderFill 模拟卖出订单成交
func (m *StrategyTestManager) SellOrderFill(orderID string) {
	m.ordersLock.Lock()
	defer m.ordersLock.Unlock()

	testOrder, exists := m.orders[orderID]
	if !exists {
		return
	}

	var profit float64 = 0
	if testOrder.Order.Category == types.LongOrderCategory {
		// 获取买入订单的成本
		var buyPrice float64 = 0
		if buyOrder, ok := m.orders[testOrder.Order.CloseOrderID]; ok {
			buyPrice = buyOrder.Order.TargetPrice
		}
		profit = float64(testOrder.Order.Quantity) * (testOrder.Order.TargetPrice - buyPrice)
	}

	testOrder.Status = "FilledStatus"
	testOrder.FillTime = utils.Now()
	testOrder.Profit = profit

	logger.Info("[测试] 卖出订单成交: %s, 价格=%.4f, 数量=%d, 盈亏=%.2f",
		orderID, testOrder.Order.TargetPrice, testOrder.Order.Quantity, profit)

	// 发送订单事件
	m.orderChan <- &types.TestOrderEvent{
		OrderID:   orderID,
		Order:     testOrder.Order,
		Status:    "FilledStatus",
		Timestamp: utils.Now(),
		Profit:    profit,
	}
}

// CancelOrder 取消订单
func (m *StrategyTestManager) CancelOrder(orderID string) bool {
	m.ordersLock.Lock()
	defer m.ordersLock.Unlock()

	testOrder, exists := m.orders[orderID]
	if !exists {
		return false
	}

	if testOrder.Status != "SUBMITTED" {
		return false
	}

	testOrder.Status = "CancelledStatus"
	logger.Info("[测试] 订单已取消: %s", orderID)

	m.orderChan <- &types.TestOrderEvent{
		OrderID:   orderID,
		Order:     testOrder.Order,
		Status:    "CancelledStatus",
		Timestamp: utils.Now(),
	}

	return true
}
