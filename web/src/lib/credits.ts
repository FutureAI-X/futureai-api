/**
 * 积分的展示与录入口径，与后端 model.CreditPrecision 保持一致。
 *
 * 存储层刻意比它宽（numeric(20,10)），所以放宽这个数字只需要同时改后端常量——
 * 不需要动数据库，存量数据本来就只有这些位，是纯加法、不会被重新舍入。
 */
export const CREDIT_DECIMALS = 3

/** 业务精度下的最小非零单位（3 位 → 0.001） */
const MIN_VISIBLE = 10 ** -CREDIT_DECIMALS

// 固定 CREDIT_DECIMALS 位小数的格式（1 → 1.000）。
// 界面上所有积分统一这个格式，不做「去尾零」——同一列里 1 和 1.5 宽度不一，
// 看起来像是没按同一口径显示。
const creditFormatter = new Intl.NumberFormat('zh-CN', {
  minimumFractionDigits: CREDIT_DECIMALS,
  maximumFractionDigits: CREDIT_DECIMALS,
})

// 判定「取整值确实大于原值」的相对阈值。
// 浮点噪声在 1e-16 相对量级，而真正超精度的那点残渣至少在 1e-4 绝对量级，
// 两者相差十几个数量级，取 1e-9 能干净地把它们分开。
const NOISE_RATIO = 1e-9

/**
 * 收敛到业务精度，并保证结果不超过原值（向下取）。
 *
 * 为什么不是直接截断：浮点误差会把价格带偏——`1.005 * 1000` 在 JS 里是
 * `1004.9999999999999`，截断得到 1.004，把一个 3 位小数的价格显示低 0.001。
 * 先四舍五入、超出再退一格，两种情况都对。
 *
 * 为什么不用四舍五入：历史遗留的 4 位小数余额（1.2345）会被显示成 1.235，
 * 比实际多，用户按这个数下单会莫名其妙地收到「积分不足」。向下取保证
 * **显示出来的每个数都花得起**，也让存量里的小数残渣彻底无害——不必再为了
 * 它去跑一次数据订正脚本。
 *
 * 「超出」要比到噪声以上才算数：算出来的值常落在真值下方一点点，
 * `1.000 - 0.999` 是 `0.0009999999999999998`，按字面退一格会显示成 `<0.001`，
 * 而正确答案是 0.001。
 */
function floorCredits(value: number): number {
  const scale = 10 ** CREDIT_DECIMALS
  const scaled = Math.round(value * scale)
  const rounded = scaled / scale
  if (rounded - value <= Math.abs(value) * NOISE_RATIO) return rounded
  return (scaled - 1) / scale
}

/**
 * 格式化积分：固定 CREDIT_DECIMALS 位小数（1 → `1.000`）。
 *
 * 非零值绝不能显示成 0——一个 0.001 的价格显示成 0，用户会以为免费，
 * 所以小于最小单位时显示成 `<0.001` 而不是 `0.000`。
 */
export function formatCredits(value: number): string {
  if (!Number.isFinite(value)) return '0'

  const floor = floorCredits(value)
  if (floor === 0) {
    return value === 0 ? creditFormatter.format(0) : `<${MIN_VISIBLE.toFixed(CREDIT_DECIMALS)}`
  }
  return creditFormatter.format(floor)
}

/**
 * 校验积分输入的小数位是否不超过业务精度。
 *
 * 按字符串判断而不是对数值取小数部分：JS 对小于 1e-6 的数会转成科学计数法，
 * `(1e-7).toString()` 是 `"1e-7"`，按数值取小数部分会漏判、把它当合法输入放行。
 * 位数正好卡在 1e-6 边界上时，这个洞就会露出来。
 */
export function validCreditInput(raw: string): boolean {
  const text = raw.trim()
  if (!/^(\d+(\.\d*)?|\.\d+)$/.test(text)) return false

  const dot = text.indexOf('.')
  return dot < 0 || text.length - dot - 1 <= CREDIT_DECIMALS
}

/**
 * 一组积分输入里是否有「填了但格式不对」的。
 *
 * 空值不算：基础积分必填、参考图积分留空表示不计费，是否允许为空由各表单自己判断，
 * 这里只回答「格式」这一个问题。
 */
export function hasMalformedCreditInput(values: string[]): boolean {
  return values.some((v) => v.trim() !== '' && !validCreditInput(v))
}
