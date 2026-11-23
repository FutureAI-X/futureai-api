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

/** 视频计费的单个分辨率档位 */
export interface VideoCreditRate {
  id?: number
  rule_id?: number
  /** 输出分辨率档位，统一大写 */
  resolution: string
  /** 每个参考视频秒消耗的积分 */
  input_video_credits_per_second: number
  /** 不含参考视频时每秒输出消耗的积分 */
  output_credits_per_second_no_ref: number
  /** 含参考视频时每秒输出消耗的积分 */
  output_credits_per_second_with_ref: number
  created_at?: string
  updated_at?: string
}

/** 视频模型计费规则 */
export interface VideoCreditRule {
  id: number
  model_id: number
  description?: string
  status: number
  /** 每次请求固定收取的积分，0=不收取 */
  base_credits_per_request: number
  /** 基础扣费说明，展示给用户 */
  base_credits_description?: string
  /** 每张参考图积分 */
  image_credits_per_item: number
  /** 每请求免费的前 N 张参考图 */
  image_free_items: number
  /** 每个参考音频积分 */
  audio_credits_per_item: number
  rates?: VideoCreditRate[]
  created_at?: string
  updated_at?: string
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
  /** 视频模型的独立计费规则 */
  video_credit_rule?: VideoCreditRule
}

export interface PricingData {
  models: PricingModel[]
}
