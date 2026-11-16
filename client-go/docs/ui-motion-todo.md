# React 客户端的 UI/UX 与动效：在 mygo 上复刻到什么程度

对照对象：`client/src/index.css`、`client/src/lib/motion.ts`、各组件里的 Tailwind 类与 Framer Motion 用法。
mygo 版本：v0.9.1（`go.mod`）。结论先行：**动效本身 mygo 完全接得住**（`Transition`、`AnimateTo`、弹簧 `Smooth/Snappy/Bouncy`、`CubicBezier`、`Enter`/`Exit` 的 `Motion{Collapse,Opacity,X,Y}`），**接不住的是"背景模糊"这一类合成效果**（`backdrop-filter`）。

曲线与时长已按 React 的 `motion.ts` 原值搬到 `internal/motion/motion.go`（`EaseInOutCubic`/`EaseOutCubic`、`Accordion 350ms`、`Popover 280/150ms`、`Tabs 250ms`、`HoverLift 300ms`、`EmptyState 350ms`、位移 2/6/8 px），并有 `internal/motion/motion_test.go` 钉住端点与单调性。

## 1. 已复刻

| React | 做法 | 这里 |
| --- | --- | --- |
| `Rail.tsx` 行 hover 上浮 | `hover:-translate-y-[2px] hover:shadow-[0_6px_14px_-6px_rgba(0,0,0,.45)]`，300 ms easeOutCubic | `internal/app/rail.go` 的 `railRow`：`AnimateTo("lift")` 驱动 `Translate(0,-2)`（300 ms easeOutCubic）；阴影即时切换——**`Transition` 不能挂在 `ui.List` 的行上**：行会先被画进图层再合成，实测整栏出现重复行与白色块（`docs/snapshots/rail-dark.png` 曾出现），所以行内的颜色/阴影过渡暂缓，见 §4 |
| `TurnRail.tsx` 折叠展开 | 350 ms easeInOutCubic 高度过渡 | `transcript.go` 的 `stepRow`：输出块带 `motion.AccordionTransition()`（`Size` + `Enter/Exit Collapse`）；`TestSnapshotStepExpanded` 存了折叠/半程/落定三帧，半程能看到卡片长到一半（无图层错位） |
| `empty-state.tsx` 空态 | 350 ms 淡入并上浮 8 px | `transcript.go` 空会话块、`railEmpty` 用 `motion.FadeIn()` |
| `bg-accent/10`、`hover:bg-*` 等颜色切换 | `transition-colors` 100–200 ms | **尚未挂**：`motion.Hover()` 已写好（颜色+阴影过渡），但行在 `ui.List` 里不能带 `Transition`（见上一行），剩下的按钮都在行内。需要时只给不在列表里的控件挂 |
| `shadow-sm/lg/2xl` | shadcn 阴影 token | `ui.Shadow(x,y,blur,spread,color)` 手写等效值 |
| `prefers-reduced-motion` | `@media` 关掉动画 | mygo 自带 `Preferences.ReduceMotion`：`Transition`/弹簧自动改为瞬时，`AnimateTo` 用 `Animation{NonMotion:true}` 保留淡入 |

## 2. 可以复刻，尚未做（TODO）

| 项 | React | mygo 手法 | 优先级 |
| --- | --- | --- | --- |
| Toast 进场/退场 | 右侧滑入 240 px / 350 ms，退场滑回 350 ms | `ui.Toast` 内容元素挂 `Transition{Enter:&Motion{X:240,Opacity:0}, Exit:&Motion{X:240,Opacity:0}}` | 中 |
| Toast 底部 1.8 s 排空条 | `::after` 宽度 100%→0 线性 | 一条 `ui.Box`，`AnimateTo("drain",0,…)` 或按 toast 剩余时间画宽度 | 低 |
| Popover/命令面板进场 | 280 ms、从锚点方向滑 6 px（`#055`） | `motion.PopoverEnter()` 已写好，挂到 `ui.Popover` 内容/`modelPalette` 上即可 | 中 |
| 标签内容切换 | 200 ms 上浮 8 px，延迟 100 ms | 面板内容挂 `motion.FadeIn()`，切换时给 `Transition.Delay` | 中 |
| 侧栏标签下划线滑动 | 250 ms，X 与宽度同时过渡 | 需要上一帧的 `Element.Bounds()` 量出当前标签与目标标签的框（mygo 没有 `Measure()`），再用 `AnimateTo("underlineX")` + `AnimateTo("underlineW")` | 中 |
| 运行中任务/步骤呼吸 | `animate-pulse` | 同色两个 `AnimateTo` 循环，或换成 `ui.Spinner` | 低 |
| Spinner 弧 | 243° 弧、1.3 转/秒（`#070`） | `ui.Spinner` 形状不同；可自绘 `Arc` 240° + `AnimateTo("turn")` 旋转，ease Linear | 低 |
| 行内操作按钮渐显 | `opacity-0 group-hover/row:opacity-100`，`transition` 150 ms | 可做（`Opacity` + `AnimateTo`），但本客户端**刻意常显**（`rail.go` 注释：隐藏操作的行没人会去置顶），需要时再改 | 低 |
| 面板打开的整体进场 | `animate-in fade-in slide-in-from-right-6` 200 ms | `motion.FadeIn()` 变体（`X:+24`） | 低 |

