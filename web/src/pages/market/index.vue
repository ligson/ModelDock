<template>
  <div class="page market-page">
    <a-card>
      <a-card v-if="componentNotice" class="component-notice" size="small">
        <template #title>Qwen-Image 运行组件准备清单</template>
        <template #extra><a-tag color="orange">不是普通模型</a-tag></template>
        <a-alert type="warning" show-icon message="只下载下面列出的组件，不要点击下方 Qwen、Stable Diffusion 等普通模型的下载按钮。" />
        <a-list size="small" :data-source="componentRecommendations" class="component-list">
          <template #renderItem="{ item, index }"><a-list-item><a-list-item-meta><template #title><strong>第 {{ index + 1 }} 项：{{ item.name }}</strong></template><template #description><span>{{ item.role }} · {{ item.size }} · 下载后自动放入 {{ item.folder }}</span></template></a-list-item-meta><a-button type="primary" size="small" :loading="componentLoading[item.name]" @click="downloadComponent(item)">加入下载任务</a-button></a-list-item></template>
        </a-list>
        <a-typography-text type="secondary">下载完成后回到“模型交互”，点击“重启 ComfyUI”，再重新发送提示词。ComfyUI-GGUF 节点已经由“一键准备”安装。</a-typography-text>
      </a-card>
      <a-tabs v-model:active-key="tab">
        <a-tab-pane key="recommended" tab="适合我的电脑" />
        <a-tab-pane key="all" tab="全部模型" />
        <a-tab-pane key="search" tab="搜索 Hugging Face" />
      </a-tabs>
      <template v-if="tab !== 'search'">
        <a-space wrap class="filters"><a-button v-for="item in categories" :key="item.value" :type="category === item.value ? 'primary' : 'default'" @click="category = item.value">{{ item.label }}</a-button></a-space>
        <a-alert v-if="hardwareNotice" :message="hardwareNotice" type="info" show-icon />
        <div class="recommended-list">
          <div v-for="item in filteredCatalog" :key="item.id" class="model-row">
            <div class="model-main"><div class="model-title"><strong>{{ item.name }}</strong><a-tag>{{ item.category }}</a-tag><a :href="sourceURL(item.source, item.id)" target="_blank" rel="noreferrer" class="detail-link"><LinkOutlined /> 详情</a></div><div class="model-description">{{ item.description }}</div><div class="model-meta">{{ item.format }} · {{ compatibility(item).reason }}</div></div>
            <div class="model-assessment"><a-tag :color="compatibility(item).color">{{ compatibility(item).status }}</a-tag><a-button v-if="needsGGUF(item)" size="small" :loading="variantLoading[item.id]" @click="findGGUF(item)">查找 GGUF 版本</a-button><a-button v-else type="primary" size="small" :disabled="!canDownload(item)" @click="downloadModel(item)">下载推荐版</a-button></div>
            <a-list v-if="variants[item.id]?.length" size="small" :data-source="variants[item.id]" class="variant-list"><template #renderItem="{ item: variant }"><a-list-item><a-list-item-meta :title="variant.name" :description="formatLabel(variant)" /><GGUFFilePicker :files="inventories[variant.id]?.files || []" :mmproj="inventories[variant.id]?.mmproj" :selected="selectedQuant[variant.id]" :include-mmproj="includeMmproj[variant.id]" :loading="variantLoading[variant.id]" @inspect="inspectVariant(variant)" @select="selectedQuant[variant.id] = $event" @include-mmproj="includeMmproj[variant.id] = $event" @download="downloadQuantized(variant)" /></a-list-item></template></a-list><a-alert v-if="variantErrors[item.id]" :message="variantErrors[item.id]" type="warning" show-icon />
          </div>
          <a-empty v-if="!filteredCatalog.length" description="暂无匹配模型" />
        </div>
      </template>
      <template v-else>
        <a-space direction="vertical" style="width:100%" size="large">
          <a-space>
            <a-select v-model:value="source" style="width:180px" :options="sources" />
            <a-input-search v-model:value="query" placeholder="搜索公开模型" enter-button="搜索" :loading="searching" @search="search" />
          </a-space>
          <a-alert v-if="error" :message="error" type="error" show-icon closable @close="error = ''" />
          <a-alert v-if="notice" :message="notice" type="info" show-icon />
          <a-row :gutter="16">
            <a-col v-for="item in results" :key="item.id" :xs="24" :md="12" :lg="8">
              <a-card size="small">
                <template #title>
                  <div class="market-card-title">
                    <span class="market-card-name">{{ item.name }}</span>
                    <a v-if="sourceURL(item.source,item.id)" class="market-detail-link" :href="sourceURL(item.source,item.id)" target="_blank" rel="noreferrer"><LinkOutlined /> 详情</a>
                  </div>
                </template>
                <a-space wrap>
                  <a-typography-text type="secondary">{{ item.source }}</a-typography-text>
                  <a-tag>{{ formatLabel(item) }}</a-tag>
                  <a-tag :color="compatibility(item).color">{{ compatibility(item).status }}</a-tag>
                </a-space>
                <a-typography-text type="secondary" class="compatibility-reason">{{ compatibility(item).reason }}</a-typography-text>
                <a-button v-if="needsGGUF(item)" size="small" :loading="variantLoading[item.id]" @click="findGGUF(item)">查找可运行的 GGUF 仓库</a-button>
                <GGUFFilePicker v-else-if="isGGUF(item)" :files="inventories[item.id]?.files || []" :mmproj="inventories[item.id]?.mmproj" :selected="selectedQuant[item.id]" :include-mmproj="includeMmproj[item.id]" :loading="variantLoading[item.id]" @inspect="inspectVariant(item)" @select="selectedQuant[item.id] = $event" @include-mmproj="includeMmproj[item.id] = $event" @download="downloadQuantized(item)" />
                <a-alert v-if="variantErrors[item.id]" :message="variantErrors[item.id]" type="warning" show-icon />
                <a-list v-if="variants[item.id]?.length" size="small" :data-source="variants[item.id]" class="variant-list">
                  <template #renderItem="{ item: variant }">
                    <a-list-item>
                      <a-list-item-meta :title="variant.name" :description="formatLabel(variant)" />
                      <GGUFFilePicker :files="inventories[variant.id]?.files || []" :mmproj="inventories[variant.id]?.mmproj" :selected="selectedQuant[variant.id]" :include-mmproj="includeMmproj[variant.id]" :loading="variantLoading[variant.id]" @inspect="inspectVariant(variant)" @select="selectedQuant[variant.id] = $event" @include-mmproj="includeMmproj[variant.id] = $event" @download="downloadQuantized(variant)" />
                    </a-list-item>
                  </template>
                </a-list>
                <template #actions>
                  <a-button v-if="!needsGGUF(item) && !isGGUF(item)" type="link" disabled>没有确认可下载的模型文件</a-button>
                </template>
              </a-card>
            </a-col>
          </a-row>
          <a-empty v-if="searched && !searching && !error && !results.length" description="没有找到模型" />
        </a-space>
      </template>
    </a-card>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { LinkOutlined } from '@ant-design/icons-vue'
