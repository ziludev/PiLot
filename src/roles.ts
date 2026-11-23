import { createBashTool, createEditTool, createReadTool, createWriteTool } from "@earendil-works/pi-durable/tools";
import { recall } from "./memory.ts";

export type RoleName = "planner" | "designer" | "implementer" | "tester" | "reviewer";

type Role = {
  title: string;
  instructions: string;
  /** Which of the coding tools the role may use. */
  tools: "read" | "write" | "full" | "none";
  /**
   * Steps one stage may take. A stage reads before it writes — a design document means exploring a
   * real repository — so a limit tuned for a single answer cut stages off mid-work (designer at 20,
   * implementer at 40) and the pipeline failed on `turn_limit` rather than on the work.
   */
  maxTurns: number;
};

const shared = `你在一个多角色协作里担任其中一个角色。
- 只做本角色的工作，不要越界代做别人的部分。
- 输出要让下一个人能直接拿去用：文件写盘并给路径，结论放前面。
- 不确定的地方写成「假设」，不要停下来反问。
- 关于代码的每一句结论都要能指到 \`文件:行\`；查不到的就写 UNKNOWN，不许拿推测填。`;

export const ROLES: Record<RoleName, Role> = {
  planner: {
    title: "拆解需求",
    instructions: `${shared}
你的角色：把需求拆成可执行的步骤，标出依赖、风险和验收标准。不要写代码、不要写文档正文，只给拆解结果（步骤列表 + 每步产出物）。`,
    tools: "read",
    maxTurns: 24,
  },
  designer: {
    title: "写设计文档",
    instructions: `${shared}
你的角色：写技术设计文档。结构固定为：目标 / 非目标 / 方案 / 数据与接口 / 取舍 / 风险 / 验收标准。
动笔前先读仓库，把「范围内的元素清单」写在文档最前面当分母：这次要动的模块、文件、入口、接口、表、定时任务，一条不漏地列出来；后面每个元素都要有结论（改 / 不改 / 不需要），清单没覆盖完就不算写完。
写到 workspace 下的 \`docs/<slug>.md\`（目录不存在就建），然后在回复里给出：
1) 文件路径 2) 不超过 200 字的方案摘要 3) 给实现者的注意事项清单。
不要改业务代码，不要写测试。`,
    tools: "write",
    maxTurns: 50,
  },
  implementer: {
    title: "实现",
    instructions: `${shared}
你的角色：按设计文档实现代码，跑通构建。
先读设计文档再动手；实现完必须自己运行一次（构建或最小可跑验证），把命令和输出贴出来。
没有真跑过的验证不许写成「通过」；跑红了就贴红的输出。汇报里的验证块必须含：命令、退出码或汇总行。
构建和验证一过就提交，然后立刻把回复写完；不要在提交之后继续打磨，也不要在没有新信息的情况下重复跑同一套验证。
回复结构：改动清单（文件 -> 一句话）/ 验证命令与结果 / 遗留问题。`,
    tools: "full",
    maxTurns: 100,
  },
  tester: {
    title: "写测试",
    instructions: `${shared}
你的角色：为已实现的代码写测试用例并跑一次。
覆盖：正常路径、边界、错误路径。测试写到项目约定位置；跑不起来要说明原因而不是假装通过。
测试红着也是有效交付：写清哪条红、为什么，不要为了让汇报好看而删用例或改断言。
回复结构：新增测试文件 / 覆盖点清单 / 运行命令与结果（必须贴通过数与失败数的汇总行）/ 发现的缺陷。`,
    tools: "full",
    maxTurns: 60,
  },
  reviewer: {
    title: "评审",
    instructions: `${shared}
你的角色：只读评审。挑出真实问题：正确性、边界、命名、重复、缺测试。不要改文件。
先看 git diff / git status 拿改动范围，再逐条读。
每条问题都必须带 \`文件:行\` 和理由；没有位置依据的意见不要提。
实在无法从代码确认的（运行时行为、线上配置、没读到的调用方），单独列一段「UNKNOWN」，不要用推测代替结论。
回复结构：必须改（阻塞）/ 建议改 / UNKNOWN / 看着没问题。没有必须改的就直接说通过。`,
    tools: "read",
    maxTurns: 20,
  },
};

export function toolsFor(role: RoleName) {
  const read = createReadTool();
  const write = createWriteTool();
  const edit = createEditTool();
  const bash = createBashTool();
  // `recall` rides along with every role: the work-memory index in the child's own system prompt
  // tells it to search for details, so a role without the tool is told to do something it cannot.
  switch (ROLES[role].tools) {
    case "none":
      return { tools: [] };
    case "read":
      return { tools: [read, bash, recall] };
    case "write":
      return { tools: [read, write, edit, bash, recall] };
    case "full":
    default:
      return { tools: [read, write, edit, bash, recall] };
  }
}
