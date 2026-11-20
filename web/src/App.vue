<template><AppShell :active-key="active" @navigate="active=$event"><KeepAlive><HomePage v-if="active==='home'" /></KeepAlive><MarketPage v-if="active==='market'" /><LocalPage v-if="active==='local'" /><InteractionPage v-if="active==='interaction'" /><TasksPage v-if="active==='tasks'" /><StoragePage v-if="active==='storage'" /><RuntimePage v-if="active==='runtime'" /><ThemePage v-if="active==='theme'" /></AppShell></template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'; import AppShell from './components/AppShell.vue'; import HomePage from './pages/home/index.vue'; import MarketPage from './pages/market/index.vue'; import LocalPage from './pages/local/index.vue'; import InteractionPage from './pages/interaction/index.vue'; import TasksPage from './pages/tasks/index.vue'; import StoragePage from './pages/storage/index.vue'; import RuntimePage from './pages/runtime/index.vue'; import ThemePage from './pages/theme/index.vue'
const active=ref('home')
function navigate(event: Event) { const detail = (event as CustomEvent<string>).detail; if (detail) active.value = detail }
onMounted(() => window.addEventListener('modeldock-navigate', navigate)); onUnmounted(() => window.removeEventListener('modeldock-navigate', navigate))
document.documentElement.dataset.theme = localStorage.getItem('modeldock-theme') || 'light'
</script>
