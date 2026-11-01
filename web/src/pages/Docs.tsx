import { Fragment } from 'react'
import { Link, Navigate, useParams } from 'react-router-dom'
import { ArrowUpRight, BookOpen, ChevronLeft, ChevronRight } from 'lucide-react'
import { cn } from '../lib/utils'
import { Header } from '../components/Header'
import { DOC_GROUPS, DOCS } from '../docs/content'

export function Docs() {
  const { slug } = useParams<{ slug: string }>()

  // /docs 展示第一篇；未知章节跳回 /docs，避免出现空白内容区。
  const index = slug ? DOCS.findIndex((doc) => doc.slug === slug) : 0
  if (slug && index === -1) {
    return <Navigate to='/docs' replace />
  }

  const doc = DOCS[index]
  const prev = index > 0 ? DOCS[index - 1] : null
  const next = index < DOCS.length - 1 ? DOCS[index + 1] : null

  return (
    <div className='bg-background text-foreground relative min-h-svh overflow-x-clip'>
      <Header />

      <div className='mx-auto flex max-w-7xl gap-16 px-4 pt-24 pb-20 sm:px-6 lg:px-8'>
        {/* ── 左侧文档菜单 ── */}
        <aside className='hidden w-60 shrink-0 lg:block'>
          <div className='sticky top-24'>
            <div className='mb-4 flex items-center gap-2 px-3'>
              <BookOpen className='text-muted-foreground size-4' />
              <span className='text-sm font-semibold'>文档中心</span>
            </div>
            <div className='space-y-5'>
              {DOC_GROUPS.map((group, groupIndex) => (
                <div
                  key={group.title}
                  className={cn(
                    groupIndex > 0 && 'border-border/60 border-t pt-5'
                  )}
                >
                  <h3 className='text-muted-foreground mb-1.5 px-3 text-[11px] font-semibold tracking-wider'>
                    {group.title}
                  </h3>
                  <nav className='space-y-0.5'>
                    {group.items.map((entry) => {
                      const Icon = entry.icon
                      const isActive = entry.slug === doc.slug
                      return (
                        <Link
                          key={entry.slug}
                          to={`/docs/${entry.slug}`}
                          className={cn(
                            'flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                            isActive
                              ? 'bg-accent text-accent-foreground'
                              : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground'
                          )}
                        >
                          <Icon className='size-4 shrink-0' />
                          {entry.title}
                        </Link>
                      )
                    })}
                  </nav>
                </div>
              ))}
            </div>
            <div className='border-border/60 mt-6 border-t px-3 pt-4'>
              <p className='text-muted-foreground text-xs'>没找到需要的说明？</p>
              <Link
                to='/docs/contact'
                className='text-primary mt-1 inline-flex items-center gap-0.5 text-xs font-medium hover:opacity-80'
              >
                联系我们
                <ArrowUpRight className='size-3' />
              </Link>
            </div>
          </div>
        </aside>

        {/* ── 右侧内容 ── */}
        <main className='min-w-0 flex-1'>
          {/* 小屏：菜单收成横向滚动的胶囊 */}
          <nav className='-mx-4 mb-8 flex items-center gap-2 overflow-x-auto px-4 pb-1 [scrollbar-width:none] lg:hidden [&::-webkit-scrollbar]:hidden'>
            {DOC_GROUPS.map((group, groupIndex) => (
              <Fragment key={group.title}>
                {groupIndex > 0 ? (
                  <span className='bg-border/70 h-4 w-px shrink-0' />
                ) : null}
                {group.items.map((entry) => {
                  const isActive = entry.slug === doc.slug
                  return (
                    <Link
                      key={entry.slug}
                      to={`/docs/${entry.slug}`}
                      className={cn(
                        'shrink-0 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors',
                        isActive
                          ? 'bg-foreground text-background border-foreground'
                          : 'border-border/70 text-muted-foreground hover:text-foreground'
                      )}
                    >
                      {entry.title}
                    </Link>
                  )
                })}
              </Fragment>
            ))}
          </nav>

          <article className='max-w-4xl'>
            <header className='border-border/60 mb-8 border-b pb-6'>
              <p className='text-muted-foreground flex items-center gap-1 text-xs'>
                <BookOpen className='size-3.5' />
                文档中心
                <ChevronRight className='size-3' />
                {doc.title}
              </p>
              <h1 className='mt-3 text-2xl font-semibold tracking-tight sm:text-3xl'>
                {doc.title}
              </h1>
              <p className='text-muted-foreground mt-2 text-sm leading-6'>
                {doc.summary}
              </p>
            </header>

            {doc.content}

            {/* 上一篇 / 下一篇 */}
            <nav className='border-border/60 mt-12 grid gap-3 border-t pt-6 sm:grid-cols-2'>
              {prev ? (
                <Link
                  to={`/docs/${prev.slug}`}
                  className='border-border/70 hover:border-foreground/20 hover:bg-muted/30 group rounded-xl border px-4 py-3 transition-colors'
                >
                  <span className='text-muted-foreground flex items-center gap-1 text-[11px]'>
                    <ChevronLeft className='size-3' />
                    上一篇
                  </span>
                  <span className='mt-1 block text-sm font-medium'>
                    {prev.title}
                  </span>
                </Link>
              ) : (
                <div />
              )}
              {next ? (
                <Link
                  to={`/docs/${next.slug}`}
                  className='border-border/70 hover:border-foreground/20 hover:bg-muted/30 group rounded-xl border px-4 py-3 text-right transition-colors'
                >
                  <span className='text-muted-foreground flex items-center justify-end gap-1 text-[11px]'>
                    下一篇
                    <ChevronRight className='size-3' />
                  </span>
                  <span className='mt-1 block text-sm font-medium'>
                    {next.title}
                  </span>
                </Link>
              ) : null}
            </nav>
          </article>
        </main>
      </div>
    </div>
  )
}
