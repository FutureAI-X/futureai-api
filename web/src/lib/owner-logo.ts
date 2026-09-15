// 开发者（模型的 owner 字段）名称 → 内置 LOGO 路径。
//
// 键必须是 normalizeOwner() 处理后的形式：小写、去掉空格与 . _ - 。
// 新增品牌见 web/public/logos/README.md。
//
// 本文件刻意不含组件：oxlint 的 react/only-export-components 不允许
// 组件与非组件导出混在同一个文件里。
const OWNER_LOGOS: Record<string, string> = {
  openai: '/logos/openai.svg',
}

// 归一化 owner 以便匹配：'OpenAI' / 'openai' / 'Open AI' / 'x.ai' 都能命中同一个键
function normalizeOwner(owner: string): string {
  return owner.toLowerCase().replace(/[\s._-]+/g, '')
}

// ownerLogoUrl 返回该开发者的内置 LOGO 地址；没有内置图标时返回 undefined，
// 由调用方回落到字母头像。
export function ownerLogoUrl(owner?: string): string | undefined {
  if (!owner) return undefined
  return OWNER_LOGOS[normalizeOwner(owner)]
}
