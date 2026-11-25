package quant

import (
	"fmt"
	"sort"
	"strings"

	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/strategy"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"
)

// calculateSummary 计算交易汇总（只统计当前策略的订单）
func (qs *QuantSystem) calculateSummary() *TradeSummary {
	qs.OrdersLock.RLock()
	defer qs.OrdersLock.RUnlock()

	summary := &TradeSummary{
		SymbolStats: make(map[string]*SymbolStats),
	}

	for _, order := range qs.CompletedOrders {
		if _, exists := summary.SymbolStats[order.Symbol]; !exists {
			summary.SymbolStats[order.Symbol] = &SymbolStats{Symbol: order.Symbol}
		}
		stats := summary.SymbolStats[order.Symbol]

		// 做空
		short := utils.IsShortOrderCategory(int32(order.Category))
		if order.Action == types.BuyOrderAction {
			summary.TotalBuyAmount += order.Amount
			stats.BuyAmount += order.Amount
			if short {
				summary.TotalProfitLoss = order.ProfitLoss
				if order.ProfitLoss > 0 {
					summary.WinCount++
				} else if order.ProfitLoss < 0 {
					summary.LossCount++
				}

				stats.ProfitLoss += order.ProfitLoss
				if order.ProfitLoss > 0 {
					stats.WinCount++
				} else if order.ProfitLoss < 0 {
					stats.LossCount++
				}
			}
		} else if order.Action == types.SellOrderAction {
			summary.TotalSellAmount += order.Amount
			stats.SellAmount += order.Amount
			if !short {
				summary.TotalProfitLoss += order.ProfitLoss
				if order.ProfitLoss > 0 {
					summary.WinCount++
				} else if order.ProfitLoss < 0 {
					summary.LossCount++
				}

				stats.ProfitLoss += order.ProfitLoss
				if order.ProfitLoss > 0 {
					stats.WinCount++
				} else if order.ProfitLoss < 0 {
					stats.LossCount++
				}
			}
		}
	}

	totalTrades := summary.WinCount + summary.LossCount
	if totalTrades > 0 {
		summary.WinRate = float64(summary.WinCount) / float64(totalTrades) * 100
	}

	return summary
}

// printStrategyOrders 从策略内部打印订单明细
func (qs *QuantSystem) printStrategyOrders(currentStrategy strategy.Strategy) {
	if currentStrategy == nil {
		logger.Warn("无当前策略")
		return
	}

	state := currentStrategy.GetCurrentState()

	orders, ok := state["orders"].([]map[string]interface{})
	if !ok || len(orders) == 0 {
		logger.Warn("策略无订单记录")
		return
	}

	logger.Warn("")
	logger.Warn("【策略订单明细】")

	// 准备表头
	headers := []string{"OrderId", "Symbol", "Action", "Price", "ClosePrice", "Num", "remaining", "Profit", "CloseOrderId"}
	tp := utils.NewTablePrinter(headers)
	tp.SetUseSimpleChars(false)

	for _, order := range orders {
		category := utils.GetInt32(order, "category")
		if category <= 0 {
			logger.Error("category err %d", category)
			continue
		}

		short := utils.IsShortOrderCategory(category)
		orderID := utils.GetString(order, "order_id")
		symbol := utils.GetString(order, "symbol")
		action := utils.GetString(order, "action")
		price := utils.GetFloat64(order, "price")
		close_price := utils.GetFloat64(order, "close_price")
		quantity := utils.GetInt64(order, "quantity")
		remaining := utils.GetInt64(order, "remaining")
		profit := utils.GetFloat64(order, "profit")

		var bindInfo string

		close_order_id := utils.GetString(order, "close_order_id")
		if close_order_id != "" {
			bindInfo = fmt.Sprintf("%s", close_order_id)
		} else {
			currentPrice := qs.PriceManager.GetCurrentPrice(symbol)
			if short {
				// 获取当前价格计算浮动盈亏
				if remaining != 0 {
					profit += (price - currentPrice) * float64(remaining)
				}
			} else {
				// 获取当前价格计算浮动盈亏
				if remaining != 0 {
					profit += (currentPrice - price) * float64(remaining)
				}
			}
		}

		// 格式化盈亏显示
		var profitStr string
		if profit > 0 {
			profitStr = fmt.Sprintf("+%.2f", profit)
		} else if profit < 0 {
			profitStr = fmt.Sprintf("%.2f", profit)
		} else {
			profitStr = ""
		}

		tp.AddRow([]string{
			orderID,
			symbol,
			action,
			fmt.Sprintf("%.2f", price),
			fmt.Sprintf("%.2f", close_price),
			fmt.Sprintf("%d", quantity),
			fmt.Sprintf("%d", remaining),
			profitStr,
			bindInfo,
		})
	}

	// 输出表格
	tableStr := tp.Build()
	for _, line := range strings.Split(tableStr, "\n") {
		if line != "" {
			logger.Warn(line)
		}
	}
}

