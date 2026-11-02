import type { LucideIcon } from 'lucide-react'
import {
  Contact,
  Film,
  Image as ImageIcon,
  Infinity as InfinityIcon,
  KeyRound,
  MessageCircle,
  UserRound,
  Video,
  Wallet,
} from 'lucide-react'
import type { ReactNode } from 'react'
import {
  Callout,
  CodeLine,
  ComingSoon,
  DocH2,
  DocImage,
  DocLink,
  DocP,
  DocStrong,
  DocUl,
  NetdiskCard,
  QrCard,
  Step,
  Steps,
} from './blocks'

export interface DocEntry {
  slug: string
  title: string
  summary: string
  icon: LucideIcon
  content: ReactNode
}

export interface DocGroup {
  title: string
  items: DocEntry[]
}

// public/ 下的静态资源。用 BASE_URL 拼接，部署在子路径时也不用改。
const image = (name: string) => `${import.meta.env.BASE_URL}docs/${name}`

// ── 一、账号与积分 ──
const accountCredits = (
  <div>
    <Callout>
      本站点不开放自助注册和积分充值入口。平台账号与积分均通过购买
      <DocLink to='/docs/consulting'>咨询服务</DocLink>
      获得。
    </Callout>

    <DocH2>账号获取</DocH2>
    <DocP>
      本站点不开放注册入口。购买咨询服务后，我们会赠送一个平台账号及等价值积分，详情见
      <DocLink to='/docs/consulting'>《咨询服务》</DocLink>。
    </DocP>
    <DocUl>
      <li>是否开放注册：否</li>
      <li>账号来源：购买咨询服务赠送</li>
    </DocUl>

    <DocH2>积分获取</DocH2>
    <DocP>
      使用本站点的模型将会消耗积分。本站点不开放积分充值入口；购买咨询服务将赠送等价值积分。
    </DocP>
    <DocUl>
      <li>是否开放充值：否</li>
      <li>积分来源：购买咨询服务赠送</li>
      <li>积分用途：调用本站点模型时消耗</li>
    </DocUl>
  </div>
)

// ── 二、咨询服务 ──
const consulting = (
  <div>
    <Callout>
      咨询服务无统一定价，您可以自行决定咨询价格，仅支持整数，如 1 元档、5
      元档、10 元档等。
    </Callout>

    <DocH2>服务说明</DocH2>
    <DocUl>
      <li>支持多次购买咨询服务</li>
      <li>购买咨询服务后赠送平台账号及等价值积分</li>
    </DocUl>

    <DocH2>购买全流程</DocH2>
    <Steps>
      <Step n={1} title='添加作者'>
        <p>
          推荐通过 QQ 群添加作者（进群后找群主），QQ、微信也都可以。添加时请备注：
          <DocStrong>咨询服务</DocStrong>。
        </p>
        <p>
          联系方式见
          <DocLink to='/docs/contact'>《联系我们》</DocLink>。
        </p>
      </Step>
      <Step n={2} title='确定服务档位'>
        <p>咨询服务仅支持整数价格，您可以根据自己的需要确定服务档位。</p>
      </Step>
      <Step n={3} title='付款'>
        <p>
          建议添加作者后再付款；也可以通过
          <DocLink to='/docs/payment'>《收款方式》</DocLink>
          付款，付款后把截图发送给作者。
        </p>
        <p>
          付款请备注：<DocStrong>咨询服务</DocStrong>。
        </p>
      </Step>
      <Step n={4} title='赠送平台账号'>
        <p>付款后会自动发送平台账号。</p>
      </Step>
      <Step n={5} title='赠送等价值积分'>
        <p>
          首次购买咨询服务：积分将直接赠送到新开的账号中。
        </p>
        <p>
          非首次购买咨询服务：积分将直接赠送到您原有的账号中。
        </p>
      </Step>
    </Steps>
  </div>
)

// ── 三、联系我们 ──
// 三张二维码图片放到 web/public/docs/，文件名见内容里的 image(...)。
const contact = (
  <div>
    <DocP>
      添加作者时请备注「咨询服务」。扫描下方任意一个二维码，即可通过 QQ
      群、QQ 或微信联系作者。
    </DocP>

    <Callout>
      推荐通过 QQ 群联系作者（进群后找群主）；QQ、微信也都可以。
    </Callout>

    <div className='mt-6 grid gap-4 sm:grid-cols-3'>
      <QrCard
        src={image('contact-qq-group.png')}
        label='QQ 群'
        hint='进群后找群主'
      />
      <QrCard src={image('contact-qq.png')} label='QQ' />
      <QrCard src={image('contact-wechat.png')} label='微信' />
    </div>
  </div>
)

// ── 四、收款方式 ──
const payment = (
  <div>
    <DocP>
      咨询服务付款请扫描下方支付宝或微信收款码。付款时请备注「咨询服务」，付款后把截图发送给作者。
    </DocP>

    <Callout>
      建议先通过
      <DocLink to='/docs/contact'>《联系我们》</DocLink>
      添加作者，确认服务档位后再付款。
    </Callout>

    <div className='mx-auto mt-6 grid max-w-[33rem] gap-4 sm:grid-cols-2'>
      <QrCard src={image('payment-alipay.png')} label='支付宝' />
      <QrCard src={image('payment-wechat.png')} label='微信' />
    </div>
  </div>
)

