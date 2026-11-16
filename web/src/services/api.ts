import type { CatalogItem, Host, ModelManifest, OllamaStatus, Recommendation, RuntimeDirectory, RuntimePlan, StorageRoot, Task } from '../types/api'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  if (response.status === 204) return undefined as T
  const body = await response.json().catch(() => null) as { success?: boolean; data?: T; errorMsg?: string; error?: string } | null
  if (!response.ok || !body?.success) throw new Error(body?.errorMsg || body?.error || `请求失败（${response.status}）`)
  return body.data as T
}

export const api = {
  host: () => request<Host>('/api/host'),
  models: () => request<ModelManifest[]>('/api/models'),
  tasks: () => request<Task[]>('/api/tasks'),
  recommend: () => request<Recommendation[]>('/api/recommend'),
  catalog: () => request<CatalogItem[]>('/api/catalog'),
  storage: () => request<{ root: string; roots: StorageRoot[] }>('/api/storage'),
  search: (source: string, query: string) => request<Array<{ id: string; name: string; source: string; downloads?: number }>>(`/api/search?source=${encodeURIComponent(source)}&q=${encodeURIComponent(query)}`),
  manifest: (source: string, id: string) => request<ModelManifest>('/api/source/manifest', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ source, id }) }),
  runtimePlan: (modelId: string) => request<RuntimePlan>('/api/runtime/plan', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ modelId }) }),
  runtime: () => request<RuntimeDirectory>('/api/runtime'),
  saveRuntime: (root: string) => request<RuntimeDirectory>('/api/runtime', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ root }) }),
  ollama: () => request<OllamaStatus>('/api/ollama'),
  saveOllama: (baseURL: string) => request<OllamaStatus>('/api/ollama', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ baseURL }) }),
  importOllama: (name: string) => request<ModelManifest>('/api/ollama/import', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }) }),
  generate: (modelId: string, prompt: string, imagePath = '', options?: { size?: string; steps?: number; guidance?: number; seed?: number; promptExpansion?: boolean }, conversationId = '', messages: Array<{ role: string; content: string }> = []) => request<{ id: string; status: string; message?: string; conversationId?: string }>('/api/generate', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ modelId, prompt, imagePath, options, conversationId, messages }) }),
  generationJob: (id: string) => request<{ id: string; status: string; phase: string; conversationId?: string; message?: string; response?: string; artifactURL?: string; artifactName?: string; artifactBytes?: number; error?: string; startedAt: string; finishedAt?: string; steps: Array<{ key: string; label: string; status: string; detail?: string; startedAt: string; finishedAt?: string }> }>(`/api/generate/jobs/${encodeURIComponent(id)}`),
  upload: (file: File) => { const form = new FormData(); form.append('file', file); return request<{ path: string }>('/api/uploads', { method: 'POST', body: form }) },
  scan: () => request<unknown[]>('/api/local/scan-all'),
  manage: (manifest: ModelManifest) => request('/api/models', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...manifest, managed: true }) }),
  unmanage: (id: string) => request(`/api/local/unimport`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id }) }),
  testModel: (id: string) => request<{ status: string; detail: string; name: string }>(`/api/models/${encodeURIComponent(id)}/test`, { method: 'POST' }),
  taskAction: (id: string, action: string) => request(`/api/tasks/${encodeURIComponent(id)}/${action}`, { method: 'POST' }),
  setRoot: (root: string) => request('/api/storage', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ root }) }),
  addRoot: (path: string, name: string) => request('/api/storage/roots', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path, name }) }),
  removeRoot: (id: string) => request(`/api/storage/roots?id=${encodeURIComponent(id)}`, { method: 'DELETE' }),
  migrate: (from: string, to: string) => request<{ models: number }>('/api/storage/migrate', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ from, to }) }),
  deleteModel: (id: string, deleteFiles: boolean) => request(`/api/models/${encodeURIComponent(id)}${deleteFiles ? '?deleteFiles=true' : ''}`, { method: 'DELETE' }),
}