import { api } from '../../services/api'
import { sourceURL } from '../../utils/source'
import GGUFFilePicker, { type GGUFFileOption, type GGUFProjectionOption } from '../../components/GGUFFilePicker.vue'
import type { CatalogItem, Host } from '../../types/api'
type SearchItem = { id: string; name: string; source: string; downloads?: number; tags?: string[]; format?: string; category?: string; sizeBytes?: number }
const tab = ref('recommended'); const category = ref('all'); const catalog = ref<CatalogItem[]>([]); const host = ref<Host>(); const hardwareNotice = ref(''); const componentNotice = ref(localStorage.getItem('modeldock-market-components') === 'qwen-image-2.1')
const componentRecommendations = [{ name: 'qwen3vl_8b_w4a8.safetensors', role: 'Qwen-Image 文本编码器', folder: 'ComfyUI/models/text_encoders', size: '约 5.9 GB', url: 'https://huggingface.co/Comfy-Org/Qwen-Image-2.1/resolve/main/text_encoders/qwen3vl_8b_w4a8.safetensors' }, { name: 'qwen_image_2.1_vae_bf16.safetensors', role: 'Qwen-Image VAE 解码器', folder: 'ComfyUI/models/vae', size: '约 645 MB', url: 'https://huggingface.co/Comfy-Org/Qwen-Image-2.1/resolve/main/vae/qwen_image_2.1_vae_bf16.safetensors' }]
const componentLoading = ref<Record<string, boolean>>({})
async function downloadComponent(item: (typeof componentRecommendations)[number]) { if (componentLoading.value[item.name]) return; componentLoading.value[item.name] = true; try { const result = await api.downloadComfyComponent(item.name); message.success(`${item.name} 已加入下载任务`); localStorage.setItem('modeldock-market-components', 'qwen-image-2.1') } catch (error) { message.error(error instanceof Error ? error.message : '创建组件下载任务失败') } finally { componentLoading.value[item.name] = false } }
const source = ref('huggingface'); const query = ref(''); const searched = ref(false); const searching = ref(false); const error = ref(''); const notice = ref(''); const results = ref<SearchItem[]>([]); const variants = ref<Record<string, SearchItem[]>>({}); const variantLoading = ref<Record<string, boolean>>({}); const variantErrors = ref<Record<string, string>>({})
const inventories = ref<Record<string, { files: GGUFFileOption[]; mmproj?: GGUFProjectionOption }>>({}); const selectedQuant = ref<Record<string, string>>({}); const includeMmproj = ref<Record<string, boolean>>({})
const sources = [{ value: 'huggingface', label: 'Hugging Face' }, { value: 'modelscope', label: 'ModelScope' }, { value: 'ollama', label: '本机 Ollama' }]
const categories = [{ value: 'all', label: '全部' }, { value: '文本', label: '对话' }, { value: '代码', label: '编程' }, { value: '图片', label: '看图' }, { value: '视频', label: '视频' }, { value: '声音', label: '声音' }]
const filteredCatalog = computed(() => tab.value === 'recommended' ? catalog.value.filter(item => compatibility(item).status !== '内存可能不足') : catalog.value.filter(item => category.value === 'all' || item.category === category.value))
function tags(item: SearchItem) { return (item.tags || []).map(tag => tag.toLowerCase()) }
function quantization(item: SearchItem) { return tags(item).find(tag => /^(?:iq)?[2-8]_[a-z0-9_]+$/.test(tag))?.toUpperCase() || '' }
function isGGUF(item: SearchItem) { return tags(item).includes('gguf') || /gguf/i.test(item.id) }
function sizeLabel(size?: number) { return size ? `${(size / 1024 ** 3).toFixed(2)} GB` : '大小未知' }
function canDownload(item: SearchItem) { return ['本机可运行（估算）', '显存可容纳（估算）'].includes(compatibility(item).status) }
function formatLabel(item: SearchItem) { const allTags = tags(item); if (allTags.includes('gguf') || /gguf/i.test(item.id)) return `GGUF${quantization(item) ? ` · ${quantization(item)}` : ''}`; if (allTags.includes('safetensors') || item.format === 'safetensors') return 'SafeTensors'; if (allTags.includes('ollama')) return 'Ollama'; return '格式未确认' }
function needsGGUF(item: SearchItem) { const allTags = tags(item); const text = `${item.name} ${item.category || ''} ${allTags.join(' ')}`.toLowerCase(); return item.source === 'huggingface' && (allTags.includes('safetensors') || item.format === 'safetensors') && !allTags.includes('gguf') && /(text-generation|conversational|causal-lm|文本|对话|编程|代码)/.test(text) }
function isImageGeneration(item: SearchItem) { const allTags = tags(item); return allTags.some(tag => ['text-to-image', 'image-to-image', 'image-editing', 'qwen-image'].includes(tag) || tag === 'pipeline_tag:text-to-image') }
function compatibility(item: { name: string; category?: string; tags?: string[]; id?: string; source?: string; format?: string; sizeBytes?: number }) { const searchItem = item as SearchItem; const text = `${item.name} ${item.category || ''} ${(item.tags || []).join(' ')}`.toLowerCase(); const memory = host.value?.memoryGB || 0; const gpu = Math.max(0, ...(host.value?.gpus || []).map(item => item.memoryGB || 0)); const platform = host.value ? `${host.value.os}/${host.value.arch}` : '未知架构'; const hardware = `${platform}，${memory || '未知'} GB 内存，${gpu || '未知'} GB 显存`; const comfy = host.value?.runtimes.some(runtime => runtime.name === 'comfyui' && runtime.installed); if (needsGGUF(searchItem)) return { status: '当前格式不能运行', color: 'red', reason: `此仓库是 SafeTensors，llama.cpp 需要 GGUF 权重；请查找对应量化版本。硬件：${hardware}` }; const gguf = isGGUF(searchItem); const hasLlama = host.value?.runtimes.some(runtime => runtime.name === 'llama.cpp' && runtime.installed); if (gguf && isImageGeneration(searchItem)) return { status: comfy ? '需 ComfyUI 工作流' : '缺少图像运行时', color: comfy ? 'orange' : 'red', reason: `这是图像生成模型，不能用 llama.cpp 聊天。${comfy ? '已发现 ComfyUI，但还需 ComfyUI-GGUF 加载节点，以及基座的文本编码器和 VAE（本仓库不包含）。' : '需要 ComfyUI（含 ComfyUI-GGUF 加载节点）或 Diffusers；本仓库只含 Transformer 权重。'} 硬件：${hardware}` }; if (gguf && !quantization(searchItem)) return { status: '未确认量化文件', color: 'orange', reason: '仓库可能包含多个权重文件；为避免整仓库下载，暂不推荐直接下载' }; if (gguf && !item.sizeBytes) return { status: '需要检查文件大小', color: 'orange', reason: `运行时：${hasLlama ? '已发现 llama.cpp' : '未发现 llama.cpp'}；尚未读取所选权重大小。硬件：${hardware}` }; if (gguf && !hasLlama) return { status: '缺少 llama.cpp', color: 'red', reason: `GGUF 需要 llama.cpp；当前未发现该运行时。硬件：${hardware}` }; if (gguf && item.sizeBytes) { const weightGB = item.sizeBytes / 1024 ** 3; const estimatedRAM = weightGB + 2; if (memory && memory >= estimatedRAM) return { status: '本机可运行（估算）', color: 'green', reason: `${quantization(searchItem)} 权重 ${weightGB.toFixed(2)} GB；系统内存 ${memory} GB，高于约 ${estimatedRAM.toFixed(1)} GB 的权重与运行余量估算。显存 ${gpu || '未知'} GB，实际速度取决于 GPU 卸载比例` }; if (gpu && gpu >= weightGB + 1) return { status: '显存可容纳（估算）', color: 'green', reason: `${quantization(searchItem)} 权重 ${weightGB.toFixed(2)} GB，显存 ${gpu} GB；仍需预留上下文缓存` }; return { status: '硬件资源可能不足', color: 'red', reason: `权重 ${weightGB.toFixed(2)} GB，约需 ${estimatedRAM.toFixed(1)} GB 系统内存或 ${Math.ceil(weightGB + 1)} GB 显存；检测到内存 ${memory || '未知'} GB、显存 ${gpu || '未知'} GB` } } if (/(text-to-video|video-generation|diffusion|flux|sdxl|wan2)/.test(text)) return { status: '需图片/视频运行时', color: 'orange', reason: `${comfy ? '已发现 ComfyUI' : '当前未发现 ComfyUI'}；硬件：${hardware}，还需确认模型工作流和显存要求` }; if (/(image-text-to-text|vision-language)/.test(text)) return { status: '可运行文本，视觉待确认', color: 'orange', reason: `硬件：${hardware}；图像输入需确认对应 mmproj 文件和 llama.cpp 支持` }; return { status: '需进一步检查', color: 'default', reason: `硬件：${hardware}；参数量、权重格式和运行时信息不足，暂不推荐直接下载` } }
async function search() { if (!query.value.trim()) { error.value = '请输入模型名称或关键词'; return }; if (searching.value) return; searching.value = true; searched.value = true; error.value = ''; variants.value = {}; try { results.value = await api.search(source.value, query.value.trim()) } catch (cause) { results.value = []; error.value = cause instanceof Error ? cause.message : '搜索失败，请检查网络连接后重试' } finally { searching.value = false } }
function quantizationFromPath(path: string) { const base = path.split(/[\\/]/).pop()?.replace(/\.gguf$/i, '') || ''; const match = base.match(/(?:^|[._-])(IQ[1-8]_[A-Z0-9_]+|TQ[1-2]_[A-Z0-9_]+|MXFP[0-9]+|BF16|Q[2-8]_[A-Z0-9_]+|Q[2-8]|F16|F32)$/i); return match?.[1].toUpperCase() || '' }
function assessSize(sizeBytes: number, item: SearchItem) { const memory = host.value?.memoryGB || 0; const gpu = Math.max(0, ...(host.value?.gpus || []).map(item => item.memoryGB || 0)); const comfy = host.value?.runtimes.some(runtime => runtime.name === 'comfyui' && runtime.installed); const image = isImageGeneration(item); const weightGB = sizeBytes / 1024 ** 3; if (image && !comfy) return { compatible: false, reason: '需要 ComfyUI 和 ComfyUI-GGUF；本仓库还不含文本编码器、VAE' }; if (!image && !host.value?.runtimes.some(runtime => runtime.name === 'llama.cpp' && runtime.installed)) return { compatible: false, reason: '未检测到 llama.cpp' }; if (!sizeBytes) return { compatible: false, reason: '仓库未提供文件大小' }; const runtimeMargin = image ? 4 : 2; if (memory && memory >= weightGB + runtimeMargin) return { compatible: true, reason: `内存约需 ${(weightGB + runtimeMargin).toFixed(1)} GB${image ? '，另需 ComfyUI-GGUF、文本编码器和 VAE' : ''}` }; if (memory >= 2 && gpu && gpu >= weightGB + (image ? 4 : 1)) return { compatible: true, reason: `显存约需 ${Math.ceil(weightGB + (image ? 4 : 1))} GB${image ? '，另需 ComfyUI-GGUF、文本编码器和 VAE' : ''}` }; return { compatible: false, reason: `约需 ${(weightGB + runtimeMargin).toFixed(1)} GB 内存或 ${Math.ceil(weightGB + (image ? 4 : 1))} GB 显存${image ? '，并需 ComfyUI-GGUF、文本编码器和 VAE' : ''}` } }
async function inspectVariant(item: SearchItem) {
  variantLoading.value[item.id] = true
  variantErrors.value[item.id] = ''
  try {
    const manifest = await api.manifest(item.source, item.id, '', false, true)
    const mainFiles = manifest.files.filter(file => file.path.toLowerCase().endsWith('.gguf') && !file.path.toLowerCase().includes('mmproj'))
    const projectionFiles = manifest.files.filter(file => file.path.toLowerCase().includes('mmproj') && file.path.toLowerCase().endsWith('.gguf'))
    const options: GGUFFileOption[] = mainFiles.map(file => {
      const quant = quantizationFromPath(file.path)
      const assessment = assessSize(file.size || 0, item)
      return { quantization: quant || 'GGUF', path: file.path, sizeBytes: file.size || 0, compatible: assessment.compatible, reason: assessment.reason }
    }).filter(file => file.quantization !== 'GGUF')
    const preferred = ['Q4_K_M', 'Q4_K_S', 'IQ4_XS', 'Q5_K_M', 'Q5_K_S', 'Q3_K_M', 'Q3_K_L', 'Q3_K_S', 'Q2_K', 'Q6_K', 'Q8_0', 'F16', 'BF16', 'F32']
    const rank = (quant: string) => {
      const index = preferred.indexOf(quant)
      return index < 0 ? preferred.length : index
    }
    options.sort((a, b) => rank(a.quantization) - rank(b.quantization))
    const recommended = options.find(file => file.quantization === 'Q4_K_M' && file.compatible) || options.find(file => file.compatible)
    if (recommended) recommended.recommendation = '推荐：质量与体积均衡'
    const projection = projectionFiles.sort((a, b) => Number(!a.path.toLowerCase().includes('q8_0')) - Number(!b.path.toLowerCase().includes('q8_0')))[0]
    inventories.value[item.id] = { files: options, mmproj: projection ? { path: projection.path, sizeBytes: projection.size || 0, quantization: quantizationFromPath(projection.path) } : undefined }
    selectedQuant.value[item.id] = (recommended || options[0])?.quantization || ''
    includeMmproj.value[item.id] = false
    if (!options.length) variantErrors.value[item.id] = '这个仓库没有发现可选的 GGUF 主权重文件'
    else if (!options.some(file => file.compatible) && isImageGeneration(item)) variantErrors.value[item.id] = '已识别仓库 GGUF 文件；当前未检测到可运行它的 ComfyUI 图像运行时，llama.cpp 不能用于此图像模型'
    else if (!options.some(file => file.compatible)) variantErrors.value[item.id] = '已列出仓库文件，但按本机资源估算没有明确可推荐的量化版本'
  } catch (cause) {
    variantErrors.value[item.id] = cause instanceof Error ? cause.message : '读取仓库文件失败'
  } finally {
    variantLoading.value[item.id] = false
  }
}
async function findGGUF(item: SearchItem) { variantLoading.value[item.id] = true; variantErrors.value[item.id] = ''; try { const found = await api.search('huggingface', `${item.id} GGUF`); const baseName = item.id.split('/').pop()?.toLowerCase() || ''; const candidates = found.filter(candidate => tags(candidate).includes('gguf') && (tags(candidate).some(tag => tag.toLowerCase() === `base_model:${item.id.toLowerCase()}`) || candidate.id.toLowerCase().includes(baseName) && /gguf/i.test(candidate.id))); candidates.sort((a, b) => Number(!tags(a).includes('q4_k_m')) - Number(!tags(b).includes('q4_k_m'))); variants.value[item.id] = candidates.slice(0, 5); if (!candidates.length) variantErrors.value[item.id] = '暂未找到与该模型对应的 GGUF 仓库；不要下载当前 SafeTensors 版本用于 llama.cpp。' } catch (cause) { variantErrors.value[item.id] = cause instanceof Error ? cause.message : '查找 GGUF 版本失败' } finally { variantLoading.value[item.id] = false } }
async function downloadQuantized(item: SearchItem) { const quant = selectedQuant.value[item.id]; const option = inventories.value[item.id]?.files.find(file => file.quantization === quant); if (!option?.compatible) return; variantLoading.value[item.id] = true; try { const manifest = await api.manifest(item.source, item.id, quant, includeMmproj.value[item.id] === true); const task = await api.download(manifest.id); notice.value = `${item.name} · ${quant}${includeMmproj.value[item.id] ? ' + mmproj' : ''} 已加入下载任务`; message.success(`已创建下载任务：${task.id}`) } catch (cause) { message.error(cause instanceof Error ? cause.message : '创建下载任务失败') } finally { variantLoading.value[item.id] = false } }
async function downloadModel(item: SearchItem) { try { if (!canDownload(item)) { message.warning('此仓库格式、运行时或硬件条件未确认，暂不创建下载任务'); return }; const quant = quantization(item); const manifest = await api.manifest(item.source, item.id, quant); const format = manifest.format.toLowerCase(); const task = await api.download(manifest.id); notice.value = `${manifest.name}（${quant || format.toUpperCase()}）已加入下载任务`; message.success(`下载任务已创建：${task.id}`) } catch (cause) { message.error(cause instanceof Error ? cause.message : '创建下载任务失败') } }
onMounted(async () => { try { const [detected, popular] = await Promise.all([api.host(), api.catalog()]); host.value = detected; catalog.value = popular; hardwareNotice.value = `已根据本机 ${detected.memoryGB || '未知'} GB 内存、${Math.max(0, ...(detected.gpus || []).map(item => item.memoryGB || 0)) || '未知'} GB 显存进行推荐` } catch { hardwareNotice.value = '暂时无法读取本机硬件，推荐结果仅供参考' } })
</script>
<style scoped>
.filters { margin-bottom: 18px; }.recommended-list { margin-top: 18px; border: 1px solid var(--theme-border); border-radius: 8px; overflow: hidden; }.model-row { display: flex; justify-content: space-between; gap: 20px; padding: 18px 20px; border-bottom: 1px solid var(--theme-border); }.model-row:last-child { border-bottom: 0; }.model-main { min-width: 0; }.model-title { display: flex; align-items: flex-start; gap: 8px; min-width: 0; }.model-title strong { min-width: 0; overflow-wrap: anywhere; }.detail-link { flex: none; color: var(--theme-accent); font-size: 12px; }.model-description, .model-meta { margin-top: 7px; color: var(--theme-muted); font-size: 12px; }.model-assessment { display: flex; align-items: center; gap: 12px; flex: none; }.market-card-title { display: flex; align-items: flex-start; gap: 8px; min-width: 0; }.market-card-name { min-width: 0; white-space: normal; overflow-wrap: anywhere; line-height: 20px; }.market-detail-link { flex: none; color: var(--theme-accent); font-size: 12px; white-space: nowrap; }.compatibility-reason { display: block; margin-top: 8px; min-height: 36px; font-size: 12px; line-height: 18px; }@media (max-width: 640px) { .model-row { flex-direction: column; }.model-assessment { justify-content: space-between; } }
.component-notice { margin-bottom: 16px; }.component-notice :deep(.ant-list-item) { padding-inline: 0; gap: 16px; }.component-notice :deep(.ant-list-item-meta-title) { overflow-wrap: anywhere; }.component-list { margin: 10px 0; }
</style>
