# ModelDock

ModelDock（中文名：模型舱）是一个跨平台本地 AI 模型管理中心。

它不只负责下载模型，还负责判断当前机器适合哪个模型版本、使用什么运行时，以及下载完成后模型是否真的可用。

## 当前阶段

当前处于设计和项目初始化阶段，产品设计与技术决策见：

- [docs/产品设计方案.md](docs/产品设计方案.md)
- [docs/当前上下文.md](docs/当前上下文.md)

当前已提供可运行的 Go MVP：`go run ./cmd/modeldock serve`，默认监听 `127.0.0.1:8787`，数据保存在 `~/ModelDock`。Web 页面支持 Hugging Face 搜索、manifest 导入、模型下载、任务暂停和本机能力查看；CLI 支持 `modeldock search` 和 `modeldock info`。

前端工程位于 `web/`，使用 Vue 3、TypeScript、Vite 和 Ant Design Vue。页面按 `web/src/pages/<菜单>/` 拆分，公共组件、API 服务和类型分别位于 `components/`、`services/` 和 `types/`；执行 `make build` 会先类型检查并构建前端，再将产物嵌入 Go 服务。当前服务模式已使用这套嵌入页面，Wails 原生桌面壳仍在后续接入。

## 计划能力

- Windows、Linux、macOS
- Hugging Face、ModelScope、Ollama、本地目录
- Go 原生下载、断点续传、代理、分片、重试和完整性校验
- Apple Silicon/MLX、NVIDIA/CUDA、AMD/ROCm、Intel/DirectML、CPU 能力识别
- MLX、Ollama、llama.cpp、ComfyUI 运行时适配
- Wails 桌面窗口、浏览器服务模式和 CLI
- 多模型存储目录、迁移、去重和离线目录管理
