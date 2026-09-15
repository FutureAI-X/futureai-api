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
  /** 参考图参数名（逗号分隔） */
  ref_image_params?: string
  items?: CreditRuleItem[]
}

export interface PricingModel {
  id: number
  name: string
  description?: string
  tags?: string
  owner: string
  status: number
  credit_rule?: CreditRule
}

export interface PricingData {
  models: PricingModel[]
}
