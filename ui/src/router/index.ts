import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/dictionaries' },
    { path: '/dashboard', redirect: '/dictionaries' },
    { path: '/dictionaries', name: 'dictionaries', component: () => import('../pages/DictionariesPage.vue') },
    {
      path: '/dictionaries/:dictionaryId',
      name: 'dictionary-details',
      component: () => import('../pages/DictionaryDetailsPage.vue'),
    },
    { path: '/attributes', name: 'attributes', component: () => import('../pages/AttributesPage.vue') },
    { path: '/schema', redirect: '/dictionaries' },
    { path: '/objects', name: 'objects', component: () => import('../pages/ObjectsPage.vue') },
    { path: '/entries', redirect: '/objects' },
    { path: '/audit', name: 'audit', component: () => import('../pages/AuditPage.vue') },
    { path: '/health', name: 'health', component: () => import('../pages/HealthPage.vue') },
  ],
})
