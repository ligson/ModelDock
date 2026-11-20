# 开发规则

## 技术栈

- 后端：Go。
- 数据库：SQLite，优先使用纯 Go 驱动。
- 桌面：Wails + Vue3。
- 前端：Vue3、Vite、TypeScript、Ant Design Vue。
- 构建：Makefile；前端构建产物通过 `go:embed` 嵌入 Go 程序。

## 前端目录与页面规范

- 前端统一使用 Vue 3 + TypeScript + Vite + Ant Design Vue；桌面模式由 Wails 承载同一套前端构建产物，服务模式由 Go HTTP 服务嵌入并提供。
- `web/src/pages/` 按菜单组织页面，每个菜单页面使用独立目录和 `index.vue`，页面只编排页面状态与组件，不直接拼接 HTML 字符串。
- `web/src/components/` 只放可复用的业务组件或基础组件；跨页面的模型卡片、任务卡片、布局、状态标签必须复用，不在页面中复制实现。
- `web/src/services/` 统一封装 HTTP API，`web/src/types/` 统一维护接口类型，页面不得散落 `fetch` 和重复的 JSON 类型定义。
- 普通 HTTP JSON 接口必须统一返回：`{"success":true,"data":{},"errorMsg":"","error":""}`。成功时 `success` 为 `true` 且 `errorMsg`、`error` 为空；失败时 `success` 为 `false`、`data` 通常为 `null`，`errorMsg` 是中文用户提示，`error` 是稳定的机器可读或诊断文本。HTTP 状态码仍必须正确表达参数、认证、权限和服务端错误。前端请求只能通过 API 服务层统一解包 envelope；文件、图片、视频等二进制响应不套 JSON envelope。
- `web/src/utils/` 放格式化、状态映射等纯函数；全局样式集中在 `web/src/styles.css`，页面只保留必要的局部样式。
- `web/dist` 和 `node_modules` 不提交；`make build` 必须先完成前端类型检查和构建，再编译 Go 嵌入产物。

## 代码规范

- 核心业务代码保持跨平台，不在业务层散落操作系统判断。
- Windows、Linux、macOS 差异通过平台适配层隔离。
- 文件操作、下载、进程启动和运行时探测都必须有可诊断日志。
- 所有长任务支持取消、恢复和失败重试。
- 不把模型权重、token、密码、Cookie 或代理密码提交到 Git。
- 本地开发服务统一通过开发脚本管理：macOS/Linux 使用 `scripts/modeldock-dev.sh start|stop|restart|status`，Windows 使用 `powershell -ExecutionPolicy Bypass -File scripts/modeldock-dev.ps1 -Action start|stop|restart|status`。两套脚本都使用 `.modeldock-dev` 保存 PID、日志和临时二进制，该目录不得提交。

## 模型管理规则

- 模型必须有来源、revision、文件清单、许可证和兼容性信息。
- 下载完成不能只检查目录存在，必须校验必要文件和文件大小/哈希。
- 量化版本按独立候选项展示，不能把 FP16、INT8、Q4 等混为一个模型。
- 推荐结果必须说明原因，并区分推荐、可运行、不建议和不可用。
- 模型文件默认存放在用户选择的存储根目录，不写死个人路径。

## UI/UX 要求

- 适配明暗模式和 Windows/macOS/Linux 常见窗口尺寸。
- 所有用户可见的按钮、确认框、输入对话框、提示、下拉菜单和反馈组件统一使用 Ant Design Vue；禁止在业务页面使用浏览器原生 `alert`、`confirm` 或 `prompt`，组件必须跟随当前主题变量。
- 下载状态必须能区分正常、停滞、失败、等待代理和校验中。
- 对几十 GB 模型显示预计磁盘占用、可用空间和风险确认。
- 重要操作提供可见的日志、取消、继续、重试和迁移入口。

## Git 规则

- 每次实际改动记录到根目录 `CHANGELOG.md`。
- 设计决策和稳定经验沉淀到 `docs/`。
- 不提交模型权重、下载缓存、运行日志和本地数据库。

## 首版实现约定

- 用户可见的产品和命令名称统一为 `ModelDock` / `modeldock`，不再使用 `modelhub` 作为 CLI 名称。
- 首版只面向公开模型来源；来源适配器必须保留扩展点，新增来源不能修改下载任务核心流程。
- 模型候选项的显示名称统一包含模型名、量化版本和参数规模；不同量化版本必须使用不同候选 ID。
- 首版下载任务需要具备 Range 断点续传、可配置并发、代理、重试、stalled 检测、取消/恢复和 SHA-256 校验能力。
- 首版跨平台目标为 Windows、Linux、macOS；平台差异必须通过适配层或构建标签隔离。
- 首版删除采用物理删除，但必须先检查模型引用和运行实例，并记录可诊断日志。
- 首版暂不实现账号认证、私有源、模型 revision 锁定和自动性能 benchmark；更新公开源信息时允许刷新到最新数据。
