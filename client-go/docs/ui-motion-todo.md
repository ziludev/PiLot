# React 客户端的 UI/UX 与动效：在 mygo 上复刻到什么程度

对照对象：被删除的 React 客户端的 `index.css`、`lib/motion.ts`、各组件里的 Tailwind 类与 Framer Motion 用法（记录留档，见 `docs/history.md`）。
mygo 版本：v0.10.0（`go.mod`，见 `docs/mygo-version.md` §5）。结论先行：**动效本身 mygo 完全接得住**（`Transition`、`AnimateTo`、弹簧 `Smooth/Snappy/Bouncy`、`CubicBezier`、`Enter`/`Exit` 的 `Motion{Collapse,Opacity,X,Y}`），**接不住的是"背景模糊"这一类合成效果**（`backdrop-filter`）。

曲线与时长已按 React 的 `motion.ts` 原值搬到 `internal/motion/motion.go`（`EaseInOutCubic`/`EaseOutCubic`、`Accordion 350ms`、`Popover 280/150ms`、`Tabs 250ms`、`HoverLift 300ms`、`EmptyState 350ms`、位移 2/6/8 px），并有 `internal/motion/motion_test.go` 钉住端点与单调性。

## 1. 已复刻

| React | 做法 | 这里 |
| --- | --- | --- |
| `Rail.tsx` 行 hover 上浮 | `hover:-translate-y-[2px] hover:shadow-[0_6px_14px_-6px_rgba(0,0,0,.45)]`，300 ms easeOutCubic | `internal/app/rail.go` 的 `railRow`：`AnimateTo("lift")` 驱动 `Translate(0,-2)`（300 ms easeOutCubic），颜色与阴影交给行上的 `Transition(motion.Hover())`（v0.9.1 挂在 `ui.List` 行上会画出重复行与白色块，v0.10.0 修掉了，见 `docs/mygo-version.md` §5） |
| `TurnRail.tsx` 折叠展开 | 350 ms easeInOutCubic 高度过渡 | `transcript.go` 的 `stepRow`：输出块带 `motion.AccordionTransition()`（`Size` + `Enter/Exit Collapse`）；`TestSnapshotStepExpanded` 存了折叠/半程/落定三帧，半程能看到卡片长到一半（无图层错位） |
| `empty-state.tsx` 空态 | 350 ms 淡入并上浮 8 px | `transcript.go` 空会话块、`railEmpty` 用 `motion.FadeIn()` |
| `bg-accent/10`、`hover:bg-*` 等颜色切换 | `transition-colors` 100–200 ms | 会话行在 v0.10.0 后挂上 `motion.Hover()`（见上一行）；图标按钮、消息操作按钮、命中回合边框同样用它 |
| `shadow-sm/lg/2xl` | shadcn 阴影 token | `ui.Shadow(x,y,blur,spread,color)` 手写等效值 |
| `prefers-reduced-motion` | `@media` 关掉动画 | mygo 自带 `Preferences.ReduceMotion`：`Transition`/弹簧自动改为瞬时，`AnimateTo` 用 `Animation{NonMotion:true}` 保留淡入 |

## 1.1 2026-11 新增（第 34 轮）

- 模型面板 `Enter Y:+6, Opacity:0` 280ms（须带 `Key`，否则 mygo 认为元素没有前后身份，不播 Enter）。
- 右栏切标签时内容 `FadeIn`（`Key(sideTab)` 换身份）。
- 审批条 `Enter Y:+6` 滑入。
- 跳到底部条 `FadeIn`。
- 会话行 `Key(conversationID)`：List 复用行导致的上浮串行问题。
- 图标按钮 / 消息操作按钮 / 命中回合边框用 `motion.Hover()` 做颜色过渡。
- 不做的：查找条不加过渡（进入动画移动输入框时首帧按键会丢）。

### 第 35 轮

- **流式光标**：`turn.State` 为 `StateStreaming` 时，在流式卡片末尾画 6×13 的方光标，`Loop("caret", 1060ms, ui.Linear) < 0.5` 决定明灭（`Loop` 会持续请求帧，所以不需要额外定时器）。测试 `TestStreamingCaretBlinks` 只数光标所在窄带里的主色像素，避开上方 Spinner 的干扰。
- **`⌘↑`/`⌘↓` 改滑动**：`ScrollTo(180ms, EaseOut)` + `pageGliding` 标志；`followScroll` 在 glide 期间直接 return（否则会在下一帧把位置弹回底部），落地后再决定是否继续跟随。`ScrollState.Scrolling()` 是判据。
- **会话行的退出动画仍不做**：`ui.List` 复用行元素，`Transition` 挂在行上会出现重复行与白块（第 25 轮踩过），归档消失保持硬切。

## 2. 逐项状态（第 36 轮清完）

§2 原来的 8 项在 `docs/client-go-motion-rest.md` 的设计基础上逐项落地，一项一个 commit。表格里的"帧"都是快照测试落在 `/tmp/pitta-motion-*.png` 的文件名（不进仓库，看完即弃）。

