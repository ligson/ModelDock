<template><AppShell :active-key="active" @navigate="active=$event"><HomePage v-if="active==='home'" /><MarketPage v-else-if="active==='market'" /><LocalPage v-else-if="active==='local'" /><InteractionPage v-else-if="active==='interaction'" /><OllamaPage v-else-if="active==='ollama'" /><TasksPage v-else-if="active==='tasks'" /><StoragePage v-else-if="active==='storage'" /><ThemePage v-else /></AppShell></template>
<script setup lang="ts">
import { ref } from 'vue'; import AppShell from './components/AppShell.vue'; import HomePage from './pages/home/index.vue'; import MarketPage from './pages/market/index.vue'; import LocalPage from './pages/local/index.vue'; import InteractionPage from './pages/interaction/index.vue'; import OllamaPage from './pages/ollama/index.vue'; import TasksPage from './pages/tasks/index.vue'; import StoragePage from './pages/storage/index.vue'; import ThemePage from './pages/theme/index.vue'
const active=ref('home')
document.documentElement.dataset.theme = localStorage.getItem('modeldock-theme') || 'light'
</script>