// printStrategyPositions 打印当前策略的持仓
func (qs *QuantSystem) printStrategyPositions(currentStrategy strategy.Strategy) {
	if currentStrategy == nil {
		logger.Warn("无当前策略")
		return
	}

	state := currentStrategy.GetCurrentState()

	orders, ok := state["orders"].([]map[string]interface{})
	if !ok {
		logger.Warn("")
		logger.Warn("【当前策略持仓】")
		logger.Warn("  无持仓")
		return
	}

	// 筛选未卖出的订单
	unsoldOrders := make([]map[string]interface{}, 0)
	var totalUnsoldProfit float64 = 0
	var totalSoldProfit float64 = 0
	var totalUnsoldQuantity int64 = 0
	var totalUnsoldMarketValue float64 = 0
	var totalUnsoldCost float64 = 0
	var unsoldWinCount int = 0
	var unsoldLossCount int = 0

	for _, order := range orders {
		category := utils.GetInt32(order, "category")
		if category <= 0 {
			logger.Error("category err %d", category)
			continue
		}
		short := utils.IsShortOrderCategory(category)

		if short {
			remaining := utils.GetInt64(order, "remaining")
			profit := utils.GetFloat64(order, "profit")

			symbol := utils.GetString(order, "symbol")
			price := utils.GetFloat64(order, "price")

			currentPrice := qs.PriceManager.GetCurrentPrice(symbol)

			marketValue := float64(remaining) * price
			totalCost := float64(remaining) * currentPrice
			floatingProfit := profit + marketValue - totalCost

			if remaining <= 0 {
				totalSoldProfit += profit
			} else {
				unsoldOrders = append(unsoldOrders, order)
				totalUnsoldProfit += floatingProfit
				totalUnsoldQuantity += remaining
				totalUnsoldMarketValue += marketValue
				totalUnsoldCost += totalCost
				if floatingProfit > 0 {
					unsoldWinCount++
				} else if floatingProfit < 0 {
					unsoldLossCount++
				}
			}
		} else {
			remaining := utils.GetInt64(order, "remaining")
			profit := utils.GetFloat64(order, "profit")

			symbol := utils.GetString(order, "symbol")
			price := utils.GetFloat64(order, "price")

			currentPrice := qs.PriceManager.GetCurrentPrice(symbol)

			marketValue := float64(remaining) * currentPrice
			totalCost := float64(remaining) * price
			floatingProfit := profit + marketValue - totalCost

			if remaining <= 0 {
				totalSoldProfit += profit
			} else {
				unsoldOrders = append(unsoldOrders, order)
				totalUnsoldProfit += floatingProfit
				totalUnsoldQuantity += remaining
				totalUnsoldMarketValue += marketValue
				totalUnsoldCost += totalCost
				if floatingProfit > 0 {
					unsoldWinCount++
				} else if floatingProfit < 0 {
					unsoldLossCount++
				}
			}
		}
	}

	logger.Warn("")
	logger.Warn("【当前策略持仓】")

	if len(unsoldOrders) == 0 {
		logger.Warn("  无持仓")
	} else {
		// 使用表格打印机打印持仓列表
		headers := []string{"Symbol", "OrderId", "Price", "CurrentPrice", "Num", "MarketValue", "Profit"}
		tp := utils.NewTablePrinter(headers)
		tp.SetUseSimpleChars(false) // 使用美观字符

		for _, order := range unsoldOrders {
			symbol := utils.GetString(order, "symbol")
			orderID := utils.GetString(order, "order_id")
			price := utils.GetFloat64(order, "price")
			quantity := utils.GetInt64(order, "remaining")
			profit := utils.GetFloat64(order, "profit")

			currentPrice := qs.PriceManager.GetCurrentPrice(symbol)

			var marketValue float64 = 0
			var totalCost float64 = 0
			category := utils.GetInt32(order, "category")
			if category <= 0 {
				logger.Error("category err %d", category)
				continue
			}

			short := utils.IsShortOrderCategory(category)
			if short {
				marketValue = float64(quantity) * price
				totalCost = float64(quantity) * currentPrice
				profit += marketValue - totalCost
			} else {
				marketValue = float64(quantity) * currentPrice
				totalCost = float64(quantity) * price
				profit += marketValue - totalCost
			}

			profitStr := fmt.Sprintf("%+.2f", profit)

			tp.AddRow([]string{
				symbol,
				orderID,
				fmt.Sprintf("%.2f", price),
				fmt.Sprintf("%.2f", currentPrice),
				fmt.Sprintf("%d", quantity),
				fmt.Sprintf("%.2f", marketValue),
				profitStr,
			})
		}

		// 输出表格
		tableStr := tp.Build()
		for _, line := range strings.Split(tableStr, "\n") {
			if line != "" {
				logger.Warn(line)
			}
		}
	}

	// 打印盈亏汇总（使用表格打印机）
	logger.Warn("")
	logger.Warn("【当前策略盈亏汇总】")

	summaryHeaders := []string{"Project", "money", "Tips"}
	summaryTp := utils.NewTablePrinter(summaryHeaders)
	summaryTp.SetUseSimpleChars(false)

	summaryTp.AddRow([]string{"WinMoney", fmt.Sprintf("%.2f", totalSoldProfit), ""})
	summaryTp.AddRow([]string{"FloatingMoney", fmt.Sprintf("%.2f", totalUnsoldProfit),
		fmt.Sprintf("Floating Win:%d, Lost:%d", unsoldWinCount, unsoldLossCount)})
	summaryTp.AddRow([]string{"TotalWinMoney", fmt.Sprintf("%.2f", totalSoldProfit+totalUnsoldProfit), ""})

	summaryStr := summaryTp.Build()
	for _, line := range strings.Split(summaryStr, "\n") {
		if line != "" {
			logger.Warn(line)
		}
	}

	// 打印持仓统计（使用表格打印机）
	if totalUnsoldQuantity > 0 {
		avgCost := totalUnsoldCost / float64(totalUnsoldQuantity)
		avgPrice := totalUnsoldMarketValue / float64(totalUnsoldQuantity)
		avgProfitRate := (avgPrice - avgCost) / avgCost * 100

		logger.Warn("")
		logger.Warn("【Postion】")

		statsHeaders := []string{"Project", "Value"}
		statsTp := utils.NewTablePrinter(statsHeaders)
		statsTp.SetUseSimpleChars(false)

		statsTp.AddRow([]string{"Total Postion", fmt.Sprintf("%d", totalUnsoldQuantity)})
		statsTp.AddRow([]string{"Total Unsold Cost", fmt.Sprintf("%.2f", totalUnsoldCost)})
		statsTp.AddRow([]string{"Total Unsold MarketValue", fmt.Sprintf("%.2f", totalUnsoldMarketValue)})
		statsTp.AddRow([]string{"Avg Cost", fmt.Sprintf("%.4f", avgCost)})
		statsTp.AddRow([]string{"Avg Price", fmt.Sprintf("%.4f", avgPrice)})
		statsTp.AddRow([]string{"Total Cost", fmt.Sprintf("%.4f", totalUnsoldMarketValue-totalUnsoldCost)})
		statsTp.AddRow([]string{"Avg Profit Rate", fmt.Sprintf("%.2f%%", avgProfitRate)})

		statsStr := statsTp.Build()
		for _, line := range strings.Split(statsStr, "\n") {
			if line != "" {
				logger.Warn(line)
			}
		}
	}
}

