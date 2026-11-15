# ModelDock 项目协作说明

## 项目定位

ModelDock 是一个跨平台本地模型管理中心，目标平台为 Windows、Linux 和 macOS。

核心目标：

- 统一发现、下载、校验、存储和迁移 AI 模型。
- 根据当前机器的 CPU、内存、GPU、显存、驱动和运行时推荐模型版本。
- 统一管理 MLX、Ollama、llama.cpp、ComfyUI 等运行时。
- 桌面模式使用 Wails + Vue3；服务模式使用 Go HTTP API；最终前端嵌入 Go 程序。

## 必读文档

- [产品设计方案](docs/产品设计方案.md)
- [当前上下文](docs/当前上下文.md)
- [开发规则](docs/development_rules.md)
- [更新日志](CHANGELOG.md)

## 重要约定

- 后端优先使用 Go，核心功能不依赖 Python、Node.js、Docker 或 aria2。
- SQLite 使用纯 Go 驱动，保证跨平台单文件发布。
- 模型文件不提交到 Git；只提交清单、配置、代码和文档。
- 修改项目文件后同步更新 `CHANGELOG.md`。
- 重要设计决策必须写入 `docs/`，不能只留在聊天上下文里。
- 默认本地服务只监听 `127.0.0.1`，局域网访问必须显式开启并有认证保护。

## 入口

后续开发从 `docs/产品设计方案.md` 和 `docs/当前上下文.md` 开始，不要重新假设项目目标。
