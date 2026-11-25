# AGENTS.md — LongBridge-QuantSystem

## Build / run

- Build: `go build` → `LongBridge-QuantSystem.exe`
- Exe reads `config.env` from **exe directory** (`utils.GetExecpath()`), not CWD
- No `go test`, Makefile, CI, or lint config exist
- `.vscode/launch.json` provides Go debug launch

## VirtualClock (critical gotcha)

**`utils.Now()` / `utils.Sleep()` everywhere** — never `time.Now()` / `time.Sleep()`.

- `utils.VirtualClockInit()` called in `NewQuantSystem()`; `utils.Now()` **panics** if called before
- Test mode sets scale to `TestTimeScale` (e.g. 2); test strategies read Excel prices through `VirtualEvent` heap
- All time logic (strategy ticks, order timeouts, MACD) uses virtual time

## Test mode

- `TestOpen=true` in `config.env` → `TradeCtx` / `QuoteCtx` remain nil (no API)
- `strategy_t/` backtests via Excel → `VirtualEvent` heap; distinct from live strategies in `strategy/`
- Only **one** test strategy at a time (unlike live multi-strategy)
- `TestXlsx` path uses **forward slashes**: `test_data/test20260611.xlsx`
- Excel columns: A=Symbol, B=Sequence, C=LastDone, D=Open, E=High, F=Low, G=Timestamp(unix sec), H=Volume, I=Turnover, J=TradeStatus(int), K=TradeSession(int)

## Batch test runner (`run_all_test.bat`)

- Iterates `test_data/*.xlsx`, overwrites `TestXlsx=` in `config.env`, runs exe per file
- **Inline PowerShell** writes config.env without BOM via `[System.Text.UTF8Encoding]::new($false)` — safe for `godotenv`
- Replaces `\` → `/` in paths via `set "xf=!xf:\=/!"`
- Runs `extract_logs.exe` after all tests complete

## Config architecture

- `config.env` parsed by `godotenv` + `utils.GetEnv*` helpers; loaded from exe directory
- `config/config.go` — `AppConfig` singleton (`GetConfig()`, `sync.Once`)
- Struct `flag` tags group fields for runtime console discovery, **not** serialization
- `flag:"ro"` = read-only; others name a category (`"Com"`, `"Log"`, `"TimeTrend"`, `"GridPercent"`, etc.)
- Strategy config lookup order in `system.go:GetStrategyConfig()`: `flag:"<shortName>Com"` → `flag:"<shortName>"` → `flag:"<shortName><strategyType>"`
- TimeTrend specific: `TrendFlowPeriod`, `TrendFlowEntryThreshold`, `TrendFlowExitThreshold` — K-line flow ratio entry/exit filters

## K-line flow ratio (TimeTrend strategy)

`getFlowRatio()` in `strategy_time_trend.go` — estimates buyer vs seller pressure:

- **阳线** (Close>Open) → volume counts as buying pressure
- **阴线** (Close<Open) → volume counts as selling pressure
- **平盘** (Close=Open) → volume split 50/50
- Returns `[-1, 1]`: positive = net buyer-dominated
- Used for: early exit on flow reversal (`TrendFlowExitThreshold`), and entry filter (`TrendFlowEntryThreshold`)

## Path resolution

Both `config.env` loading and `NewExcelQuoteReader` resolve relative paths against `utils.GetExecpath()` (exe directory), not `os.Getwd()`. Works regardless of CWD as long as `config.env` and `test_data/` are next to the exe.

## Multi-strategy

- `StrategyManager.currentStrategy` is `[]Strategy`, not a single strategy
- `config.env` `StrategyType` is comma-separated (e.g. `TimeTrend,GridPercent`)

## Strategy registry

Live (`strategy/strategy_manager.go:registerAllStrategies()`):

| GetName() | shortName | strategyType | `StrategyType=` value |
|---|---|---|---|
| `GridPercent` | `Grid` | `Percent` | `GridPercent` |
| `TimeTrend` | `TimeTrend` | `""` | `TimeTrend` |
| `TimeSignal` | `TimeSignal` | `""` | `TimeSignal` |
| `TestOrder` | `TestOrder` | `""` | `TestOrder` |

Test (`strategy_t/strategy_t_manager.go`):

| GetName() | `TestStrategy=` |
|---|---|
| `price` | `price` |

## Architecture

| Package | Role |
|---|---|
| `main.go` | Entry: `NewQuantSystem()` → `system.Run()` |
| `quant/` | Core: startup, main loop, price manager, orders, positions, risk, console commands (~600 lines in `console.go`) |
| `strategy/` | Live strategies: `GridPercent`, `TimeTrend`, `TimeSignal`, `TestOrder` |
| `strategy_t/` | Backtest manager with event heap + Excel reader; `price` strategy |
| `trading/` | Session control (pre/regular/after/overnight bitmask), US holiday calendar |
| `config/` | `AppConfig` singleton, flag-tag reflection |
| `types/` | Shared interfaces + order/price/MACD types |
| `utils/` | VirtualClock, timezone, Excel reader, env getters, `GetExecpath()` |
| `logger/` | File + HTML + console loggers, per-channel log levels |

## Side effects

- `time.Local` **globally reassigned** to configured `TimeZone` (default `America/New_York`) at startup

## Startup command file

- `cmd.txt` in exe directory — each line is a console command executed at startup (e.g. `set SlopeFactor 0.00002`)
