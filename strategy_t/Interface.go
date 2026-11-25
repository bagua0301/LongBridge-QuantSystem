package strategy_t

// StrategyTest 测试策略接口
type StrategyTest interface {
	GetName() string
	GetDescription() string
	Init()
	Run()
	StopPricePushing()
	GetCurrentState() map[string]interface{}
}