| 项 | 状态 | 做法与证据 |
| --- | --- | --- |
| Toast 进场/退场 | **done** | `glass.go` 的 `toasts()` 给 `parts.Root` 挂 `motion.ToastEnter()`：`Enter/Exit = {X:240, Opacity:0}`，各 350 ms（进 easeOutCubic、出 easeInOutCubic）。**不给 `Root` 加 `Key`**——`coreToastBase` 已挂 `ts.key`，覆盖会毁掉 exit 的 ghost 匹配与多 toast 身份。帧：`toast-in-0/in-mid/end/out-0/out-mid/gone`；断言：进入半程比落定位置偏右 >40 px、退场半程同样偏右、exit 结束后玻璃带里剩不到 40 个非背景像素（没有 ghost 残留）。玻璃材质 + `Transition` 的组合没有白块、没有重影（逐帧看过） |
| Toast 底部 1.8 s 排空条 | **done** | `drainBar()`：底边 2 px 的绝对定位条，`ui.Row` 的 `WidthPercent(left/ToastLife*100)`。**不用 App 侧记账**：`ui.ToastParts.Left()` 是导出 API（`api_gen.go:8895`，类型断言即可用），它自己在视口 hover 时停表，与 toast 计时同源，所以不需要"记创建时刻 + 自己冻结"。帧：`toast-bar-early/bar-late/bar-after`；断言：0.06 s 时 ≥40 px、0.96 s 时 < 早期宽度的 65%、存活结束后 <8 px。副本 toast 存活随之从默认 4 s 降到 `motion.ToastLife`(1.8 s) |
| Popover/命令面板进场 | **已在用** | `motion.PopoverEnter()` 挂在审批条（`composer.go:85`）与模型面板（`modelpicker.go:119`）上，280/150 ms，无需再动 |
| 运行中步骤呼吸 | **done** | `stepRow` 前的小圆点：`dot.Loop("pulse", motion.Pulse=1400ms, ui.Bounce(ui.EaseInOut))` 在同色两档（alpha 0.35↔1）间呼吸。用 `Loop` 不用 `AnimateTo`：`AnimateTo` 只会收敛到目标、不循环，`Loop` 自带持续请求帧。帧：`step-a/step-b`；断言：半个周期前后圆点亮度差 ≥20，且已完成的步骤两帧完全一致（不让时间轴整体闪）。把 alpha 写死为 1 后测试确实会红（做过变异验证） |
| 标签内容切换 / 面板整体进场 | **done（改成 X 轴）** | 内容列 `Key(sideTab)` + `motion.PanelEnter()`：`Enter = {X:24, Opacity:0}`、200 ms（原来是 `FadeIn()` 的 Y:+8）。`TestSidePanelTabSlidesIn`（原 `TestSidePanelTabRises`，断言改 X）。帧：`panel-in/panel-settled`。**"面板打开"那一层不播，理由见下** |
| 侧栏标签下划线滑动 | **done（待拍板是否保留）** | 标签条移出被 `Key(sideTab)` 的那列（否则切 tab 整棵树重建、`AnimateTo` 状态清零，永远不滑）。指示线是流式占位：`spacer.AnimateTo("tabX")` + `bar.AnimateTo("tabW")`，两个目标都取上一帧 `Bounds()`，`motion.TabSlide`(250 ms easeInOutCubic)。**不能用 Absolute**：`ui/paint.go` 里 Absolute 子元素画在流式兄弟之上，会盖住标签文字。条的第一帧没有布局、直接跳过作画，避免从最左滑进来。帧：`tabs-start/tabs-mid/tabs-end`；断言：落定位置压在新标签下、半程位置严格介于两端之间（跳动会立刻红） |
| Spinner 弧 | **不做** | 全仓没有 `ui.Spinner`：思考中的指示是有意画的品牌 mark（`thinking.go`），`#070` 的 243° 弧没有可替换的对象。自绘一条弧只会多一个没人用的组件 |
| 行内操作按钮渐显 | **仍不做** | 本客户端刻意常显（见 §1）。同前 |

### 面板"打开"时为什么不播进场

`ui/transition.go` 的 `appears` 要求父节点早于本帧存在（`parent.st.born != rt.frame`）。右栏打开时 `ui.Split` 与面板列在同一帧出生，条件不成立，`Enter` 一定不播；要让它播就得把 Split 改成常驻，代价是分隔条与拖拽手柄常留在右边缘。所以第 5 项落在"切 tab 的内容列"上——那是真正"整块换掉"的地方。打开时仍是硬切。

### 第 36 轮顺带修掉的

- `glass.go` 的 toast 一直只画玻璃壳、没有标题（`ToastBase` 只给 status box，文字要自己加），`Copied` 提示此前是空胶囊。现在补上 `toast.Title`，也是快照能定位 toast 的前提。
- 上述每个动效都有对应的 `go test` 断言 + 中间帧/落定帧快照，`go build ./... && go vet ./... && go test ./internal/...` 全绿，`go tool mygo build -skip-dmg` 出包。

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
- **toast 存活 4 s → 1.8 s，侧栏下划线是新视觉元素**（第 36 轮）：为了排空条与存活同步，`copied` 提示改成 `motion.ToastLife`(1.8 s)；侧栏标签本来只有"选中态填充 pill"、React 端也没有下划线，这次按 TODO 加了 2 px 指示线。两件都容易回退——前者改回 `c.Toast(...)` 的默认 4 s，后者删掉 `sideTabs` 里的下划线那一行即可——但都改了观感，需要拍板。
- **Radix 的 popover/dialog 遮罩渐变**：mygo `ui.Modal` 自带 `RGBA(0,0,0,0.4)` 遮罩与投影（`ui/widgets.go:689`），不再做二次动画。

## 5. 结论

动效层面 mygo 与 React 一一对应，剩下的是"逐处挂 `Transition`"的体力活（§2 共 8 项，除 Spinner 弧与行内按钮渐显外都已落地，理由见该节）。`backdrop-filter` 这一条不再是缺口：mygo 的 `plugins/glass` 直接把 Liquid Glass 画出来，`internal/app/glass.go` 已挂在浮层上，`TestGlassSamplesWhatIsUnderIt` 钉住"背景真的透过来了"。唯一保留的差异是 `glass.Native`（系统 `NSGlassEffectView`）采样不到同帧元素，见 §4。
