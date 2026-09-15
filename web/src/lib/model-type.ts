// 模型类型：英文键存库、中文标签展示，与后端 model.AllModelTypes 一一对应。
//
// 用 as const 数组而不是 Record<string, string>：前者的 value 会被推导成字面量联合类型，
// 拼错 key 时是编译错误而不是静默拿到 undefined。
// （tsconfig 开了 erasableSyntaxOnly，所以不能用 enum。）
//
// 本文件刻意不含任何组件：oxlint 的 react/only-export-components 不允许把
// 组件和非组件导出混在同一个文件里。
export const MODEL_TYPE_OPTIONS = [
  { value: 'image', label: '图像生成' },
  { value: 'video', label: '视频生成' },
  { value: 'text', label: '文本生成' },
  { value: 'music', label: '音乐生成' },
  { value: 'other', label: '其他' },
] as const

export type ModelType = (typeof MODEL_TYPE_OPTIONS)[number]['value']

// 未知值原样返回，避免脏数据在界面上渲染成空白
export function modelTypeLabel(value?: string): string {
  return MODEL_TYPE_OPTIONS.find((o) => o.value === value)?.label ?? value ?? ''
}
