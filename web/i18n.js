// Interface translations. English source text is the key, so untranslated
// strings fall back to English unchanged. Placeholders are written {name}.
(function (root) {
  "use strict";
  const zh = {
    // Page shell and navigation
    "TraceX — Agent workload profiler": "TraceX — Agent 负载剖析器",
    "TraceX home": "TraceX 首页",
    "AGENT WORKLOAD PROFILER": "AGENT 负载剖析器",
    "Workload explorer": "负载浏览",
    RUNS: "运行",
    "Select run": "选择运行",
    "Select active run": "选择当前运行",
    "THIS RUN": "本次运行",
    "Filter by task": "按任务筛选",
    "DATA SOURCES": "数据来源",
    "Local workspace": "本地工作区",
    "Evidence stays on this machine": "证据只保存在本机",
    "TraceX on GitHub": "GitHub 上的 TraceX",
    Workspace: "工作区",
    Connecting: "连接中",
    "Pause live": "暂停实时更新",
    "Resume live": "恢复实时更新",
    Refresh: "刷新",
    "↑ Import profile": "↑ 导入剖析文件",
    "EXPERIMENT RECORD": "实验记录",
    "Workload profile": "负载剖析",
    "Measured intervals, explicit dependencies and observed resource samples. Derived quantities and missing evidence are labelled.":
      "实测区间、显式依赖和资源采样。推导出的数值和缺失的证据都会标明。",
    "+ New experiment": "+ 新建实验",
    "↓ Export JSON": "↓ 导出 JSON",
    "Language": "语言",

    // Experiment panel
    "Run a real ARIES experiment": "运行真实的 ARIES 实验",
    "REAL ARIES EXECUTION": "真实 ARIES 运行",
    "New experiment": "新建实验",
    "Choose a configured workload and follow its live profile.": "选择一个已配置的负载，并实时查看它的剖析。",
    Close: "关闭",
    "Find a workload": "查找负载",
    "Workload, harness, model…": "负载、harness、模型…",
    "Profile preset": "配置预设",
    Concurrency: "并发数",
    "Workload tasks": "负载任务",
    "Select all": "全选",
    Clear: "清空",
    "I acknowledge real model calls may incur provider charges.": "我知道真实的模型调用可能产生服务商费用。",
    "Prepare data & images": "准备数据和镜像",
    "Start experiment": "开始实验",
    "Preparation runs ARIES setup without model calls. Runs use the credentials and endpoints in the selected profile.":
      "准备阶段只运行 ARIES setup，不调用模型。正式运行会使用所选配置中的凭据和端点。",
    "Experiment jobs": "实验任务",
    "Refresh jobs": "刷新任务",

    // Run summary
    "Run summary": "运行概览",
    "Loading workload…": "正在加载负载…",
    "Connecting to local source": "正在连接本地数据源",
    Loading: "加载中",
    "WALL TIME": "墙钟时间",
    "Run elapsed time": "运行总耗时",
    AGENTS: "AGENT 数",
    "Distinct observed agents": "观测到的不同 agent",
    "EXECUTION SPANS": "执行 SPAN",
    "Across all tasks": "所有任务合计",
    "MODEL / TOOL TIME": "模型 / 工具时间",
    "Interval union within each kind": "每类内部的区间并集",
    "Untitled workload": "未命名负载",
    "Imported profile": "导入的剖析文件",
    "Synthetic demo": "合成演示数据",
    "Recorded artifacts": "已记录的产物",
    "Demo dataset": "演示数据集",
    "Live source": "实时数据源",
    "Recorded evidence": "已记录的证据",
    "Across {count} task occurrences · full run": "共 {count} 个任务实例 · 整个运行",
    "Local import": "本地导入",
    "Updates paused": "已暂停更新",
    "Source error": "数据源错误",
    Snapshot: "快照",
    Reconnecting: "重新连接中",
    "Watching · rev {revision}": "监视中 · 第 {revision} 版",
    "Source update failed: {error}. The last valid view is retained.": "数据源更新失败：{error}。已保留上一次有效的视图。",
    "All task occurrences": "全部任务实例",
    "accuracy {score}": "准确率 {score}",
    "ended: {reason}": "结束原因：{reason}",
    "duration outlier": "耗时异常",
    "sparse monitor": "监控采样稀疏",

    // Analysis
    "Workload analysis": "负载分析",
    "P50 / P95 TASK LATENCY": "P50 / P95 任务延迟",
    "Completed task occurrences": "已完成的任务实例",
    "MEAN ACCURACY": "平均准确率",
    "Recorded score.json · {known}/{total} tasks": "来自 score.json · {known}/{total} 个任务",
    "No continuous scores recorded": "没有记录连续分数",
    "PEAK WORK CONCURRENCY": "峰值工作并发",
    "Nested envelopes excluded": "不计嵌套的外层 span",
    THROUGHPUT: "吞吐量",
    "{succeeded} succeeded · {failed} failed": "{succeeded} 个成功 · {failed} 个失败",
    "MODEL TOKENS": "模型 TOKEN",
    "Input / output · {known}/{calls} input usage known": "输入 / 输出 · {known}/{calls} 次调用有输入用量",
    "Where the work happens": "时间花在哪里",
    "Union time within each kind; categories can overlap.": "每类内部的区间并集时间；不同类别之间可能重叠。",
    "{duration} summed calls": "调用累计 {duration}",
    "{duration} summed calls · {count} incomplete or completion-only": "调用累计 {duration} · {count} 个不完整或只有完成时间",
    "Recorded critical path": "记录的关键路径",
    "Highlight path": "高亮路径",
    "Waiting for evidence": "等待证据",
    "Task latency and evidence coverage": "任务延迟和证据覆盖",
    Task: "任务",
    Harness: "Harness",
    "End reason": "结束原因",
    Evaluation: "评测",
    Accuracy: "准确率",
    Cleanup: "清理",
    Duration: "耗时",
    "Browser network": "浏览器网络",
    "Observed work": "已观测工作",
    Unattributed: "未归属",
    Unavailable: "不可用",
    "Timing unavailable": "无计时数据",
    "In progress": "进行中",
    "in progress": "进行中",
    duration: "耗时",
    "{count} req": "{count} 个请求",
    "{count} failed": "{count} 个失败",

    // Comparison
    "Compare runs": "对比运行",
    "Compare experiments": "对比实验",
    "Configuration, latency, usage, and coverage": "配置、延迟、用量和覆盖",
    Baseline: "基线",
    Candidate: "对比组",
    "Baseline run": "基线运行",
    "Candidate run": "对比组运行",
    Compare: "对比",
    Metric: "指标",
    Change: "变化",
    "Mean accuracy": "平均准确率",
    "Task P50": "任务 P50",
    "Task P95": "任务 P95",
    "Throughput / s": "吞吐量 / 秒",
    "Peak concurrency": "峰值并发",
    "Input tokens": "输入 token",
    "Output tokens": "输出 token",
    "No zero baseline": "基线为零",
    "Experiment conditions differ: {fields}. Changes are descriptive, not a controlled causal comparison.":
      "实验条件不同：{fields}。这些变化只是描述，不是受控的因果对比。",
    "Recorded configuration fields match. Unrecorded conditions and incomplete measurements still limit comparability.":
      "已记录的配置字段一致。未记录的条件和不完整的测量仍会影响可比性。",
    "Configuration metadata is unavailable. Experiment comparability is unknown.": "没有配置元数据，无法判断实验是否可比。",
    Condition: "条件",
    Unknown: "未知",
    "{label}: {known}/{total} tasks have usable timing; mean observed-work coverage {coverage}; token input known for {inputs}/{calls} model records.":
      "{label}：{known}/{total} 个任务有可用计时；已观测工作的平均覆盖率 {coverage}；{inputs}/{calls} 条模型记录有输入 token。",
    unavailable: "不可用",
    "Comparison failed: {error}": "对比失败：{error}",

    // Execution explorer
    "Execution explorer": "执行浏览",
    "Visualization layout": "可视化布局",
    "▦ Split view": "▦ 分屏视图",
    "◇ Agent DAG": "◇ Agent DAG",
    "≡ Timeline": "≡ 时间线",
    "Find a span or agent…": "查找 span 或 agent…",
    "Search spans and agents": "搜索 span 和 agent",
    "Filter span type": "按 span 类型筛选",
    "All span types": "全部 span 类型",
    Agent: "Agent",
    Model: "模型",
    Tool: "工具",
    Browser: "浏览器",
    "Explicit parent, spawn, call, return, dependency & join edges": "显式的父子、派生、调用、返回、依赖和汇合边",
    "TIME WINDOW": "时间窗口",
    From: "从",
    To: "到",
    "Time window start seconds": "时间窗口起点（秒）",
    "Time window end seconds": "时间窗口终点（秒）",
    Apply: "应用",
    "Full run": "整个运行",
    "Drag empty timeline space to select a window": "在时间线空白处拖动即可选择窗口",
    "Agent execution DAG": "Agent 执行 DAG",
    "Select a node to inspect": "选择一个节点查看详情",
    "Fit graph to viewport": "让图适配视口",
    "Fit graph": "适配视图",
    "Agent execution directed acyclic graph": "Agent 执行有向无环图",
    "No matching execution spans.": "没有匹配的执行 span。",
    "Linear timeline": "线性时间线",
    Zoom: "缩放",
    "Timeline zoom": "时间线缩放",
    Reset: "重置",
    "Linear execution timeline": "线性执行时间线",
    "Hover for timing · Select for evidence · Scroll horizontally when zoomed": "悬停查看计时 · 点击查看证据 · 放大后可横向滚动",
    "Page size": "每页数量",
    "Spans per page": "每页 span 数",
    "← Previous": "← 上一页",
    "Next →": "下一页 →",
    "{first}–{last} of {total} matching spans": "第 {first}–{last} 个，共 {total} 个匹配的 span",
    "No matching spans": "没有匹配的 span",
    "{count} relationships cross this page; inspect a node to follow neighbors.": "有 {count} 条关系跨出本页；查看节点可跟踪相邻 span。",
    "All relationships between visible spans are shown.": "可见 span 之间的关系已全部显示。",
    "{visible} visible edges · {hidden} cross this page": "{visible} 条可见边 · {hidden} 条跨出本页",
    "{count} recorded relationship": "{count} 条记录的关系",
    "{count} recorded relationships": "{count} 条记录的关系",
    "No recorded dependencies · isolated spans": "没有记录的依赖 · 孤立 span",
    "ROOT / UNLINKED": "根 / 未关联",
    "DEPTH {depth}": "深度 {depth}",
    Unassigned: "未分配",
    "Unassigned lane": "未分配泳道",
    "Run scope": "运行级",
    "Duration: {duration}": "耗时：{duration}",
    "{start} → {end} · {count} matching spans": "{start} → {end} · {count} 个匹配的 span",

    // Resources
    "Resource signals": "资源信号",
    "Utilization over run time": "运行期间的资源使用",
    "Resource runtime": "资源运行时",
    "All runtimes": "全部运行时",
    "Observed samples": "实测采样",
    "{returned} / {total} samples": "{returned} / {total} 个采样",
    "(a) CPU utilization": "(a) CPU 使用率",
    "(b) Memory usage": "(b) 内存用量",
    "(c) GPU utilization": "(c) GPU 使用率",
    "(d) Network I/O (rx + tx)": "(d) 网络 I/O（收 + 发）",
    "CPU utilization over time": "CPU 使用率随时间变化",
    "Memory usage over time": "内存用量随时间变化",
    "GPU utilization over time": "GPU 使用率随时间变化",
    "Container network throughput over time": "容器网络吞吐随时间变化",
    "Signals share the selected time window. Envelopes preserve observed peaks and gaps; device and shared infrastructure measurements remain separate from task attribution.":
      "各信号共用所选时间窗口。包络线保留实测峰值和空缺；设备和共享基础设施的测量不归属到任务。",
    "Not measured": "未测量",
    "No measurements available": "没有可用的测量",
    "{value} peak": "峰值 {value}",
    "{percent} of limit": "占上限 {percent}",

    // Coverage
    "Evidence coverage": "证据覆盖",
    "Synthetic demo. Agent relationships, timing, usage and resource signals are authored example data.":
      "合成演示数据。Agent 关系、计时、用量和资源信号都是人为编写的示例。",
    "Evidence coverage. Relationships require explicit references; overlapping timing does not establish causality.":
      "证据覆盖。关系必须有显式引用；时间上重叠不代表存在因果。",
    "Inspect {count} source coverage reports": "查看 {count} 份数据来源覆盖报告",
    "Evidence first · assumptions visible": "证据优先 · 假设可见",

    // Inspector
    "Span details": "Span 详情",
    "SPAN DETAILS": "SPAN 详情",
    "Close span details": "关闭 span 详情",
    "Span details unavailable: {error}": "无法获取 span 详情：{error}",
    "Not recorded": "未记录",
    Status: "状态",
    "Time quality": "计时质量",
    "Recorded interval": "已记录区间",
    Start: "开始",
    End: "结束",
    "Not yet recorded": "尚未记录",
    Source: "来源",
    Unspecified: "未指定",
    "Span ID": "Span ID",
    Runtime: "运行时",
    "Evidence flags": "证据标记",
    "Task browser network": "任务的浏览器网络",
    "Aggregate counters": "汇总计数",
    "{count} unsent": "{count} 个未发送",
    "{count} navigations": "{count} 次导航",
    "{duration} active": "活跃 {duration}",
    "Recorded relationships": "记录的关系",
    "Resources during this interval": "这段时间内的资源",
    "No samples recorded within this interval.": "这段时间内没有采样。",
    Memory: "内存",
    "{attribution} · {count} samples": "{attribution} · {count} 个采样",
    "{label}: {min} – {max} · mean {mean}": "{label}：{min} – {max} · 均值 {mean}",
    "Sample statistics describe concurrent runtime or task context, not resource cost caused by this span. Shared/device samples remain separate.":
      "采样统计描述的是同一时间的运行时或任务环境，而不是这个 span 造成的资源开销。共享和设备级采样单独列出。",
    "Recorded metadata": "记录的元数据",
    "Focus this time interval": "聚焦这段时间",

    // Notifications
    "Choose a non-negative time window with end after start.": "请选择非负的时间窗口，并且终点要晚于起点。",
    "Profile exceeds browser import limit of 32 MiB. Use --run for a larger source.": "剖析文件超过浏览器导入上限 32 MiB。较大的数据请用 --run 加载。",
    "Profile imported locally and available for comparison.": "剖析文件已在本地导入，可以用来对比。",
    "Import failed: {error}": "导入失败：{error}",
    "Complete profile exported, including spans outside the current page.": "已导出完整剖析，包括当前页以外的 span。",
    "Export failed: {error}": "导出失败：{error}",

    // Experiment panel (dynamic)
    "Request failed ({status})": "请求失败（{status}）",
    "I acknowledge this will run {count} selected task(s) using {target} and may incur provider charges.":
      "我知道这会用 {target} 运行所选的 {count} 个任务，并可能产生服务商费用。",
    "{selected} / {total} selected": "已选 {selected} / {total}",
    "Workload: {workload} · Harness: {harness} · Model: {model} · Backend: {backend}":
      "负载：{workload} · Harness：{harness} · 模型：{model} · 后端：{backend}",
    "configured model": "已配置的模型",
    Prepare: "准备",
    Run: "运行",
    "Process exit: {code}. ": "进程退出码：{code}。",
    "Setup status only; no benchmark evaluation.": "这里只有 setup 状态，没有 benchmark 评测。",
    "Cleanup pending.": "等待清理。",
    "Cleanup status not reported.": "没有报告清理状态。",
    "Stop request failed: {error}": "停止请求失败：{error}",
    "Could not refresh jobs: {error}": "无法刷新任务：{error}",
    "Cannot load ARIES runner configuration: {error}": "无法加载 ARIES 运行配置：{error}",
    "Preparation submitted. Follow the job below.": "已提交准备任务，请在下方查看进度。",
    "Experiment submitted. Follow the job below.": "已提交实验，请在下方查看进度。",
    "I acknowledge this will run {count} selected task(s) using {target} and may incur provider charges across repeated loop executions.":
      "我知道这会用 {target} 运行所选的 {count} 个任务，并可能因为循环重复执行而产生服务商费用。",
    "the configured model/provider": "已配置的模型和服务商",
    "No matching configured profiles.": "没有匹配的已配置预设。",
    "Loop duration: {duration}. ARIES preserves this profile’s repeated execution behavior; task counts are selections, not total calls.":
      "循环时长：{duration}。ARIES 会保留这个预设的重复执行方式；任务数指的是选中的任务，不是总调用次数。",
    "This profile has an arrival schedule. All declared tasks are required and the configured schedule is preserved.":
      "这个预设带有到达时间表。必须包含全部声明的任务，并保留配置的时间表。",
    "No experiments launched in this server session.": "本次服务运行期间还没有启动过实验。",
    "{count} tasks · concurrency {concurrency}": "{count} 个任务 · 并发 {concurrency}",
    "Benchmark evaluation is reported in the live profile.": "Benchmark 评测结果显示在实时剖析中。",
    "Harness: {status}. ": "Harness：{status}。",
    "Evaluation: {status}. ": "评测：{status}。",
    "Cleanup: {status}.": "清理：{status}。",
    "Open live profile": "打开实时剖析",
    "Waiting for ARIES run artifacts to become available…": "等待 ARIES 生成运行产物…",
    "Retry graceful stop": "重试正常停止",
    "Stop experiment": "停止实验",
    "Graceful cancellation requested; waiting for ARIES and cleanup.": "已请求正常取消，正在等待 ARIES 结束和清理。",
    "Execution target: {target}. Runs execute on this target, not inside the browser.": "执行目标：{target}。实验在这个目标上运行，不在浏览器里。",
    "not configured": "未配置",
    "machine {name}": "机器 {name}",
    "Runner ready. Select a configured workload.": "运行器已就绪，请选择一个已配置的负载。",
    "ARIES runner is unavailable. Configure the server before launching.": "ARIES 运行器不可用，请先配置服务端再启动。",
    "An experiment still owns the runner. Wait for completion or retry graceful stop before launching another.":
      "还有实验占用着运行器。请等它完成，或者重试正常停止，然后再启动新的实验。",
    "Submitting request. Please wait; do not retry an uncertain submission.": "正在提交请求，请稍候；结果不确定时不要重复提交。",
    "Submission could not be confirmed: {error}. Refresh jobs before submitting again; the request is not automatically retried.":
      "无法确认是否提交成功：{error}。请先刷新任务再决定是否重新提交；请求不会自动重试。",
    "Runner is not ready.": "运行器尚未就绪。",
    "Select a profile.": "请选择一个预设。",
    "Select at least one task.": "请至少选择一个任务。",
    "Concurrency must be a positive integer.": "并发数必须是正整数。",
    "Arrival schedules require all declared tasks.": "带到达时间表的预设必须包含全部声明的任务。",
    "Acknowledge real model calls before starting.": "开始前请确认会产生真实的模型调用。",
    "Experiment controls disabled. Restart TraceX with --aries-dir to enable ARIES execution.":
      "实验控制未启用。用 --aries-dir 重新启动 TraceX 才能运行 ARIES。",

    // Backend warnings and coverage reasons
    "A trailing partial evidence record is pending and will be retried on refresh.": "最后一条证据记录还不完整，刷新时会重试。",
    "ARIES reports a resource observation failure; retained samples provide partial coverage.": "ARIES 报告资源观测失败；已保存的采样只覆盖了一部分。",
    "An evidence file could not be fully read within streaming limits.": "有一个证据文件超出流式读取上限，没能完整读取。",
    "Authored synthetic events for exploring the interface.": "用于体验界面的人工合成事件。",
    "Browser network evidence is produced inside the task sandbox; URLs and request payloads are omitted.":
      "浏览器网络证据产生于任务 sandbox 内部；URL 和请求内容已省略。",
    "Coverage depends on retained harness and bridge telemetry; absent events are not zero-duration work.":
      "覆盖程度取决于保存下来的 harness 和 bridge 遥测；缺失的事件不等于耗时为零。",
    "Event edge pending unresolved endpoints.": "事件中的边还有端点未解析。",
    "Event parent pending an unresolved span reference.": "事件中的父 span 引用还未解析。",
    "Event resource pending an unresolved task reference.": "事件中的资源还有任务引用未解析。",
    "Event span pending an unresolved task reference.": "事件中的 span 还有任务引用未解析。",
    "Evidence record limit reached; profile coverage is partial.": "已达到证据记录上限；剖析只覆盖了一部分。",
    "Explicit synthetic parent links show concurrent researchers; no temporal inference.": "显式的合成父子链接展示了并发的研究 agent；没有按时间推断。",
    "Generated resource samples, not measured hardware.": "生成的资源采样，不是实测的硬件数据。",
    "Lifecycle phases require explicit start/end events; absent phase boundaries are unavailable.": "生命周期阶段需要显式的开始/结束事件；缺失的阶段边界无法显示。",
    "Live run snapshot: final task outcomes and phase durations may not yet be available.": "实时运行快照：最终的任务结果和阶段耗时可能还没有。",
    "Longest sum of recorded span durations along finish-before-start precedence; excludes unrecorded waits and containment.":
      "沿着“先完成后开始”的先后关系，记录的 span 耗时之和最长的路径；不含未记录的等待和包含关系。",
    "Malformed cyclic or unresolved parent links were omitted.": "已省略格式错误的循环或未解析的父子链接。",
    "Malformed cyclic phase dependencies were omitted.": "已省略格式错误的循环阶段依赖。",
    "Native evidence remains partial.": "原生证据仍不完整。",
    "Native session intervals, explicit tool-call IDs and model completion usage; missing model durations remain unavailable.":
      "原生会话区间、显式工具调用 ID 和模型完成用量；缺失的模型耗时仍不可用。",
    "No absolute timestamps available; only task durations are known.": "没有绝对时间戳，只知道任务耗时。",
    "No recorded finish-before-start dependency, join, or return relationships.": "没有记录“先完成后开始”的依赖、汇合或返回关系。",
    "Only explicit span parent relationships are shown; parallel timing does not imply causality.": "只显示显式的 span 父子关系；时间上并行不代表存在因果。",
    "Opaque trajectory data has no supported span timing contract; use bridge evidence or normalized spans.":
      "不透明的轨迹数据没有受支持的 span 计时约定；请使用 bridge 证据或规范化的 span。",
    "OpenClaw model completion markers retain token usage but do not imply a measured model duration.": "OpenClaw 的模型完成标记保留了 token 用量，但不代表测到了模型耗时。",
    "Recorded precedence contains overlapping or incomplete spans; a completion critical path is unavailable.": "记录的先后关系中有重叠或不完整的 span，无法得出关键路径。",
    "Recorded precedence is cyclic.": "记录的先后关系存在循环。",
    "Resource gaps exceed the points budget; select a narrower interval or increase points.": "资源空缺超出点数预算；请选择更窄的时间段或增加点数。",
    "Resource series exceed the points budget; select a runtime or narrower interval.": "资源序列超出点数预算；请选择一个运行时或更窄的时间段。",
    "Sandbox-produced request and page timings; source integrity is not independently verified.": "请求和页面计时由 sandbox 产生；数据来源的完整性未经独立验证。",
    "Shared infrastructure is kept separate from task resources.": "共享基础设施和任务资源分开统计。",
    "Some evidence could not be assigned to a unique task occurrence and was skipped.": "有些证据无法对应到唯一的任务实例，已跳过。",
    "Some phase dependencies are unresolved or incomplete; those edges are omitted.": "有些阶段依赖未解析或不完整，这些边已省略。",
    "Some span parents are unresolved or ambiguous; those edges are omitted.": "有些 span 的父 span 未解析或有歧义，这些边已省略。",
    "Some spans lack an explicit agent identity; task-level unassigned lanes do not identify measured agents.":
      "有些 span 没有显式的 agent 标识；任务级的“未分配”泳道不代表测到的 agent。",
    "Status reported by ARIES monitor index; raw errors are omitted.": "状态来自 ARIES monitor 索引；原始错误已省略。",
    "Task start timestamp unavailable; its timeline start is unknown and shown at zero.": "没有任务开始时间戳；它在时间线上的起点未知，显示为零。",
    "The first ARIES CPU sample per runtime is a counter baseline, not a measured zero; it is omitted from CPU values.":
      "每个运行时的第一个 ARIES CPU 采样是计数器基线，不是实测的零，因此不计入 CPU 数值。",
    "This dataset is entirely synthetic. Durations, agents, parent links, and resource samples are authored demonstration data.":
      "这个数据集完全是合成的。耗时、agent、父子链接和资源采样都是人为编写的演示数据。",
    "Timeline origin is the earliest retained task or observation timestamp; cross-process clock synchronization is not verified.":
      "时间线的起点是最早保存的任务或观测时间戳；跨进程的时钟同步未经验证。",
    "Unterminated final event is pending; retry after producer commits its newline.": "最后一个事件还没有结束符；等生产者写入换行后会重试。",

    // Recorded status values
    succeeded: "成功",
    failed: "失败",
    canceled: "已取消",
    completed: "已完成",
    running: "运行中",
    pending: "等待中",
    unknown: "未知",
    partial: "部分",
    confirmed: "已确认",
    unsupported: "不支持",
    not_enabled: "未启用",
    deadline_exceeded: "超时",
    starting: "启动中",
    stopping: "停止中",
    stop_failed: "停止失败",
  };
  const patterns = {
    "zh-CN": [
      [
        /^Task (\S+) ran (\S+) the run median duration with sparse monitor coverage \((\S+) of expected samples\); its resource profile is incomplete\.$/,
        "任务 $1 的耗时是本次运行中位数的 $2，且监控采样稀疏（只有预期的 $3），它的资源数据不完整。",
      ],
      [/^Skipped (\d+) invalid or unsupported evidence records\.$/, "已跳过 $1 条无效或不受支持的证据记录。"],
    ],
  };
  const dictionaries = { "zh-CN": zh };
  // Code, full name, and the short label on the header switch.
  const languages = [
    ["en", "English", "EN"],
    ["zh-CN", "简体中文", "中文"],
  ];
  const storageKey = "tracex.language";

  function detect() {
    try {
      const stored = root.localStorage?.getItem(storageKey);
      if (stored && languages.some(([code]) => code === stored)) return stored;
    } catch {}
    const preferred = root.navigator?.languages || [root.navigator?.language || ""];
    return preferred.some((l) => String(l).toLowerCase().startsWith("zh")) ? "zh-CN" : "en";
  }
  let language = detect();

  function fill(text, values) {
    return values ? text.replace(/\{(\w+)\}/g, (m, k) => (k in values ? String(values[k]) : m)) : text;
  }
  function t(text, values) {
    const source = String(text ?? "");
    const dictionary = dictionaries[language];
    if (dictionary) {
      if (Object.prototype.hasOwnProperty.call(dictionary, source)) return fill(dictionary[source], values);
      for (const [pattern, replacement] of patterns[language] || [])
        if (pattern.test(source)) return source.replace(pattern, replacement);
    }
    return fill(source, values);
  }
  function normalize(text) {
    return text.replace(/\s+/g, " ").trim();
  }
  // Translates static markup once: text nodes and labelling attributes whose
  // normalized English text is a dictionary key.
  function translateDOM(element) {
    if (!dictionaries[language] || !element) return;
    const walker = element.ownerDocument.createTreeWalker(element, 4);
    const nodes = [];
    while (walker.nextNode()) nodes.push(walker.currentNode);
    for (const node of nodes) {
      const key = normalize(node.nodeValue);
      if (!key || node.parentElement?.closest("script,style,pre")) continue;
      const translated = t(key);
      if (translated !== key) {
        const lead = /^\s*/.exec(node.nodeValue)[0],
          trail = /\s*$/.exec(node.nodeValue)[0];
        node.nodeValue = lead + translated + trail;
      }
    }
    for (const el of element.querySelectorAll("[placeholder],[aria-label],[title]"))
      for (const attribute of ["placeholder", "aria-label", "title"])
        if (el.hasAttribute(attribute)) el.setAttribute(attribute, t(normalize(el.getAttribute(attribute))));
  }
  function setLanguage(code) {
    try {
      root.localStorage?.setItem(storageKey, code);
    } catch {}
    root.location?.reload();
  }

  // use switches language in place, for tests; the page reloads instead.
  function use(code) {
    language = code;
  }

  const api = { t, translateDOM, setLanguage, use, languages, dictionaries, patterns, get language() { return language; } };
  root.TraceXI18n = api;
  if (typeof module !== "undefined") module.exports = api;
  if (root.document) {
    root.document.documentElement.lang = language;
    root.document.title = t(root.document.title);
    translateDOM(root.document.body);
  }
})(typeof globalThis !== "undefined" ? globalThis : this);
