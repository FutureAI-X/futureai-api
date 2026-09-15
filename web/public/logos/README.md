# 开发者 LOGO

模型广场的卡片与详情弹框会按模型的 `owner` 字段（如 `OpenAI`）在这里查一个 LOGO 来展示。

## 来源与许可

图标取自 [@lobehub/icons-static-svg](https://www.npmjs.com/package/@lobehub/icons-static-svg)，
MIT 许可。这个图标集专做 AI 品牌，覆盖对话/图像/视频/音乐各类厂商。

> 补充说明：Simple Icons 应商标方要求下架了 OpenAI，Wikimedia 在部分网络下不可达，
> 所以选了这个来源。**也不要改成运行时去外部图床取**——那会把访问者 IP 与厂商名
> 送给第三方，而且在网络受限的环境下会直接挂掉。

## 这些文件怎么被用

`web/public/` 下的内容会被 Vite 原样拷进 `web/dist/`，再由后端从嵌入文件系统里服务，
所以路径就是 `/logos/<名字>.svg`。对应关系写在 `web/src/lib/owner-logo.ts`。

图标本身是 `fill="currentColor"` 的单色路径，页面用 **CSS mask** 渲染（不是 `<img>`），
因此颜色跟随主题的 `muted-foreground`，亮色/暗色都正常。

## 新增一个品牌

1. 把 SVG 放进本目录，文件名用小写品牌名，例如 `anthropic.svg`
2. 在 `web/src/lib/owner-logo.ts` 的 `OWNER_LOGOS` 里加一行：
   ```ts
   anthropic: '/logos/anthropic.svg',
   ```
   键要写**归一化后**的形式：小写、去掉空格与 `. _ -`。
   例如 `xAI` → `xai`，`Google Gemini` → `googlegemini`。
3. 重新构建前端（`make web`）——`web/dist` 是编译期被嵌进后端的，不重建不会生效

匹配不到的厂商会回落成字母头像，所以没加图标也不会出错。

图标需要满足：单色、`fill="currentColor"`（或干脆不写 fill）、`viewBox` 从 0 0 开始。
