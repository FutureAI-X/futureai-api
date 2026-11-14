export interface CreditRuleCondition {
  id: number
  param_path: string
  param_value: string
}

export interface CreditRuleItem {
  id: number
  credits: number
  conditions?: CreditRuleCondition[]
}

export interface CreditRule {
  id: number
  model_id: number
  rule_type: string
  base_credits: number
  description?: string
  /** 每张参考图消耗的积分，0 = 不计费 */
  ref_image_credits?: number
  /** per_token：文本输入每百万 token 的积分 */
  input_text_credits_per_mtok?: number
  /** per_token：图片输入每百万 token 的积分 */
  image_input_credits_per_mtok?: number
  /** per_token：图片输出每百万 token 的积分 */
  image_output_credits_per_mtok?: number
  items?: CreditRuleItem[]
}

export interface PricingModel {
  id: number
  name: string
  description?: string
  tags?: string
  owner: string
  status: number
  /** 模型类型：image / video / text / music / other。老接口可能没有，故可选 */
  type?: string
  credit_rule?: CreditRule
}

export interface PricingData {
  models: PricingModel[]
}
