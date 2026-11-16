<template>
  <div class="page theme-page">
    <a-page-header title="主题" sub-title="选择 ModelDock 的整体外观，主题会同步应用到滚动条和工作区。" />
    <div class="theme-grid">
      <a-card v-for="item in themes" :key="item.key" class="theme-card" :class="{ selected: current === item.key }" hoverable @click="apply(item.key)">
        <div class="theme-preview" :class="`preview-${item.key}`"><span /><span /><span /></div>
        <div class="theme-card-footer"><div><strong>{{ item.name }}</strong><small>{{ item.description }}</small></div><a-tag v-if="current === item.key" color="blue">当前</a-tag></div>
      </a-card>
    </div>
  </div>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import { message } from 'ant-design-vue'
type ThemeKey = 'light' | 'dark' | 'ink' | 'vermilion' | 'celadon' | 'bamboo'
const themes: Array<{ key: ThemeKey; name: string; description: string }> = [{ key:'light', name:'晨光浅色', description:'清爽、明亮的工作区' }, { key:'dark', name:'夜航暗色', description:'低亮度，适合长时间使用' }, { key:'ink', name:'水墨', description:'黑白灰与朱砂点缀' }, { key:'vermilion', name:'朱砂', description:'暖白底色与东方红' }, { key:'celadon', name:'青瓷', description:'青绿色与温润灰白' }, { key:'bamboo', name:'竹影', description:'竹青与宣纸色' }]
const current = ref<ThemeKey>((localStorage.getItem('modeldock-theme') as ThemeKey) || 'light')
function apply(theme: ThemeKey) { current.value = theme; document.documentElement.dataset.theme = theme; localStorage.setItem('modeldock-theme', theme); message.success(`已切换为${themes.find(item=>item.key===theme)?.name}`) }
document.documentElement.dataset.theme = current.value
</script>
