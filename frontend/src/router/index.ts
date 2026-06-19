import {createRouter,createWebHistory} from 'vue-router'
import {useAuthStore} from '../stores/auth'
const routes=[
{path:'/login',name:'login',component:()=>import('../views/Login.vue'),meta:{title:'登录',public:true}},
{path:'/',name:'dashboard',component:()=>import('../views/Dashboard.vue'),meta:{title:'概览'}},
{path:'/console',name:'console',component:()=>import('../views/Console.vue'),meta:{title:'控制台'}},
{path:'/config',name:'config',component:()=>import('../views/ConfigEditor.vue'),meta:{title:'配置'}},
{path:'/logs',name:'logs',component:()=>import('../views/Logs.vue'),meta:{title:'日志'}},
{path:'/metrics',name:'metrics',component:()=>import('../views/Metrics.vue'),meta:{title:'指标'}},
{path:'/agent',name:'agent',component:()=>import('../views/AgentOps.vue'),meta:{title:'Agent 运维'}},
{path:'/users',name:'users',component:()=>import('../views/Users.vue'),meta:{title:'用户管理',admin:true}}]
const router=createRouter({history:createWebHistory(),routes})
router.beforeEach(async to=>{const auth=useAuthStore();if(!auth.checked)await auth.load();if(to.meta.public)return auth.user?'/':true;if(!auth.user)return {path:'/login',query:{redirect:to.fullPath}};if(to.meta.admin&&!auth.isAdmin)return '/';return true})
export default router
