<template>
  <div class="page runtime-page">
    <a-page-header title="运行时设置" sub-title="管理 ModelDock 使用的本地推理引擎" />
    <a-card title="llama.cpp" :loading="loading">
      <template #extra><a-button :loading="scanning" @click="scan"><ReloadOutlined />扫描 PATH</a-button></template>
      <a-alert v-if="engine && !engine.installed" type="warning" show-icon message="尚未发现 llama.cpp" description="请扫描 PATH，或填写 llama-server 可执行文件（也可填写包含它的目录）。" />
      <a-descriptions v-if="engine" bordered :column="1" size="small">
        <a-descriptions-item label="状态"><a-tag :color="engine.installed ? 'green' : 'default'">{{ engine.installed ? '可用' : '未发现' }}</a-tag></a-descriptions-item>
        <a-descriptions-item label="版本">{{ engine.version || '未读取' }}</a-descriptions-item>
        <a-descriptions-item label="路径"><span class="runtime-path">{{ engine.path || '未配置' }}</span></a-descriptions-item>
        <a-descriptions-item label="发现来源">{{ engine.source }}</a-descriptions-item>
      </a-descriptions>
      <a-divider />
      <a-form layout="vertical">
        <a-form-item label="llama.cpp 路径"><a-input v-model:value="path" placeholder="例如 C:\\tools\\llama.cpp\\llama-server.exe" /></a-form-item>
        <a-space wrap>
          <a-form-item label="运行模式"><a-segmented v-model:value="mode" :options="[{ label: 'CUDA/GPU', value: 'cuda' }, { label: 'CPU', value: 'cpu' }]" /></a-form-item>
          <a-form-item label="本地 API 端口"><a-input-number v-model:value="apiPort" :min="1" :max="65535" /></a-form-item>
        </a-space>
        <a-space wrap>
          <a-button type="primary" :loading="saving" @click="save">保存并检查</a-button>
          <a-button disabled>手动导入 ZIP</a-button>
          <a-button disabled>更新到最新版</a-button>
        </a-space>
      </a-form>
      <a-typography-text type="secondary">ModelDock 会优先使用这里保存的路径；未配置时自动检查 PATH 和运行时目录。MLX 仅支持 Apple Silicon macOS，Windows 和 Linux 会明确标记为不可用。导入 ZIP 与版本更新入口已预留，后续接入平台匹配的官方构建。</a-typography-text>
    </a-card>
    <a-card title="图像运行时（ComfyUI）" :loading="loading" style="margin-top: 16px">
      <template #extra><a-tag :color="comfyEngine?.healthy ? 'green' : comfyEngine?.installed ? 'orange' : 'default'">{{ comfyEngine?.healthy ? '服务已连接' : comfyEngine?.installed ? '已找到，服务未启动' : '未配置' }}</a-tag></template>
      <a-alert type="info" show-icon message="可使用已有 ComfyUI，也可以让 ModelDock 管理独立运行环境。" description="已有目录不会被自动覆盖；ModelDock 管理的目录与用户模型目录分开，便于更新和回滚。" />
      <a-alert v-if="comfyInstallError" type="error" show-icon closable :message="comfyInstallError" style="margin-top: 12px" />
      <a-descriptions v-if="comfyEngine" bordered :column="1" size="small" style="margin-top: 16px">
        <a-descriptions-item label="状态"><a-tag :color="comfyEngine.installed ? 'green' : 'default'">{{ comfyEngine.installed ? (comfyEngine.healthy ? '可用' : '已安装') : '未发现' }}</a-tag></a-descriptions-item>
        <a-descriptions-item label="目录或入口"><span class="runtime-path">{{ comfyEngine.path || '未配置' }}</span></a-descriptions-item>
        <a-descriptions-item label="说明">{{ comfyEngine.detail || comfyEngine.source }}</a-descriptions-item>
      </a-descriptions>
      <a-form layout="vertical" style="margin-top: 16px">
        <a-form-item label="ComfyUI 目录"><a-input v-model:value="comfyPath" placeholder="已有 ComfyUI 根目录，例如 D:\\ComfyUI" /></a-form-item>
        <a-space wrap>
          <a-form-item label="使用方式"><a-segmented v-model:value="comfyMode" :options="[{ label: '使用已有目录', value: 'external' }, { label: 'ModelDock 管理目录', value: 'managed' }]" @change="onComfyModeChange" /></a-form-item>
          <a-form-item label="API 端口"><a-input-number v-model:value="comfyPort" :min="1" :max="65535" /></a-form-item>
        </a-space>
        <a-checkbox v-model:checked="comfyAutoUpdate">检查并提示 ComfyUI 更新</a-checkbox>
        <div class="comfy-actions"><a-space wrap><a-button type="primary" :loading="saving" @click="saveComfy">保存并检查</a-button><a-button :loading="scanning" @click="scan">重新扫描</a-button><a-button v-if="comfyMode === 'managed'" :disabled="!comfyPath" :loading="installing" @click="installComfy">{{ comfyEngine?.installed ? '更新 ComfyUI' : '下载并安装 ComfyUI' }}</a-button><a-button v-if="comfyEngine?.installed && !comfyEngine?.healthy" type="primary" :loading="starting" @click="startComfy">启动并准备依赖</a-button><a-button @click="viewComfyLog">查看日志</a-button></a-space></div>
      </a-form>
      <a-typography-text type="secondary">保存后模型市场会根据 ComfyUI 是否可用判断图像模型；具体模型仍可能需要 ComfyUI-GGUF 节点、文本编码器、VAE 和对应工作流。</a-typography-text>
    </a-card>
    <a-modal v-model:open="logOpen" title="ComfyUI 运行日志" width="860px" :footer="null"><a-typography-text type="secondary">{{ logPath }}</a-typography-text><pre class="runtime-log">{{ logContent || '暂无日志内容' }}</pre></a-modal>
    <a-card title="其它运行时" :loading="loading" style="margin-top: 16px">
      <a-list bordered size="small" :data-source="otherEngines">
        <template #renderItem="{ item }"><a-list-item><a-list-item-meta :title="item.name" :description="item.path || item.source" /><template #actions><a-tag :color="item.installed ? 'green' : 'default'">{{ item.installed ? '可用' : '未发现' }}</a-tag><span>{{ item.version || '系统探测' }}</span></template></a-list-item></template>
      </a-list>
    </a-card>
    <OllamaSettings style="margin-top: 16px" />
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { api } from '../../services/api'
import type { RuntimeEngine } from '../../types/api'
import OllamaSettings from '../ollama/index.vue'

