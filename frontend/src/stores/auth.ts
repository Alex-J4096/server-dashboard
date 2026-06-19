import {defineStore} from 'pinia'
import {api,post} from '../api/client'
import type {Role,User} from '../api/types'

export const useAuthStore=defineStore('auth',{state:()=>({user:null as User|null,checked:false}),getters:{canWrite:s=>s.user?.role==='admin'||s.user?.role==='operator',isAdmin:s=>s.user?.role==='admin'},actions:{async load(){try{this.user=await api<User>('/api/auth/me')}catch{this.user=null}finally{this.checked=true}return this.user},async login(username:string,password:string){this.user=await post<User>('/api/auth/login',{username,password});this.checked=true},async logout(){try{await post('/api/auth/logout')}finally{this.user=null;this.checked=true}},canRole(...roles:Role[]){return !!this.user&&roles.includes(this.user.role)}}})