// ── 五、创建 API Key ──
const apiKey = (
  <div>
    <DocP>
      API Key 是调用 FutureAI API 的凭证。登录后按下面的步骤创建，再把 Key
      填入需要使用的客户端或应用中。
    </DocP>

    <Steps>
      <Step n={1} title='找到新建 API Key 入口'>
        <p>
          登录 FutureAI API 后，进入控制台，选择左侧「API Keys」菜单，点击「新建」即可。
        </p>
        <DocImage
          src={image('api-key-entry.png')}
          alt='控制台 → API Keys → 新建'
          hint='截图内容：控制台左侧 API Keys 菜单和「新建」按钮。'
        />
      </Step>
      <Step n={2} title='填入名称'>
        <p>此名称您可以自行命名，仅用于区分不同的 Key。</p>
        <DocImage
          src={image('api-key-name.png')}
          alt='新建 API Key 弹框中填入名称'
          hint='截图内容：新建 API Key 弹框中的名称输入框。'
        />
      </Step>
      <Step n={3} title='创建完成'>
        <p>
          复制创建好的 API Key 并自行保管。此 API Key
          在关闭弹框后将不会再次显示，如忘记可重新创建。
        </p>
        <DocImage
          src={image('api-key-created.png')}
          alt='创建完成后复制 API Key'
          hint='截图内容：创建成功弹框和复制按钮。'
        />
      </Step>
    </Steps>

    <Callout variant='warning'>
      API Key 只在创建完成时显示一次。请立即复制并妥善保管，不要泄露给他人。
    </Callout>
  </div>
)

// ── 六、FutureAI Image 下载与接入 ──
const imageAccess = (
  <div>
    <DocH2>下载安装</DocH2>
    <DocP>
      FutureAI Image 支持 Windows 和 macOS，任选一个网盘下载安装。
    </DocP>

    <div className='mt-4 grid gap-3 sm:grid-cols-2'>
      <NetdiskCard
        name='百度网盘'
        url='https://pan.baidu.com/s/1Z9wV66Uyh9KvdN2jPDo-5g?pwd=5wif'
        code='5wif'
      />
      <NetdiskCard
        name='夸克网盘'
        url='https://pan.quark.cn/s/7bf01c20435c?pwd=i4fL'
        code='i4fL'
      />
    </div>

    <DocP>
      链接已包含提取码；如未自动填入，请复制上方提取码。链接失效可
      <DocLink to='/docs/contact'>联系我们</DocLink>。
    </DocP>

    <DocH2>配置 API Key</DocH2>
    <Steps>
      <Step n={1} title='创建 API Key'>
        <p>
          还没有 API Key？按
          <DocLink to='/docs/api-key'>《创建 API Key》</DocLink>
          创建并复制。
        </p>
      </Step>
      <Step n={2} title='填入模型管理'>
        <p>打开模型管理页面，把 API Key 填入对应输入框。</p>
        <DocImage
          src={image('futureai-image-settings.png')}
          alt='FutureAI Image 模型管理页面'
          hint='截图内容：FutureAI Image 模型管理页面，包含 API Key 与 API 域名输入框。'
        />
      </Step>
      <Step n={3} title='填写 API 域名'>
        <p>
          API 域名默认为
          <DocStrong> https://futureaiapi.com</DocStrong>
          ，一般无需修改。
        </p>
        <CodeLine value='https://futureaiapi.com' />
      </Step>
    </Steps>
  </div>
)

// ── FutureAI 产品 ──
// Video、Film、无限画布暂未上线，先统一展示占位内容。
const video = <ComingSoon />
const film = <ComingSoon />
const canvas = <ComingSoon />

export const DOC_GROUPS: DocGroup[] = [
  {
    title: '使用指南',
    items: [
      {
        slug: 'account-credits',
        title: '账号积分',
        summary: '了解平台账号的获取方式，以及积分的来源与消耗规则。',
        icon: UserRound,
        content: accountCredits,
      },
      {
        slug: 'consulting',
        title: '咨询服务',
        summary: '咨询服务说明，以及从添加作者到获取账号与积分的完整流程。',
        icon: MessageCircle,
        content: consulting,
      },
      {
        slug: 'contact',
        title: '联系我们',
        summary: '通过 QQ 群、QQ 或微信联系作者。',
        icon: Contact,
        content: contact,
      },
      {
        slug: 'payment',
        title: '收款方式',
        summary: '咨询服务收款码及付款说明。',
        icon: Wallet,
        content: payment,
      },
      {
        slug: 'api-key',
        title: '创建 API Key',
        summary: '在控制台创建 API Key，用于调用 FutureAI API。',
        icon: KeyRound,
        content: apiKey,
      },
    ],
  },
  {
    title: 'FutureAI 产品',
    items: [
      {
        slug: 'image',
        title: 'FutureAI Image',
        summary: '下载安装 FutureAI Image，并把 API Key 和 API 域名填入客户端。',
        icon: ImageIcon,
        content: imageAccess,
      },
      {
        slug: 'video',
        title: 'FutureAI Video',
        summary: '即将上线，敬请期待。',
        icon: Video,
        content: video,
      },
      {
        slug: 'film',
        title: 'FutureAI Film',
        summary: '即将上线，敬请期待。',
        icon: Film,
        content: film,
      },
      {
        slug: 'canvas',
        title: 'FutureAI 无限画布',
        summary: '即将上线，敬请期待。',
        icon: InfinityIcon,
        content: canvas,
      },
    ],
  },
]

// 扁平列表：查找当前文档、上一篇 / 下一篇都按这个顺序走。
export const DOCS: DocEntry[] = DOC_GROUPS.flatMap((group) => group.items)
