/**
 * owner（模型开发者）归一化。
 *
 * 忽略大小写、首尾空白和常见分隔符，保证 OpenAI / openai / Open AI
 * 在分组、筛选和 LOGO 匹配时落到同一个键上。展示时仍使用原始写法。
 */
export function normalizeOwner(owner?: string): string {
  return (owner || '').trim().toLowerCase().replace(/[\s._-]+/g, '')
}

/** owner 为空时的展示文案。 */
export const OWNER_UNKNOWN_LABEL = '未标注'
