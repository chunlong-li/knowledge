import { createRouter, createWebHashHistory } from 'vue-router'

const routes = [
  { path: '/', name: 'home', component: () => import('./views/Home.vue') },
  { path: '/users', name: 'users', component: () => import('./views/Users.vue') },
  { path: '/update', name: 'update', component: () => import('./views/Update.vue') },
  { path: '/entry/:id', name: 'entry', component: () => import('./views/Entry.vue') },
  { path: '/trash', name: 'trash', component: () => import('./views/Trash.vue') },
  { path: '/audit', name: 'audit', component: () => import('./views/Audit.vue') }
]

export default createRouter({
  history: createWebHashHistory(),
  routes
})
