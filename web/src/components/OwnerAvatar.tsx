import { ownerLogoUrl } from '../lib/owner-logo'

interface OwnerAvatarProps {
  /** 开发者名称，如 "OpenAI" */
  owner?: string
  /** owner 为空时的兜底字母来源 */
  fallbackName?: string
}

// OwnerAvatar 模型卡片/详情弹框左上角的开发者标识。
//
// 命中内置 LOGO 时用 CSS mask 渲染而不是 <img>：图标本身是 fill="currentColor"，
// 而 <img> 里的 SVG 是独立文档、继承不到页面的 color，会解析成黑色——暗色模式下
// 直接看不见。走 mask 则用背景色上色，跟随主题 token，亮暗两套都正常。
//
// 两种形态都标 aria-hidden：开发者名称的文本就在旁边，标识是装饰性的，
// 让读屏再念一遍只是噪音。
export function OwnerAvatar({ owner, fallbackName }: OwnerAvatarProps) {
  const logoUrl = ownerLogoUrl(owner)

  // 字母取 owner 的首字母（此前取的是模型名首字母，导致同一厂商的模型
  // 全长得一样——gpt-image-* 三个卡片都是 "G"）
  const initial =
    owner?.charAt(0).toUpperCase() ||
    fallbackName?.charAt(0).toUpperCase() ||
    '?'

  return (
    <div className='bg-muted/40 flex size-10 shrink-0 items-center justify-center rounded-xl'>
      {/* 宽盒而不是 size-5：MOSS 这类约 2.7:1 的宽幅标识若塞进正方形，
          按 contain 缩放后只剩几像素高。方形图标仍受 h-5 限制保持 20×20，
          宽标识则能用满 32px 宽度。 */}
      {logoUrl ? (
        <span
          aria-hidden
          className='bg-muted-foreground h-5 w-8'
          style={{
            maskImage: `url(${logoUrl})`,
            WebkitMaskImage: `url(${logoUrl})`,
            maskSize: 'contain',
            WebkitMaskSize: 'contain',
            maskRepeat: 'no-repeat',
            WebkitMaskRepeat: 'no-repeat',
            maskPosition: 'center',
            WebkitMaskPosition: 'center',
          }}
        />
      ) : (
        <span aria-hidden className='text-muted-foreground text-lg font-bold'>
          {initial}
        </span>
      )}
    </div>
  )
}
