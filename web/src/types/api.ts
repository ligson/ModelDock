export interface ModelManifest { id: string; name: string; source: string; format: string; task?: string[]; parameters?: string; quantization?: string; localPath?: string; available: boolean; managed: boolean; sizeBytes?: number; files: ModelFile[] }
export interface ModelFile { path: string; url?: string; size?: number; sha256?: string }
export interface Task { id: string; modelId: string; modelName?: string; status: string; bytesTotal: number; bytesDone: number; currentFile?: string; error?: string; speedBps?: number; etaSeconds?: number }
export interface TaskEvent { id: number; taskId: string; kind: string; message: string; createdAt: string }
export interface GPU { vendor: string; name: string; memoryGB?: number }
export interface Host { os: string; arch: string; cpu?: string; memoryGB?: number; gpus?: GPU[]; capabilities: string[]; runtimes: RuntimeInfo[] }
export interface RuntimeInfo { name: string; installed: boolean; version?: string; path?: string }
export interface Recommendation { model: ModelManifest; status: string; reason: string }
export interface CatalogItem { id: string; name: string; category: string; format: string; description: string; source: string }
export interface StorageRoot { id: string; path: string; name: string; default: boolean; available: boolean; totalBytes?: number; freeBytes?: number; modelCount?: number }
export interface RuntimePlan { adapter: string; label: string; status: string; reason: string; setup?: string[]; inputs?: string[]; outputs?: string[] }
export interface RuntimeDirectory { root: string; available: boolean; runtimes: Array<{ name: string; installed: boolean }> }
export interface RuntimeEngine { name: string; installed: boolean; path?: string; version?: string; mode: string; apiPort: number; source: string; managed?: boolean; healthy?: boolean; autoUpdate?: boolean; detail?: string }
export interface RuntimeEngines { engines: RuntimeEngine[] }
export interface OllamaModel { id: string; name: string; source: string; downloads?: number; tags?: string[] }
export interface OllamaStatus { baseURL: string; connected: boolean; models: OllamaModel[]; error?: string }
