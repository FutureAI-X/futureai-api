package model

import (
	"errors"
	"math"
)

// CreditPrecision 积分的业务精度（小数位）：录入、计费收敛、展示统一按它来。
//
// 刻意比存储标度（CreditStorageScale）小：列比业务口径宽时，Go 里算出的值在列里
// 能精确放下，不会被数据库二次舍入，也就不存在「内存值与实际扣费不一致」。
//
// 放宽到 4 位只需改这一个常量（以及前端 lib/credits.ts 的 CREDIT_DECIMALS）：
// 不需要动 schema，也不需要任何数据订正脚本。
//
// 需要维持的不变量只有两条，刻意不包含「库里只能有 N 位小数」：
//
//  1. 新写入的积分都是业务精度——录入侧校验，计费侧收敛（RoundCredits）。
//  2. 展示值永远不超过实际值——前端向下取整（formatCredits）。
//
// 第 2 条是让历史残渣无害的关键：早期 AdjustUserCredits 不校验位数也不收敛，
// 存量余额里可能留着 4~6 位小数。这类值不需要（也不该）用脚本清理——
// 展示向下取就永远不会显示出花不起的数字，残渣既花不掉也不影响对账。
//
// 因此**不要**给积分列加「只能有 N 位小数」的 CHECK 约束：它会卡住这些历史残渣，
// 让服务在启动时就起不来，而这正是「改精度必须跑脚本」的来源。
//
// 刻意不做成可配置项：改精度不会重新舍入存量数据，一旦做成配置，
// 一次配置操作就会让库里同时躺着两种精度的历史余额。
const CreditPrecision = 3

// CreditStorageScale 积分列的存储标度（小数位），大于业务精度是刻意的，
// 见 CreditPrecision 的说明。
//
// 它与各处 gorm tag 上的字面量 numeric(20,10) 是同一口径（struct tag 里引用不了常量），
// 改这里必须一起改 tag；ensureCreditColumnScale 会按本常量把历史库的列放宽。
const CreditStorageScale = 10

// creditScale 业务精度对应的整数倍数（3 位 → 1000）
var creditScale = math.Pow(10, CreditPrecision)

// RoundCredits 把积分收敛到业务精度。
//
// 浮点累加会产出 0.0030000000000000005 这类值，必须在写库前收敛：
// 否则内存里的值与落库的值不一致，对账时会被当成差异。
func RoundCredits(v float64) float64 {
	return math.Round(v*creditScale) / creditScale
}

// ValidCreditPrecision 判断 v 的小数位是否不超过业务精度。
// 与 RoundCredits 互为配套：收敛后再校验必然通过，因此录入侧先校验、写库前再收敛。
func ValidCreditPrecision(v float64) bool {
	return math.Abs(v-RoundCredits(v)) < 1e-9
}

// ── 账本边界的收敛与守卫 ──
//
// 收敛发生在写 users.credits 的三个函数内部，而不是只靠调用方：金额在账本边界
// 再收敛一次并校验，任何新增的调用方忘了收敛也脏不了库。历史上
// AdjustUserCredits 就是这么漏出 4~6 位小数的——问题不在浮点，在纪律。

// NormalizeDeductAmount 收敛扣费金额，并拒绝会让扣费失去意义的取值。
//
// 刻意允许「本来就是 0」的金额（免费模型是受支持的配置），但拒绝「正数却收敛成 0」：
// 那意味着模型单价低于最小单位，静默放行等于白送。
func NormalizeDeductAmount(v float64) (float64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("扣费金额不是有效数值")
	}

	rounded := RoundCredits(v)
	// 负数扣费在 SQL 里是 credits - (-x)，会反向给用户加积分，且日志记为负扣费
	if rounded < 0 {
		return 0, errors.New("扣费金额不能为负数")
	}
	if v > 0 && rounded == 0 {
		return 0, errors.New("扣费金额低于最小单位")
	}
	return rounded, nil
}

// NormalizeRefundAmount 收敛退款金额。
//
// 与扣费侧的差异是刻意的：这里允许收敛成 0（空操作），因为退款金额来自库里已存的
// task.Credits，退款失败会连累任务状态转换失败，让任务卡在非终态反复重试；
// 而少退一笔低于最小单位的残渣没有任何影响。
func NormalizeRefundAmount(v float64) (float64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("退款金额不是有效数值")
	}

	rounded := RoundCredits(v)
	// 负数退款等于反向扣费，绕过了扣费侧的记账
	if rounded < 0 {
		return 0, errors.New("退款金额不能为负数")
	}
	return rounded, nil
}