// printAllCompletedOrders 打印所有订单详细
func (qs *QuantSystem) printAllCompletedOrders() {
	if len(qs.CompletedOrders) == 0 {
		logger.Warn("【所有订单详细】无订单记录")
		return
	}

	// 准备表头
	headers := []string{"Symbol", "OrderId", "Category", "Action", "Price", "Num", "time", "CloseOrderId", "Reason", "StrategyName"}

	// 创建表格打印机
	tp := utils.NewTablePrinter(headers)

	lenMax := 50
	allOrders := make([]*types.CompletedOrder, 0, len(qs.CompletedOrders))
	for _, order := range qs.CompletedOrders {
		allOrders = append(allOrders, order)
	}
	sort.Slice(allOrders, func(i, j int) bool {
		return allOrders[i].Timestamp.Before(allOrders[j].Timestamp)
	})
	// 添加数据行
	for _, order := range allOrders {
		// 格式化触发原因，限制长度
		orderID := order.OrderID
		if len([]rune(orderID)) > lenMax {
			orderID = string([]rune(orderID)[:lenMax-3]) + "..."
		}
		reason := order.Reason
		if len([]rune(reason)) > lenMax {
			reason = string([]rune(reason)[:lenMax-3]) + "..."
		}

		row := []string{
			order.Symbol,
			orderID,
			utils.GetCategoryStr(order.Category),
			string(order.Action),
			fmt.Sprintf("%.2f", order.Price),
			utils.FormatQuantity(order.Quantity),
			utils.FormatTime(order.Timestamp),
			order.CloseOrderID,
			reason,
			order.StrategyName,
		}
		tp.AddRow(row)
	}

	// 构建表格
	tableStr := tp.Build()

	// 输出
	logger.Warn("")
	logger.Warn("【所有订单详细】")
	for _, line := range strings.Split(tableStr, "\n") {
		if line != "" {
			logger.Warn(line)
		}
	}
}

