<template>
  <a-layout class="app-shell">
    <a-layout-sider v-model:collapsed="collapsed" breakpoint="lg" collapsible theme="dark">
      <div class="brand"><span class="brand-mark">M</span><span v-if="!collapsed">ModelDock</span></div>
      <a-menu :selected-keys="[activeKey]" theme="dark" mode="inline" @click="select">
        <a-menu-item v-for="item in items" :key="item.key"><template #icon><component :is="item.icon" /></template>{{ item.label }}</a-menu-item>
      </a-menu>
    </a-layout-sider>
    <a-layout><a-layout-header class="topbar"><a-space><a-button type="text" class="topbar-toggle" @click="collapsed = !collapsed" aria-label="收缩或展开侧栏"><MenuOutlined /></a-button><span class="topbar-title">{{ title }}</span></a-space><a-tag color="green">本地服务</a-tag></a-layout-header><a-layout-content class="content"><slot /></a-layout-content></a-layout>
  </a-layout>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { AppstoreOutlined, CloudDownloadOutlined, DatabaseOutlined, ExperimentOutlined, HomeOutlined, SettingOutlined, BgColorsOutlined, MenuOutlined, ToolOutlined } from '@ant-design/icons-vue'
const props = defineProps<{ activeKey: string }>()
const emit = defineEmits<{ navigate: [key: string] }>()
const collapsed = ref(false)
const items = [{ key: 'home', label: '首页', icon: HomeOutlined }, { key: 'market', label: '模型市场', icon: AppstoreOutlined }, { key: 'local', label: '本地模型', icon: DatabaseOutlined }, { key: 'interaction', label: '模型交互', icon: ExperimentOutlined }, { key: 'tasks', label: '下载任务', icon: CloudDownloadOutlined }, { key: 'storage', label: '存储设置', icon: SettingOutlined }, { key: 'runtime', label: '运行时设置', icon: ToolOutlined }, { key: 'theme', label: '主题', icon: BgColorsOutlined }]
const title = computed(() => items.find(item => item.key === props.activeKey)?.label || 'ModelDock')
function select({ key }: { key: string }) { emit('navigate', key) }
</script>