## 3. 只能降级（背景模糊一条已由 `plugins/glass` 解决，见 §1）

| 项 | React | 这里 |
| --- | --- | --- |
| 毛玻璃（`.glass`） | `backdrop-filter: blur(22px) saturate(180%)`，用于 Popover、ChatMenu、Toast、错误胶囊、跳转条 | **已做**：mygo 自带 `plugins/glass`，是 macOS 26 的 Liquid Glass 材质（背景结霜、边缘折射、细边高光），`internal/app/glass.go` 的 `panelGlass()`/`pillGlass()` 挂在会话菜单、Toast、错误胶囊、跳转条上。像素级验证见 `TestGlassSamplesWhatIsUnderIt` |
| 饱和度补偿 | `saturate(180%)` | 无等价 API；`Effect` 着色器里可做，随上面一起 |
| 颜色插值空间 | 浏览器 sRGB 过渡 | mygo `Animation.Oklab` 可在 Oklab 混合（更接近 shadcn 的观感），默认 sRGB；需要时对 `Transition` 打开 |
| Radix 的 `data-state` 开/关动画 | `animate-in`/`animate-out` 工具类 | mygo 用 `Transition` + `Enter/Exit Motion` 表达，等价但需要逐个挂 |
| 弹簧手感 | Framer `transition={{ type: "spring" }}` | mygo `Smooth/Snappy/Bouncy` 三档弹簧，参数不可逐项调（`SpringOptions{Duration,Bounce}` 可调） |

## 4. 明确不做 / 需要拍板

- **`ui.List` 行上的 `Transition`**（颜色/阴影/位移的自动过渡）：mygo 会把带 `Transition` 的元素画进离屏图层，行在虚拟列表里合成时出现重复行与白色块。暂时用 `AnimateTo` 手动驱动位移，颜色与阴影即时切换。要恢复平滑过渡，得先确认 mygo 侧图层裁剪行为（属于库的行为，不是我们这层能绕过的）。
- **`glass.Glass{Native: true}`**：走 macOS 26 的 `NSGlassEffectView`，但它采样的是窗口材质或 `ui.WebView` 的页面，不采样同一帧里画出的元素；我们的浮层下面就是自己画的 transcript，所以只能用着色器版（`glass.Glass{}`），观感是"像"而非系统材质。
- **面板合并（`glass.Group`）**：macOS 的 `NSGlassEffectContainerView` 让相近的玻璃片融成一片；我们的浮层彼此不相邻，暂不需要。
- **Radix 的 popover/dialog 遮罩渐变**：mygo `ui.Modal` 自带 `RGBA(0,0,0,0.4)` 遮罩与投影（`ui/widgets.go:689`），不再做二次动画。

## 5. 结论

动效层面 mygo 与 React 一一对应，剩下的是"逐处挂 `Transition`"的体力活（§2 共 8 项，其中 3 项已做）。`backdrop-filter` 这一条不再是缺口：mygo 的 `plugins/glass` 直接把 Liquid Glass 画出来，`internal/app/glass.go` 已挂在浮层上，`TestGlassSamplesWhatIsUnderIt` 钉住"背景真的透过来了"。唯一保留的差异是 `glass.Native`（系统 `NSGlassEffectView`）采样不到同帧元素，见 §4。