// printSummary 打印交易汇总
func (qs *QuantSystem) printSummary(summary *TradeSummary, currentStrategy strategy.Strategy) {
	if currentStrategy == nil {
		return
	}
	strategyName := currentStrategy.GetName()

	logger.Warn("================================%s begin", strategyName)
	data := currentStrategy.GetStrategyConfig()
	for k, v := range data {
		logger.Warn("策略信息: %s=%v", k, v)
	}
	strategyCom := qs.GetAllFlagConfigField(fmt.Sprintf("%sCom", data["shortName"]))
	for k, v := range strategyCom {
		logger.Warn("策略公共信息: %s=%v", k, v)
	}
	strategyData := qs.GetAllFlagConfigField(fmt.Sprintf("%s", data["shortName"]))
	for k, v := range strategyData {
		logger.Warn("策略当前信息: %s=%v", k, v)
	}
	if data["strategyType"] != "" {
		strategyAllData := qs.GetAllFlagConfigField(fmt.Sprintf("%s%s", data["shortName"], data["strategyType"]))
		for k, v := range strategyAllData {
			logger.Warn("策略当前信息: %s=%v", k, v)
		}
	}
	logger.Warn("================================%s end", strategyName)

	// 策略信息表格
	logger.Warn("【策略信息】")
	infoHeaders := []string{"project", "value"}
	infoTp := utils.NewTablePrinter(infoHeaders)
	infoTp.SetUseSimpleChars(false)
	infoTp.AddRow([]string{"strategy name", strategyName})
	infoTp.AddRow([]string{"Stop Loss Ratio", fmt.Sprintf("%.2f%%", qs.AppConfig.StopLossPercent)})
	infoTp.AddRow([]string{"Position Max Ratio", fmt.Sprintf("%.2f%%", qs.AppConfig.MaxPositionPercent)})

	infoStr := infoTp.Build()
	for _, line := range strings.Split(infoStr, "\n") {
		if line != "" {
			logger.Warn(line)
		}
	}

	// ============================================
	// 计算未卖出订单的浮动盈亏统计
	// ============================================
	state := currentStrategy.GetCurrentState()
	if orders, ok := state["orders"].([]map[string]interface{}); ok {
		for _, order := range orders {
			category := utils.GetInt32(order, "category")
			if category <= 0 {
				logger.Error("category err %d", category)
				continue
			}
			short := utils.IsShortOrderCategory(category)
			if short {
				isClosed := false
				if val, ok := order["is_closed"]; ok {
					if b, ok := val.(bool); ok {
						isClosed = b
					}
				}
				remaining := utils.GetInt64(order, "remaining")
				if !isClosed && remaining > 0 {
					symbol := utils.GetString(order, "symbol")
					price := utils.GetFloat64(order, "price")
					profit := utils.GetFloat64(order, "profit")

					currentPrice := qs.PriceManager.GetCurrentPrice(symbol)
					floatingProfit := profit + float64(remaining)*(price-currentPrice)

					summary.TotalProfitLoss -= floatingProfit
					summary.TotalBuyAmount -= float64(remaining) * price

					if _, exists := summary.SymbolStats[symbol]; !exists {
						summary.SymbolStats[symbol] = &SymbolStats{Symbol: symbol}
					}
					stats := summary.SymbolStats[symbol]
					stats.Profit += floatingProfit

					if floatingProfit > 0 {
						stats.WinCount++
						summary.floatingWinCount++
					} else if floatingProfit < 0 {
						stats.LossCount++
						summary.floatingLossCount++
					}
				}
			} else {
				remaining := utils.GetInt64(order, "remaining")
				if remaining > 0 {
					symbol := utils.GetString(order, "symbol")
					buyPrice := utils.GetFloat64(order, "price")
					profit := utils.GetFloat64(order, "profit")

					currentPrice := qs.PriceManager.GetCurrentPrice(symbol)

					floatingProfit := profit + float64(remaining)*(currentPrice-buyPrice)
					summary.TotalProfitLoss += floatingProfit

					if _, exists := summary.SymbolStats[symbol]; !exists {
						summary.SymbolStats[symbol] = &SymbolStats{Symbol: symbol}
					}
					stats := summary.SymbolStats[symbol]
					stats.Profit += floatingProfit

					if floatingProfit > 0 {
						stats.WinCount++
						summary.floatingWinCount++
					} else if floatingProfit < 0 {
						stats.LossCount++
						summary.floatingLossCount++
					}
				}
			}
		}
	}

	// 从策略内部打印订单明细
	qs.printStrategyOrders(currentStrategy)
	// 打印当前策略持仓
	qs.printStrategyPositions(currentStrategy)
}