const engine = ref<RuntimeEngine>()
const engines = ref<RuntimeEngine[]>([])
const otherEngines = computed(() => engines.value.filter(item => item.name !== 'llama.cpp' && item.name !== 'comfyui'))
const comfyEngine = computed(() => engines.value.find(item => item.name === 'comfyui'))
const path = ref('')
const mode = ref('cuda')
const apiPort = ref(8686)
const loading = ref(false)
const saving = ref(false)
const scanning = ref(false)
const comfyPath = ref('')
const comfyMode = ref('external')
const comfyPort = ref(8188)
const comfyAutoUpdate = ref(false)
const installing = ref(false)
const starting = ref(false)
const comfyInstallError = ref('')
const logOpen = ref(false)
const logPath = ref('')
const logContent = ref('')

async function load() {
  loading.value = true
  try { const [result, runtimeDirectory] = await Promise.all([api.runtimeEngines(), api.runtime()]); engines.value = result.engines; engine.value = result.engines.find(item => item.name === 'llama.cpp'); if (engine.value) { path.value = engine.value.path || ''; mode.value = engine.value.mode || 'cuda'; apiPort.value = engine.value.apiPort || 8686 }; if (comfyEngine.value) { comfyPath.value = comfyEngine.value.path || ''; comfyMode.value = comfyEngine.value.mode || 'external'; comfyPort.value = comfyEngine.value.apiPort || 8188; comfyAutoUpdate.value = comfyEngine.value.autoUpdate || false; if (comfyMode.value === 'managed' && !comfyPath.value) comfyPath.value = `${runtimeDirectory.root}/comfyui` } }
  catch (error) { message.error(error instanceof Error ? error.message : '读取运行时配置失败') }
  finally { loading.value = false }
}
async function scan() { scanning.value = true; try { await load(); message.success('已重新扫描 PATH 和运行时目录') } finally { scanning.value = false } }
async function save() { saving.value = true; try { const result = await api.saveRuntimeEngine({ path: path.value, mode: mode.value, apiPort: apiPort.value, comfyPath: comfyPath.value, comfyMode: comfyMode.value, comfyPort: comfyPort.value, comfyAutoUpdate: comfyAutoUpdate.value }); engines.value = result.engines; engine.value = result.engines.find(item => item.name === 'llama.cpp'); message.success('运行时配置已保存') } catch (error) { message.error(error instanceof Error ? error.message : '保存运行时配置失败') } finally { saving.value = false } }
async function saveComfy() { await save() }
function onComfyModeChange(value: string | number) { if (String(value) === 'managed' && !comfyPath.value) comfyPath.value = 'ModelDock/runtimes/comfyui' }
async function installComfy() {
  if (!comfyPath.value) { message.warning('请先填写 ModelDock 管理目录'); return }
  const action = comfyEngine.value?.installed ? 'update' : 'install'
  installing.value = true
  comfyInstallError.value = ''
  try {
    await save()
    await api.installComfy(comfyPath.value, action)
    await load()
    message.success(action === 'update' ? 'ComfyUI 已更新' : 'ComfyUI 源码已安装，请继续准备 Python 依赖')
  } catch (error) {
    comfyInstallError.value = error instanceof Error ? error.message : 'ComfyUI 安装失败'
    message.error(comfyInstallError.value)
  } finally { installing.value = false }
}
async function startComfy() {
  if (!comfyPath.value) { message.warning('请先填写 ComfyUI 目录'); return }
  starting.value = true
  comfyInstallError.value = ''
  try {
    await api.startComfy(comfyPath.value, comfyPort.value)
    await load()
    message.success('ComfyUI 已启动，依赖已自动准备')
  } catch (error) {
    comfyInstallError.value = error instanceof Error ? error.message : '启动 ComfyUI 失败'
    message.error(comfyInstallError.value)
  } finally { starting.value = false }
}
async function viewComfyLog() {
  try { const log = await api.comfyLog(); logPath.value = log.path; logContent.value = log.content; logOpen.value = true } catch (error) { message.error(error instanceof Error ? error.message : '读取 ComfyUI 日志失败') }
}
onMounted(load)
</script>
<style scoped>
.runtime-page { max-width: 960px; }
.runtime-path { word-break: break-all; }
.comfy-actions { margin-top: 20px; padding-top: 16px; border-top: 1px solid var(--theme-border); }
.comfy-actions :deep(.ant-btn) { margin-bottom: 4px; }
.runtime-page :deep(.ant-form-item) { margin-bottom: 16px; }
.runtime-page :deep(.ant-alert-description) { line-height: 1.7; }
.runtime-log { max-height: 520px; overflow: auto; margin-top: 12px; padding: 14px; background: #101318; color: #d6e2f0; white-space: pre-wrap; overflow-wrap: anywhere; border-radius: 6px; font-size: 12px; line-height: 1.55; }
</style>