// printAllSummary 打印所有交易汇总
func (qs *QuantSystem) printAllSummary() {
	logger.Warn("================================")
	if myconfig.IsTestMode() {
		logger.Warn("【测试信息】")
		data := qs.GetAllFlagConfigField("Test")
		for k, v := range data {
			logger.Warn("  测试配置%s: %s", k, v)
		}
	}
	logger.Warn("================================")
	logger.Warn("=========== 配置信息 ===========")
	log := qs.GetAllFlagConfigField("Log")
	for k, v := range log {
		logger.Warn("Log信息: %s=%v", k, v)
	}
	com := qs.GetAllFlagConfigField("Com")
	for k, v := range com {
		logger.Warn("公共信息: %s=%v", k, v)
	}
	logger.Warn("================================")

	summary := qs.calculateSummary()
	allGetCurrentStrategies := qs.StrategyMgr.GetCurrentStrategies()
	for _, s := range allGetCurrentStrategies {
		qs.printSummary(summary, s)
	}

	logger.Warn("")
	logger.Warn("╔═════════════════════════════════════════════════════════════════╗")
	logger.Warn("║              Transaction Statistics Summary Report              ║")
	logger.Warn("╚═════════════════════════════════════════════════════════════════╝")
	logger.Warn("")

	// 准备股票统计数据
	stockHeaders := []string{"Symbol", "BuyPrice", "SellPrice", "AlreadyProfit", "FloatingProfit", "TotalProfit", "WinNum", "LostNum", "TotalNum"}
	stockTp := utils.NewTablePrinter(stockHeaders)
	stockTp.SetUseSimpleChars(false)

	for symbol, stats := range summary.SymbolStats {
		stockTp.AddRow([]string{
			symbol,
			fmt.Sprintf("%.2f", stats.BuyAmount),
			fmt.Sprintf("%.2f", stats.SellAmount),
			fmt.Sprintf("%+.2f", stats.ProfitLoss),
			fmt.Sprintf("%+.2f", stats.Profit),
			fmt.Sprintf("%+.2f", stats.ProfitLoss+stats.Profit),
			fmt.Sprintf("%d", stats.WinCount),
			fmt.Sprintf("%d", stats.LossCount),
			fmt.Sprintf("%d", stats.WinCount+stats.LossCount),
		})
	}

	stockStr := stockTp.Build()
	for _, line := range strings.Split(stockStr, "\n") {
		if line != "" {
			logger.Warn(line)
		}
	}

	// 交易统计表格
	logger.Warn("")
	logger.Warn("【所有订单交易统计】")
	tradeHeaders := []string{"Project", "Num", "Tips"}
	tradeTp := utils.NewTablePrinter(tradeHeaders)
	tradeTp.SetUseSimpleChars(false)

	tradeTp.AddRow([]string{"AlreadyNum", fmt.Sprintf("%d", summary.WinCount+summary.LossCount), ""})
	tradeTp.AddRow([]string{"WinNum", fmt.Sprintf("%d", summary.WinCount), ""})
	tradeTp.AddRow([]string{"LostNum", fmt.Sprintf("%d", summary.LossCount), ""})
	tradeTp.AddRow([]string{"WinRate", fmt.Sprintf("%.2f%%", summary.WinRate), ""})
	tradeTp.AddRow([]string{"---------------------------------", "", ""})
	tradeTp.AddRow([]string{"FloatingWinNum", fmt.Sprintf("%d", summary.floatingWinCount), ""})
	tradeTp.AddRow([]string{"FloatingLostNum", fmt.Sprintf("%d", summary.floatingLossCount), ""})
	tradeTp.AddRow([]string{"---------------------------------", "", ""})
	tradeTp.AddRow([]string{"TotalWinNum", fmt.Sprintf("%d", summary.WinCount+summary.floatingWinCount), ""})
	tradeTp.AddRow([]string{"TotalLostNum", fmt.Sprintf("%d", summary.LossCount+summary.floatingLossCount), ""})

	totalTrades := summary.WinCount + summary.floatingWinCount + summary.LossCount + summary.floatingLossCount
	if totalTrades > 0 {
		avgProfit := summary.TotalProfitLoss / float64(totalTrades)
		tradeTp.AddRow([]string{"AverageProfit", fmt.Sprintf("%.2f", avgProfit), ""})
		totalWinRate := float64(summary.WinCount+summary.floatingWinCount) / float64(totalTrades) * 100
		tradeTp.AddRow([]string{"TotalWinRate", fmt.Sprintf("%.2f%%", totalWinRate), ""})
	}

	tradeStr := tradeTp.Build()
	for _, line := range strings.Split(tradeStr, "\n") {
		if line != "" {
			logger.Warn(line)
		}
	}

	// 打印所有订单详细
	qs.printAllCompletedOrders()

	logger.Warn("")
	logger.Warn("╔═════════════════════════════════════════════════════════════════╗")
	logger.Warn("║                       The report is over                        ║")
	logger.Warn("╚═════════════════════════════════════════════════════════════════╝")

	if qs.AppConfig.TestOpenDraw {
		var macdName, priceName string = "", ""
		if myconfig.IsTestMode() {
			if qs.AppConfig.TestXlsx != "" {
				if idx := strings.LastIndex(qs.AppConfig.TestXlsx, "."); idx != -1 {
					str := qs.AppConfig.TestXlsx[:idx]
					if strIdx := strings.LastIndex(str, "/"); strIdx != -1 {
						str = str[strIdx+1:]
					}
					str += "_" + qs.AppConfig.LogFileName
					macdName = "logs/macd_" + str + ".png"
					priceName = "logs/price_" + str + ".png"
				}
			}
		} else {
			str := qs.AppConfig.LogFileName
			macdName = "logs/macd_" + str + ".png"
			priceName = "logs/price_" + str + ".png"
		}
		if macdName == "" || priceName == "" {
			logger.Error("macdName=%s, priceName=%s", macdName, priceName)
		} else {
			allOrders := make([]*types.CompletedOrder, 0, len(qs.CompletedOrders))
			for _, order := range qs.CompletedOrders {
				allOrders = append(allOrders, order)
			}
			sort.Slice(allOrders, func(i, j int) bool {
				return allOrders[i].Timestamp.Before(allOrders[j].Timestamp)
			})
			for _, symbols := range qs.AppConfig.Symbols {
				qs.PriceManager.DrawMACD(symbols, macdName, allOrders)
				qs.PriceManager.DrawPrice(symbols, priceName, allOrders)
			}
		}
	}
}
